package logstream

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/configenv"
	"github.com/runpod/runpodctl/internal/output"
	"github.com/spf13/cobra"
)

// newTestCommand builds a command carrying the shared flags, so Resolve sees the
// same cobra state the real commands give it (Changed("tail") in particular).
func newTestCommand(args ...string) (*cobra.Command, *Flags) {
	flags := &Flags{}
	cmd := &cobra.Command{Use: "logs", RunE: func(*cobra.Command, []string) error { return nil }}
	flags.Register(cmd)
	cmd.SetArgs(args)
	_ = cmd.Flags().Parse(args)
	return cmd, flags
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var coder interface{ ErrorCode() string }
	if !errors.As(err, &coder) {
		t.Fatalf("err %v carries no ErrorCode()", err)
	}
	return coder.ErrorCode()
}

// The default has to be the api's own default, so `pod logs <id>` behaves the
// same as an unflagged request.
func TestResolveDefaultsToTail100(t *testing.T) {
	cmd, flags := newTestCommand()

	opts, err := flags.Resolve(cmd, time.Now())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if opts.Tail == nil || *opts.Tail != 100 {
		t.Errorf("tail = %v, want 100", opts.Tail)
	}
	if opts.Source != "" {
		t.Errorf("source = %q, want empty (both)", opts.Source)
	}
}

// tail=0 means "live only" and must reach the wire as an explicit 0, not be
// mistaken for unset and replaced by the api default of 100.
func TestResolveKeepsExplicitTailZero(t *testing.T) {
	cmd, flags := newTestCommand("--tail", "0")

	opts, err := flags.Resolve(cmd, time.Now())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// the query rendering of this is asserted in the api package, which owns it.
	if opts.Tail == nil || *opts.Tail != 0 {
		t.Fatalf("tail = %v, want an explicit 0", opts.Tail)
	}
}

// The api answers an oversized tail with a bare 422, which names neither the flag
// nor the limit.
func TestResolveRejectsOutOfRangeTail(t *testing.T) {
	for _, value := range []string{"-1", "5001"} {
		cmd, flags := newTestCommand("--tail", value)

		_, err := flags.Resolve(cmd, time.Now())
		if err == nil {
			t.Fatalf("--tail %s was accepted", value)
		}
		if code := codeOf(t, err); code != "usage_error" {
			t.Errorf("--tail %s code = %q, want usage_error", value, code)
		}
	}
}

func TestResolveRejectsUnknownSource(t *testing.T) {
	cmd, flags := newTestCommand("--source", "stdout")

	_, err := flags.Resolve(cmd, time.Now())
	if err == nil {
		t.Fatal("expected --source stdout to be rejected")
	}
	if code := codeOf(t, err); code != "usage_error" {
		t.Errorf("code = %q, want usage_error", code)
	}
}

// "both" is not a wire value: the api returns both sources when the param is
// absent, and sending source=both would be a 422.
func TestResolveOmitsSourceForBoth(t *testing.T) {
	for _, value := range []string{"both", ""} {
		cmd, flags := newTestCommand("--source", value)

		opts, err := flags.Resolve(cmd, time.Now())
		if err != nil {
			t.Fatalf("resolve %q: %v", value, err)
		}
		if opts.Source != "" {
			t.Errorf("source %q produced param %q, want it omitted", value, opts.Source)
		}
	}
}

func TestResolvePassesThroughRealSources(t *testing.T) {
	for _, value := range []string{"container", "system"} {
		cmd, flags := newTestCommand("--source", value)

		opts, err := flags.Resolve(cmd, time.Now())
		if err != nil {
			t.Fatalf("resolve %q: %v", value, err)
		}
		if opts.Source != value {
			t.Errorf("source = %q, want %q", opts.Source, value)
		}
	}
}

