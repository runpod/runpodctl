// Package logstream holds the parts of `pod logs` and `serverless logs` that are
// the same on both: the flag set, the validation of those flags, and the loop
// that drives a snapshot or a follow into the output stream.
//
// It exists because cmd/pod and cmd/serverless are separate packages that cannot
// import each other, and the two commands are the same feature pointed at two
// routes. Keeping the flag names, the defaults and the error messages in one
// place is what stops them drifting apart.
package logstream

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/clierr"
	"github.com/runpod/runpodctl/internal/duration"
	"github.com/runpod/runpodctl/internal/output"
	"github.com/runpod/runpodctl/internal/waitfor"
	"github.com/spf13/cobra"
)

// DefaultMaxWait bounds a snapshot that never goes quiet. A container writing
// faster than the internal quiet gap would otherwise stream forever from a
// command the user did not ask to follow.
const DefaultMaxWait = 5 * time.Second

// MaxStreams caps how many log streams are read at once. See the note in Run for
// why this is a hard cap rather than a queue.
const MaxStreams = 32

// DefaultDiscoveryInterval is how often a follow re-resolves the worker set. Slow
// enough not to add meaningful load to a long follow, fast enough that a worker
// coming up during a deploy is picked up while that deploy is still interesting.
const DefaultDiscoveryInterval = 15 * time.Second

// Flags holds the values of the shared log flags.
type Flags struct {
	Tail    int
	Since   string
	Source  string
	Follow  bool
	MaxWait time.Duration
}

// Register attaches the shared flags to a command. tailChanged is read back from
// cobra rather than compared against a default, because tail=0 is a meaningful
// value ("no backfill") that must be distinguishable from an unset flag.
func (f *Flags) Register(cmd *cobra.Command) {
	cmd.Flags().IntVar(&f.Tail, "tail", 100, fmt.Sprintf("historical lines to replay before live output (0-%d; 0 = live only)", api.LogTailMax))
	cmd.Flags().StringVar(&f.Since, "since", "", "only logs after this point: a duration like 30m, 2h, 7d, or an rfc3339 timestamp. overrides --tail")
	cmd.Flags().StringVar(&f.Source, "source", "both", "which log source to read: container, system, or both")
	cmd.Flags().BoolVarP(&f.Follow, "follow", "f", false, "keep streaming new lines until interrupted")
	cmd.Flags().DurationVar(&f.MaxWait, "max-wait", DefaultMaxWait, "how long to wait for output before exiting when not following (exits earlier once the replayed lines stop arriving)")
}

// Resolve validates the flags and converts them into api options.
//
// now is passed in so a relative --since is testable. Validation is done here
// rather than left to the api because the api answers an oversized tail with a
// bare 422 and a malformed since with an unstructured message, neither of which
// tells the user which flag was wrong.
func (f *Flags) Resolve(cmd *cobra.Command, now time.Time) (api.LogStreamOptions, error) {
	opts := api.LogStreamOptions{}

	if f.Tail < 0 || f.Tail > api.LogTailMax {
		return opts, clierr.Usagef("invalid --tail %d: must be between 0 and %d", f.Tail, api.LogTailMax)
	}

	switch f.Source {
	case "both", "":
		// the wire enum is only container|system: both is expressed by omitting
		// the param, so nothing is set here.
	case "container", "system":
		opts.Source = f.Source
	default:
		return opts, clierr.Usagef("invalid --source %q: must be container, system, or both", f.Source)
	}

	if f.MaxWait <= 0 {
		return opts, clierr.Usagef("invalid --max-wait %s: must be positive", f.MaxWait)
	}

	// --max-wait bounds a snapshot; a follow runs until interrupted. Saying so
	// matches how the --tail/--since overlap is reported: a flag that was set and
	// then ignored should not be silently dropped.
	if f.Follow && cmd != nil && cmd.Flags().Changed("max-wait") {
		notef("--max-wait is ignored when --follow is set")
	}

	if f.Since != "" {
		since, err := resolveSince(f.Since, now)
		if err != nil {
			return opts, err
		}
		opts.Since = since
		// since and tail are mutually exclusive server-side (since wins). Saying
		// so is better than silently dropping the flag the user set.
		if cmd != nil && cmd.Flags().Changed("tail") {
			notef("--tail is ignored when --since is set")
		}
		return opts, nil
	}

	tail := f.Tail
	opts.Tail = &tail
	return opts, nil
}