// The ask behind this flag was "the last 30 minutes", so a duration has to work
// without the user computing a timestamp.
func TestResolveConvertsRelativeSince(t *testing.T) {
	now := time.Date(2026, 8, 13, 18, 30, 0, 0, time.UTC)
	cmd, flags := newTestCommand("--since", "30m")

	opts, err := flags.Resolve(cmd, now)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if opts.Since != "2026-08-13T18:00:00Z" {
		t.Errorf("since = %q, want 2026-08-13T18:00:00Z", opts.Since)
	}
	// since wins server-side, so tail must not also be sent.
	if opts.Tail != nil {
		t.Errorf("tail = %v, want nil when since is set", *opts.Tail)
	}
}

func TestResolveAcceptsAbsoluteSince(t *testing.T) {
	cmd, flags := newTestCommand("--since", "2026-08-13T18:00:00Z")

	opts, err := flags.Resolve(cmd, time.Now())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if opts.Since != "2026-08-13T18:00:00Z" {
		t.Errorf("since = %q", opts.Since)
	}
}

// A non-utc timestamp has to be normalized, since the api parses rfc3339 but the
// cursor semantics are easier to reason about in utc.
func TestResolveNormalizesSinceToUTC(t *testing.T) {
	cmd, flags := newTestCommand("--since", "2026-08-13T20:00:00+02:00")

	opts, err := flags.Resolve(cmd, time.Now())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if opts.Since != "2026-08-13T18:00:00Z" {
		t.Errorf("since = %q, want the utc equivalent 2026-08-13T18:00:00Z", opts.Since)
	}
}

func TestResolveRejectsMalformedSince(t *testing.T) {
	for _, value := range []string{"yesterday", "notatimestamp", "0m", "-5m"} {
		cmd, flags := newTestCommand("--since", value)

		_, err := flags.Resolve(cmd, time.Now())
		if err == nil {
			t.Fatalf("--since %q was accepted", value)
		}
		if code := codeOf(t, err); code != "usage_error" {
			t.Errorf("--since %q code = %q, want usage_error", value, code)
		}
	}
}

func TestResolveRejectsNonPositiveMaxWait(t *testing.T) {
	cmd, flags := newTestCommand("--max-wait", "0s")

	_, err := flags.Resolve(cmd, time.Now())
	if err == nil {
		t.Fatal("expected --max-wait 0s to be rejected")
	}
	if code := codeOf(t, err); code != "usage_error" {
		t.Errorf("code = %q, want usage_error", code)
	}
}

// A flag that was set and then ignored should say so, the same way the
// --tail/--since overlap does.
func TestResolveNotesMaxWaitIgnoredUnderFollow(t *testing.T) {
	cmd, flags := newTestCommand("--follow", "--max-wait", "9s")

	stderr := captureStderr(t, func() {
		if _, err := flags.Resolve(cmd, time.Now()); err != nil {
			t.Fatalf("resolve: %v", err)
		}
	})

	if !strings.Contains(stderr, "--max-wait is ignored") {
		t.Errorf("expected a note that --max-wait does not apply to a follow, got %q", stderr)
	}
}

// ...and a flag that was not set must not produce a note.
func TestResolveSilentWhenMaxWaitUnset(t *testing.T) {
	cmd, flags := newTestCommand("--follow")

	stderr := captureStderr(t, func() {
		if _, err := flags.Resolve(cmd, time.Now()); err != nil {
			t.Fatalf("resolve: %v", err)
		}
	})

	if strings.Contains(stderr, "--max-wait") {
		t.Errorf("unset --max-wait should produce no note, got %q", stderr)
	}
}

func TestResolveDefaultMaxWait(t *testing.T) {
	cmd, flags := newTestCommand()

	if _, err := flags.Resolve(cmd, time.Now()); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if flags.MaxWait != DefaultMaxWait {
		t.Errorf("max-wait = %s, want %s", flags.MaxWait, DefaultMaxWait)
	}
}

// sseServer serves a fixed number of frames per worker path and then closes,
// so a snapshot over several targets terminates without needing the quiet gap.
func sseServer(t *testing.T, framesPerWorker int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		worker := path.Base(path.Dir(r.URL.Path))
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < framesPerWorker; i++ {
			fmt.Fprintf(w, "id: %s-%d\ndata: {\"source\":\"container\",\"line\":\"%s line %d\",\"ts\":\"t\"}\n\n", worker, i, worker, i)
		}
		w.(http.Flusher).Flush()
	}))
	t.Cleanup(server.Close)
	return server
}

// runFanIn drives Run against a test server and returns what landed on stdout.
func runFanIn(t *testing.T, server *httptest.Server, workers []string) (string, error) {
	t.Helper()
	t.Setenv(configenv.RESTV2URLEnv, server.URL)
	t.Setenv(configenv.APIKeyEnv, "test-key")

	client, err := api.NewLogClient()
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	targets := make([]Target, 0, len(workers))
	for _, worker := range workers {
		targets = append(targets, Target{Path: api.WorkerLogsPath("ep1", worker), WorkerID: worker})
	}

	var runErr error
	out := captureStdout(t, func() {
		runErr = Run(client, targets, api.LogStreamOptions{}, &Flags{MaxWait: 5 * time.Second}, output.FormatJSON, nil)
	})
	return out, runErr
}

// captureStdout swaps os.Stdout for a pipe. Run writes from the goroutine that
// owns the writer, so this also proves records are not interleaved mid-line.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = writer

	collected := make(chan string, 1)
	go func() {
		var buffer bytes.Buffer
		_, _ = buffer.ReadFrom(reader)
		collected <- buffer.String()
	}()

	fn()
	_ = writer.Close()
	os.Stdout = original
	return <-collected
}

// The fan-in is the most concurrency-sensitive part of the change: several
// producers, one writer. Every record from every worker must arrive exactly once
// and as a whole line.
func TestRunFansInWithoutCorruptingRecords(t *testing.T) {
	const framesPerWorker = 40
	workers := []string{"w1", "w2", "w3"}

	out, err := runFanIn(t, sseServer(t, framesPerWorker), workers)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != framesPerWorker*len(workers) {
		t.Fatalf("lines = %d, want %d", len(lines), framesPerWorker*len(workers))
	}

	perWorker := map[string]int{}
	for i, line := range lines {
		var entry api.LogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("line %d is not a whole json record (interleaved write?): %q", i, line)
		}
		if entry.WorkerID == "" {
			t.Errorf("line %d carries no workerId: %q", i, line)
		}
		perWorker[entry.WorkerID]++
	}
	for _, worker := range workers {
		if perWorker[worker] != framesPerWorker {
			t.Errorf("worker %s produced %d records, want %d", worker, perWorker[worker], framesPerWorker)
		}
	}
}

// captureStdoutUntil captures stdout while fn runs, calling stopWhen on each line.
//
// When stopWhen returns true (or the safety timeout fires) the read end is closed,
// which makes the command's next write fail. That is how a --follow is ended
// without sending a signal, and it exercises the closed-stdout path at the same
// time. The timeout is what keeps a broken discovery a failing test rather than a
// hanging one.
func captureStdoutUntil(t *testing.T, timeout time.Duration, stopWhen func(string) bool, fn func()) string {
	t.Helper()
	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = writer

	var closeOnce sync.Once
	closeReader := func() { closeOnce.Do(func() { _ = reader.Close() }) }
	safety := time.AfterFunc(timeout, closeReader)
	defer safety.Stop()

	collected := make(chan string, 1)
	go func() {
		var buffer bytes.Buffer
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := scanner.Text()
			buffer.WriteString(line)
			buffer.WriteString("\n")
			if stopWhen(line) {
				break
			}
		}
		closeReader()
		collected <- buffer.String()
	}()

	fn()
	_ = writer.Close()
	os.Stdout = original
	return <-collected
}

// captureStderr swaps os.Stderr for a pipe. Notes are written from the stream
// goroutines, so this is how their timing is observed.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	original := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = writer

	collected := make(chan string, 1)
	go func() {
		var buffer bytes.Buffer
		_, _ = buffer.ReadFrom(reader)
		collected <- buffer.String()
	}()

	fn()
	_ = writer.Close()
	os.Stderr = original
	return <-collected
}