// resolveSince accepts either a duration ("30m") or an rfc3339 timestamp and
// returns the rfc3339 the api wants. The duration form is what users actually
// ask for ("the last 30 minutes"); requiring them to compute a timestamp is the
// friction this exists to remove.
func resolveSince(value string, now time.Time) (string, error) {
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC().Format(time.RFC3339Nano), nil
	}
	relative, err := duration.Parse(value)
	if err != nil {
		return "", clierr.Usagef("invalid --since %q: use a duration like 30m, 2h, 7d, or an rfc3339 timestamp", value)
	}
	return now.Add(-relative).UTC().Format(time.RFC3339Nano), nil
}

// Target names one log stream to read.
type Target struct {
	// Path is the api route, from api.PodLogsPath / api.WorkerLogsPath.
	Path string
	// WorkerID, when set, is stamped onto every entry. It is only used by the
	// serverless fan-in, where one output stream carries several workers.
	WorkerID string
}

// Discovery re-resolves the target set while a follow is running.
//
// It exists because an endpoint's worker set is not fixed: scaling up, or a
// worker being replaced rather than restarted, produces a worker id that did not
// exist when the command started. Resolving once meant `--follow` kept watching
// the original workers and silently ignored every one that arrived after -- the
// mirror image of not reporting the ones that left.
type Discovery struct {
	// Refresh returns the currently addressable targets. A failure is transient
	// (the endpoint is still there, the poll just did not land), so it is reported
	// and retried rather than ending the command.
	Refresh func(context.Context) ([]Target, error)
	// Interval is how often to re-resolve. Zero means DefaultDiscoveryInterval.
	Interval time.Duration
}

// Run streams one or more targets to stdout until the snapshot bound is reached
// or, when following, until the user interrupts.
//
// Several targets are read concurrently and interleaved as they arrive, which is
// the only way to follow a whole endpoint: each worker is a separate route, and
// reading them in sequence would mean the second worker's output only appears
// after the first stops -- which for a live worker is never.
//
// discovery may be nil (a pod has exactly one log stream, and an explicit
// --worker names the only one wanted). When set, it only runs under --follow: a
// snapshot is bounded and short enough that the worker set cannot meaningfully
// change while it reads.
func Run(client *api.LogClient, targets []Target, opts api.LogStreamOptions, flags *Flags, format output.Format, discovery *Discovery) error {
	if len(targets) == 0 {
		return fmt.Errorf("no log streams to read")
	}

	// Each target is its own goroutine and its own open connection, and under
	// --follow none of them ever finish -- so a queue would starve rather than
	// drain, and the only honest bound is to read fewer of them. An endpoint with
	// a large workersMax would otherwise open hundreds of concurrent tls streams.
	// Say so on stderr: a silent cap reads as full coverage.
	if len(targets) > MaxStreams {
		notef("reading the first %d of %d workers (--worker <id> to pick one)", MaxStreams, len(targets))
		targets = targets[:MaxStreams]
	}

	writer := output.NewLineWriter(&output.Config{Format: format})
	defer func() { _ = writer.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if flags.Follow {
		// A follow only ends when the user says so, so ctrl-c is the documented
		// exit and must leave the process with a clean status.
		var stop context.CancelFunc
		ctx, stop = waitfor.SignalContext(ctx, signal.NotifyContext, os.Interrupt, syscall.SIGTERM)
		defer stop()
	}

	// Entries from every target funnel through one channel so that a single
	// goroutine owns the writer: concurrent encoder writes would interleave
	// mid-record and corrupt the output.
	entries := make(chan api.LogEntry, 256)
	var wg sync.WaitGroup

	// Streams can now be added while others are running, so the error set and the
	// record of what is already being read are shared mutable state.
	//
	// seen and live are deliberately separate. seen never shrinks, because it is
	// what stops a worker that already ended from being re-attached on the next
	// discovery tick. live is the number of streams actually open, and is what
	// MaxStreams bounds: on a churning endpoint workers are *replaced* rather than
	// restarted, so counting the seen-set against the cap would stop attaching new
	// workers once 32 ids had ever existed -- while the stderr note claimed 32
	// streams were open, most of them long since ended.
	var mu sync.Mutex
	streamErrs := make([]error, 0, len(targets))
	seen := make(map[string]bool, len(targets))
	live := 0

	// spawn starts one stream. Callers hold mu only to decide *whether* to spawn,
	// never across the goroutine's lifetime. live is incremented by the caller,
	// under the same mu as the cap check: incrementing it inside the goroutine
	// would let several spawns past the cap before any of them ran.
	spawn := func(target Target) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sink := func(entry api.LogEntry) error {
				if target.WorkerID != "" {
					entry.WorkerID = target.WorkerID
				}
				select {
				case entries <- entry:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}

			var err error
			if flags.Follow {
				err = client.Follow(ctx, target.Path, opts, sink, followNotice(target))
			} else {
				err = client.Snapshot(ctx, target.Path, opts, flags.MaxWait, sink)
			}

			mu.Lock()
			streamErrs = append(streamErrs, err)
			live--
			// seen, not live: this asks whether the command is reading one worker
			// or several, which does not change as the streams finish.
			multiple := len(seen) > 1
			mu.Unlock()

			// Report the moment it happens, not at the end. Under --follow the
			// remaining streams keep the command alive indefinitely, so deferring
			// this to combineStreamErrors meant a worker that died five seconds in
			// stayed invisible until ctrl-c -- while the user sat watching a
			// deploy and silently missed the one worker that failed.
			//
			// Only worth saying when there is more than one stream: with a single
			// target the error is the command's own result and is reported once,
			// by the caller.
			if err != nil && multiple && ctx.Err() == nil {
				notef("worker %s: %v", target.WorkerID, err)
			}
		}()
	}

	mu.Lock()
	for _, target := range targets {
		seen[target.Path] = true
		live++
		spawn(target)
	}
	mu.Unlock()

	if flags.Follow && discovery != nil && discovery.Refresh != nil {
		// The discovery loop holds a WaitGroup slot for its whole life, which is
		// what makes adding streams later safe: the counter cannot reach zero
		// while it is still able to call wg.Add, so the drain goroutine below
		// never closes `entries` out from under a newly spawned stream.
		wg.Add(1)
		go func() {
			defer wg.Done()
			interval := discovery.Interval
			if interval <= 0 {
				interval = DefaultDiscoveryInterval
			}
			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}

				fresh, err := discovery.Refresh(ctx)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					// An error that will come back identically forever -- the
					// endpoint was deleted, the credential died -- must end the
					// loop, not just this poll. Observed live: after an endpoint was
					// deleted mid-follow, every worker stream 404'd and ended while
					// this loop kept re-polling, and because it holds a WaitGroup
					// slot the command stayed alive producing nothing at all rather
					// than exiting.
					if api.IsPermanentStreamError(err) {
						notef("no longer looking for new workers: %v", err)
						return
					}
					notef("could not refresh the worker list (%v); still following the workers already found", err)
					continue
				}

				for _, target := range fresh {
					mu.Lock()
					known := seen[target.Path]
					atCap := live >= MaxStreams
					if !known && !atCap {
						seen[target.Path] = true
						live++
					}
					open := live
					mu.Unlock()

					if known {
						continue
					}
					if atCap {
						notef("worker %s appeared but %d streams are already open; not reading it", target.WorkerID, open)
						continue
					}
					notef("worker %s appeared; following it too", target.WorkerID)
					spawn(target)
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(entries)
	}()

	var writeErr error
	for entry := range entries {
		if writeErr != nil {
			// stdout is gone (closed pipe): stop the producers and drain rather
			// than writing into a broken descriptor for the rest of the follow.
			continue
		}
		if err := writer.Write(entry); err != nil {
			writeErr = err
			cancel()
		}
	}

	if writeErr != nil {
		return writeErr
	}

	mu.Lock()
	defer mu.Unlock()
	return combineStreamErrors(streamErrs)
}