// Regression: a worker that dies while others keep streaming used to be reported
// only after the whole command ended. Under --follow that is at ctrl-c, so someone
// watching a deploy could sit for minutes without learning a worker had dropped.
//
// The assertion is specifically about *timing*: the note must reach stderr while
// Run is still executing. The surviving worker keeps emitting frames so the quiet
// gap never fires, which keeps Run alive until its own budget expires.
func TestRunReportsWorkerFailureWhileOthersStillStream(t *testing.T) {
	stop := make(chan struct{})
	var stopOnce sync.Once
	closeStop := func() { stopOnce.Do(func() { close(stop) }) }

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "dead") {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"detail":"worker not found","status":404}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; ; i++ {
			select {
			case <-r.Context().Done():
				return
			case <-stop:
				return
			default:
			}
			fmt.Fprintf(w, "id: %d\ndata: {\"source\":\"container\",\"line\":\"alive %d\",\"ts\":\"t\"}\n\n", i, i)
			w.(http.Flusher).Flush()
			time.Sleep(30 * time.Millisecond)
		}
	}))
	t.Cleanup(func() {
		closeStop()
		server.Close()
	})

	t.Setenv(configenv.RESTV2URLEnv, server.URL)
	t.Setenv(configenv.APIKeyEnv, "test-key")
	client, err := api.NewLogClient()
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	targets := []Target{
		{Path: api.WorkerLogsPath("ep1", "dead"), WorkerID: "dead"},
		{Path: api.WorkerLogsPath("ep1", "alive"), WorkerID: "alive"},
	}

	// stderr is scanned as it is written rather than collected at the end, so
	// "the note arrived before Run returned" is actually observable.
	originalStderr := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = writer
	defer func() { os.Stderr = originalStderr }()

	noteSeen := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), "dead") {
				select {
				case noteSeen <- scanner.Text():
				default:
				}
			}
		}
	}()

	runReturned := make(chan struct{})
	go func() {
		defer close(runReturned)
		// stdout is discarded: the surviving worker's frames are not under test.
		_ = captureStdout(t, func() {
			_ = Run(client, targets, api.LogStreamOptions{}, &Flags{MaxWait: 3 * time.Second}, output.FormatJSON, nil)
		})
	}()

	select {
	case note := <-noteSeen:
		select {
		case <-runReturned:
			t.Errorf("note %q only surfaced after Run returned", note)
		default:
		}
		if !strings.Contains(note, "worker not found") {
			t.Errorf("note does not name the cause: %q", note)
		}
	case <-runReturned:
		t.Error("Run finished without ever reporting the failed worker")
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for the failure note")
	}

	closeStop()
	<-runReturned
	_ = writer.Close()
	os.Stderr = originalStderr
}

// A worker that appears after the follow started must be picked up. Resolving the
// set once meant an endpoint scaling from 1 to 3 workers mid-deploy kept showing
// only the first, silently -- the mirror image of not reporting the ones that die.
func TestRunDiscoversWorkersThatAppearMidFollow(t *testing.T) {
	stop := make(chan struct{})
	var stopOnce sync.Once
	closeStop := func() { stopOnce.Do(func() { close(stop) }) }

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		worker := path.Base(path.Dir(r.URL.Path))
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; ; i++ {
			select {
			case <-r.Context().Done():
				return
			case <-stop:
				return
			default:
			}
			fmt.Fprintf(w, "id: %d\ndata: {\"source\":\"container\",\"line\":\"%s says %d\",\"ts\":\"t\"}\n\n", i, worker, i)
			w.(http.Flusher).Flush()
			time.Sleep(20 * time.Millisecond)
		}
	}))
	t.Cleanup(func() {
		closeStop()
		server.Close()
	})

	t.Setenv(configenv.RESTV2URLEnv, server.URL)
	t.Setenv(configenv.APIKeyEnv, "test-key")
	client, err := api.NewLogClient()
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	// the second worker only shows up on the second poll.
	var mu sync.Mutex
	polls := 0
	discovery := &Discovery{
		Interval: 40 * time.Millisecond,
		Refresh: func(context.Context) ([]Target, error) {
			mu.Lock()
			polls++
			second := polls > 1
			mu.Unlock()

			targets := []Target{{Path: api.WorkerLogsPath("ep1", "first"), WorkerID: "first"}}
			if second {
				targets = append(targets, Target{Path: api.WorkerLogsPath("ep1", "late"), WorkerID: "late"})
			}
			return targets, nil
		},
	}

	initial := []Target{{Path: api.WorkerLogsPath("ep1", "first"), WorkerID: "first"}}

	// Run under --follow only ends on a signal, so it is stopped by closing stdout:
	// the write failure cancels the context, which is also the closed-pipe path.
	sawLate := make(chan struct{})
	var sawOnce sync.Once
	out := captureStdoutUntil(t, 20*time.Second, func(line string) bool {
		if strings.Contains(line, "late says") {
			sawOnce.Do(func() { close(sawLate) })
			return true
		}
		return false
	}, func() {
		_ = Run(client, initial, api.LogStreamOptions{}, &Flags{Follow: true}, output.FormatJSON, discovery)
	})

	select {
	case <-sawLate:
	default:
		t.Fatalf("the late worker was never streamed; output was:\n%s", out)
	}
	if !strings.Contains(out, "first says") {
		t.Errorf("the original worker stopped being read:\n%s", out)
	}
	closeStop()
}

// Regression: a permanently failing refresh has to end the discovery loop.
//
// Observed live -- after the endpoint was deleted mid-follow, every worker stream
// 404'd and ended, but the loop kept re-polling a dead endpoint, and because it
// holds a WaitGroup slot the command stayed alive forever emitting nothing.
func TestRunStopsDiscoveringWhenEndpointIsGone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"detail":"worker not found","status":404}`)
	}))
	t.Cleanup(server.Close)

	t.Setenv(configenv.RESTV2URLEnv, server.URL)
	t.Setenv(configenv.APIKeyEnv, "test-key")
	client, err := api.NewLogClient()
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	discovery := &Discovery{
		Interval: 20 * time.Millisecond,
		Refresh: func(context.Context) ([]Target, error) {
			// what a deleted endpoint answers.
			return nil, &api.APIError{Message: "endpoint not found", Status: 404}
		},
	}

	// every stream is fatal and ends immediately, so the only thing that could keep
	// the command alive is the discovery loop.
	targets := []Target{{Path: api.WorkerLogsPath("gone", "w1"), WorkerID: "w1"}}

	// The goroutine is awaited to completion, not just until Run returns:
	// captureStdout restores the os.Stdout global on its way out, and letting that
	// happen after this test ends races with the next test's capture.
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_ = captureStdout(t, func() {
			_ = Run(client, targets, api.LogStreamOptions{}, &Flags{Follow: true}, output.FormatJSON, discovery)
		})
	}()

	select {
	case <-finished:
		// returned rather than hanging: the assertion.
	case <-time.After(20 * time.Second):
		t.Fatal("Run never returned: discovery kept polling a deleted endpoint and held the command open")
	}
}

// A refresh that fails is transient: the endpoint is still there and the streams
// already open are still valid, so the command must keep going rather than die.
func TestRunSurvivesDiscoveryRefreshFailure(t *testing.T) {
	server := sseServer(t, 3)
	t.Setenv(configenv.RESTV2URLEnv, server.URL)
	t.Setenv(configenv.APIKeyEnv, "test-key")
	client, err := api.NewLogClient()
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	discovery := &Discovery{
		Interval: 20 * time.Millisecond,
		Refresh: func(context.Context) ([]Target, error) {
			return nil, errors.New("worker list unavailable")
		},
	}

	targets := []Target{{Path: api.WorkerLogsPath("ep1", "w1"), WorkerID: "w1"}}

	// a snapshot ignores discovery entirely, so this also pins that a failing
	// refresh cannot affect the non-follow path.
	var runErr error
	out := captureStdout(t, func() {
		runErr = Run(client, targets, api.LogStreamOptions{}, &Flags{MaxWait: 2 * time.Second}, output.FormatJSON, discovery)
	})

	if runErr != nil {
		t.Errorf("err = %v, want nil: a failed refresh is not a failed command", runErr)
	}
	if !strings.Contains(out, "w1") {
		t.Errorf("expected the already-open stream to still produce output:\n%s", out)
	}
}

// One worker failing while another still streams is churn, not a failed command.
func TestRunPartialFailureStillPrintsAndSucceeds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "dead") {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"detail":"worker not found","status":404}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "id: 1\ndata: {\"source\":\"container\",\"line\":\"alive\",\"ts\":\"t\"}\n\n")
		w.(http.Flusher).Flush()
	}))
	t.Cleanup(server.Close)

	out, err := runFanIn(t, server, []string{"alive", "dead"})
	if err != nil {
		t.Fatalf("err = %v, want nil while one worker still streams", err)
	}
	if !strings.Contains(out, `"line":"alive"`) {
		t.Errorf("surviving worker's output missing:\n%s", out)
	}
}

// Every worker failing is a real failure, and the code has to survive so an agent
// sees `not_found` rather than a generic cli_error.
func TestRunTotalFailureReturnsCodedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"detail":"worker not found","status":404}`)
	}))
	t.Cleanup(server.Close)

	out, err := runFanIn(t, server, []string{"w1", "w2"})
	if err == nil {
		t.Fatal("expected an error when every stream failed")
	}
	var coder interface{ ErrorCode() string }
	if !errors.As(err, &coder) || coder.ErrorCode() != "not_found" {
		t.Errorf("err = %v, want code not_found", err)
	}
	// stdout is the data channel: a failure puts nothing on it.
	if strings.TrimSpace(out) != "" {
		t.Errorf("stdout should be empty on total failure, got:\n%s", out)
	}
}