// combineStreamErrors decides what a partially failed fan-in means.
//
// One worker of several failing does not fail the command: the surviving streams
// are still the output the user asked for, and a worker disappearing mid-follow is
// ordinary churn. Every target failing is a real failure and is returned, so a bad
// id or a dead credential still exits non-zero with its code intact.
//
// This only picks the return value. The per-worker stderr note is emitted by the
// stream goroutine at the time of failure, so that a follow reports it live rather
// than holding it until the command ends.
func combineStreamErrors(errs []error) error {
	var first error
	failed := 0
	for _, err := range errs {
		if err == nil {
			continue
		}
		failed++
		if first == nil {
			first = err
		}
	}
	if failed == len(errs) {
		return first
	}
	return nil
}

// followNotice builds the stderr reporter for reconnects, naming the worker when
// more than one stream is in play.
func followNotice(target Target) func(string) {
	if target.WorkerID == "" {
		return func(message string) { notef("%s", message) }
	}
	return func(message string) { notef("worker %s: %s", target.WorkerID, message) }
}

// noteMu serializes stderr notes. Reconnect and failure notes are emitted from
// one goroutine per stream, and two concurrent Fprintf calls can interleave
// mid-line, which would produce a note naming one worker and a message from
// another.
var noteMu sync.Mutex

// notef writes a note to stderr. stdout carries the log lines, so nothing
// advisory may go there.
func notef(format string, args ...interface{}) {
	noteMu.Lock()
	defer noteMu.Unlock()
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}