// Reading hundreds of streams at once is not viable, and a cap that says nothing
// would read as full coverage.
func TestRunCapsConcurrentStreams(t *testing.T) {
	var mu sync.Mutex
	opened := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		opened++
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "id: 1\ndata: {\"source\":\"container\",\"line\":\"x\",\"ts\":\"t\"}\n\n")
		w.(http.Flusher).Flush()
	}))
	t.Cleanup(server.Close)

	workers := make([]string, MaxStreams+8)
	for i := range workers {
		workers[i] = fmt.Sprintf("w%d", i)
	}

	if _, err := runFanIn(t, server, workers); err != nil {
		t.Fatalf("run: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if opened > MaxStreams {
		t.Errorf("opened %d streams, want at most %d", opened, MaxStreams)
	}
}

func TestCombineStreamErrorsPartialFailureSucceeds(t *testing.T) {
	errs := []error{errors.New("w1 vanished"), nil}

	// one worker of two dying is churn, not a failed command: the other worker's
	// output is still what was asked for.
	if err := combineStreamErrors(errs); err != nil {
		t.Errorf("err = %v, want nil when at least one stream worked", err)
	}
}

func TestCombineStreamErrorsTotalFailureReturnsFirst(t *testing.T) {
	first := &api.APIError{Message: "unauthorized", Status: 401}
	errs := []error{first, errors.New("second")}

	err := combineStreamErrors(errs)
	if !errors.Is(err, error(first)) {
		t.Fatalf("err = %v, want the first error preserved", err)
	}
	// the code has to survive so an agent still sees `unauthorized` rather than a
	// generic cli_error.
	if code := codeOf(t, err); code != "unauthorized" {
		t.Errorf("code = %q, want unauthorized", code)
	}
}

func TestCombineStreamErrorsSingleTargetReturnsError(t *testing.T) {
	sentinel := errors.New("boom")

	if err := combineStreamErrors([]error{sentinel}); !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want %v", err, sentinel)
	}
}
