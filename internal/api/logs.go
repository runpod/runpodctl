package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/runpod/runpodctl/internal/configenv"
)

// This file is the cli's only streaming transport. The log routes
// (GET /v2/pods/{id}/logs and GET /v2/serverless/{id}/workers/{wid}/logs) answer
// with text/event-stream and never close on their own: they replay `tail`
// historical lines and then stay open forever emitting live ones. Every other
// client in this package does io.ReadAll into a []byte, which against these
// routes would block until the pod died, so none of them can be reused here.

const (
	// DefaultRESTV2BaseURL is prod's v2 rest host. Note this is api.runpod.io,
	// not the rest.runpod.io that DefaultBaseURL points at.
	DefaultRESTV2BaseURL = "https://api.runpod.io/v2"

	// LogTailMax is the largest `tail` the api accepts; more is a 422. Rejecting
	// it locally turns an opaque server error into a usage error naming the limit.
	LogTailMax = 5000

	// logSnapshotQuiet is how long a snapshot waits for another line before
	// deciding the backfill has drained. The api gives no end-of-backfill marker,
	// so this gap is the only available signal — replayed history arrives in one
	// burst (hundreds of frames in well under a second), while live output
	// trickles in at whatever rate the container writes.
	logSnapshotQuiet = 750 * time.Millisecond

	// logReconnectMin/Max bound the backoff between follow reconnects.
	logReconnectMin = 500 * time.Millisecond
	logReconnectMax = 15 * time.Second

	// logLineMax and logFrameMax bound how much of one oversized line, and one
	// oversized reassembled frame, is kept in memory. Anything past the cap is
	// drained and dropped, and the entry is marked truncated. These streams never
	// end, so without a ceiling a single pathological line grows unboundedly.
	logLineMax  = 1 << 20 // 1 MiB
	logFrameMax = 4 << 20 // 4 MiB
)

// LogEntry is one frame of a log stream.
//
// Source is "container" (the workload's own stdout/stderr) or "system" (the
// platform's own narration: image pull progress, container create/start). Raw
// holds the payload verbatim when it was not the documented json object, so a
// wire change surfaces as visible text rather than a silently dropped line.
// WorkerID is set only by the serverless fan-in, where lines from several
// workers share one stream and would otherwise be unattributable.
//
// source, line and ts are NOT omitempty: the documented record shape is
// {source,line,ts}, and a container printing a blank line -- ordinary between log
// stanzas -- would otherwise emit a record with no `line` key at all, so
// `jq -r .line` returned null instead of an empty string on a shape callers were
// promised was fixed.
type LogEntry struct {
	Source    string `json:"source"`
	Line      string `json:"line"`
	TS        string `json:"ts"`
	WorkerID  string `json:"workerId,omitempty"`
	Raw       string `json:"raw,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

// LogStreamOptions are the query params the two log routes share.
//
// Tail is a pointer because 0 is meaningful (stream live with no backfill) and
// has to be distinguishable from "not set", which the api defaults to 100.
// Source empty means both sources; the wire enum is only container|system, so
// "both" is expressed by omitting the param.
type LogStreamOptions struct {
	Tail   *int
	Since  string
	Source string
}

// query renders the options as url values, omitting anything unset.
func (o LogStreamOptions) query() url.Values {
	params := url.Values{}
	if o.Tail != nil {
		params.Set("tail", fmt.Sprintf("%d", *o.Tail))
	}
	if o.Since != "" {
		params.Set("since", o.Since)
	}
	if o.Source != "" {
		params.Set("source", o.Source)
	}
	return params
}

// LogClient streams log events off the v2 rest api.
type LogClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	userAgent  string
}

// NewLogClient creates a client for the v2 log routes.
func NewLogClient() (*LogClient, error) {
	apiKey := configenv.APIKey()
	if apiKey == "" {
		return nil, ErrNoCredentials
	}

	return &LogClient{
		baseURL: restV2BaseURL(),
		apiKey:  apiKey,
		// no client-wide timeout, and no per-call deadline either: a --follow
		// stream is meant to outlive any timeout the user has configured. The
		// caller's context is the only thing that ends a read. Note this also
		// means the viper `timeout` key deliberately does not apply here.
		httpClient: &http.Client{},
		userAgent:  buildUserAgent(),
	}, nil
}

// PodLogsPath is the log route for a pod.
func PodLogsPath(podID string) string {
	return "/pods/" + url.PathEscape(podID) + "/logs"
}

// WorkerLogsPath is the log route for one serverless worker.
func WorkerLogsPath(endpointID, workerID string) string {
	return "/serverless/" + url.PathEscape(endpointID) + "/workers/" + url.PathEscape(workerID) + "/logs"
}

// LogSink receives each parsed entry. Returning an error stops the stream and is
// reported to the caller, which is how a broken stdout ends a follow instead of
// spinning forever against a closed pipe.
type LogSink func(LogEntry) error

// Snapshot reads a bounded slice of a log stream and returns.
//
// The api has no "replay and close" mode, so bounding it is the client's job and
// there are two independent bounds. It returns once no line has arrived for
// logSnapshotQuiet (the backfill has drained — the common case, and why a
// snapshot of an idle pod is fast), or once maxWait elapses (a container logging
// faster than the quiet gap would otherwise never produce one). Reaching either
// bound is a normal, successful end: only a failed request or a sink error is
// returned as an error.
func (c *LogClient) Snapshot(ctx context.Context, path string, opts LogStreamOptions, maxWait time.Duration, sink LogSink) error {
	ctx, cancel := context.WithTimeout(ctx, maxWait)
	defer cancel()

	// Deliver entries through a channel so the quiet-gap timer can race against
	// arrivals. The stream goroutine writes, this function reads.
	entries := make(chan LogEntry, 64)
	streamErr := make(chan error, 1)
	// written by the stream goroutine before it sends on streamErr, read only
	// after that receive, so the send/receive pair is the synchronisation point.
	connected := false
	go func() {
		streamErr <- c.stream(ctx, path, opts, "", func(entry LogEntry) error {
			select {
			case entries <- entry:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}, nil, func() { connected = true })
		close(entries)
	}()

	// The quiet gap measures the interval *between* lines, so it cannot be armed
	// until the first one arrives: connecting costs a TLS handshake plus the
	// server's time to first byte (~0.4s against prod), which on its own exceeds
	// the gap and would end every snapshot before it read anything. Until then a
	// nil channel parks that branch of the select and maxWait is the only bound.
	quiet := time.NewTimer(logSnapshotQuiet)
	quiet.Stop()
	defer quiet.Stop()
	var quietC <-chan time.Time

	for {
		select {
		case entry, ok := <-entries:
			if !ok {
				// stream ended on its own; surface whatever ended it.
				return normalizeStreamEnd(<-streamErr, connected)
			}
			if err := sink(entry); err != nil {
				cancel()
				return err
			}
			if !quiet.Stop() {
				// drain a timer that already fired so the reset is honored.
				select {
				case <-quiet.C:
				default:
				}
			}
			quiet.Reset(logSnapshotQuiet)
			quietC = quiet.C
		case <-quietC:
			// backfill drained: a clean end of a snapshot.
			cancel()
			return nil
		case <-ctx.Done():
			// maxWait elapsed, or the caller cancelled. Wait for the stream to
			// report why it stopped: it may have been a real failure that simply
			// took longer than the budget to surface.
			return normalizeStreamEnd(<-streamErr, connected)
		}
	}
}

// Follow streams until the context is cancelled, reconnecting when the server or
// the network drops the connection.
//
// Reconnects resume from the last event id rather than re-issuing the original
// tail/since, so a drop mid-follow neither replays lines already printed nor
// skips lines written while disconnected. The api treats Last-Event-ID as
// exclusive (verified against prod: resuming from a frame's id yields the frame
// *after* it), which is what makes this deduplicate correctly.
//
// notice, when set, is called with a human-readable note before each retry. It
// must not write to stdout: stdout carries the log lines.
func (c *LogClient) Follow(ctx context.Context, path string, opts LogStreamOptions, sink LogSink, notice func(string)) error {
	cursor := ""
	backoff := logReconnectMin

	for {
		err := c.stream(ctx, path, opts, cursor, sink, func(id string) {
			cursor = id
			// a frame arrived, so the connection works: forget earlier failures.
			backoff = logReconnectMin
		}, nil)

		if ctx.Err() != nil {
			// cancelled by the caller (ctrl-c, or a parent deadline): the
			// expected way a follow ends.
			return nil
		}
		// A sink failure is the consumer going away (a closed stdout), not the
		// connection breaking. Reconnecting cannot fix it: it would hot-loop
		// against the api, and every delivered frame resets the backoff so it
		// would not even slow down.
		var sinkFailure *sinkError
		if errors.As(err, &sinkFailure) {
			return sinkFailure.Unwrap()
		}
		if err != nil {
			// A request that the api rejects the same way every time must not be
			// retried, or a typo'd id becomes an infinite loop.
			if IsPermanentStreamError(err) {
				return err
			}
			if notice != nil {
				notice(fmt.Sprintf("log stream interrupted (%v); reconnecting in %s", err, backoff))
			}
		} else if notice != nil {
			// A clean EOF with no error still means the stream ended, which for
			// these routes is not expected.
			notice(fmt.Sprintf("log stream closed by server; reconnecting in %s", backoff))
		}

		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return nil
		}

		backoff *= 2
		if backoff > logReconnectMax {
			backoff = logReconnectMax
		}
	}
}

// stream opens one connection and feeds parsed frames to sink until the stream
// ends or ctx is done. onCursor, when set, is called with each event id so a
// follow can resume from it.
func (c *LogClient) stream(ctx context.Context, path string, opts LogStreamOptions, cursor string, sink LogSink, onCursor func(string), onConnect func()) error {
	target := c.baseURL + path
	if params := opts.query(); len(params) > 0 {
		target += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", c.userAgent)
	if cursor != "" {
		// takes precedence over tail/since server-side, which is why the
		// original options can be left in place on a reconnect.
		req.Header.Set("Last-Event-ID", cursor)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// error bodies are small json documents, so reading to the end is safe
		// here even though the success path must not.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return parseAPIError(body, resp.StatusCode)
	}

	// A 2xx from something that is not this api at all -- a captive portal, a
	// corporate proxy, a load balancer's own error page -- would otherwise be fed
	// to the sse decoder, and every line of that html would land on stdout as a
	// raw entry. Worse under --follow: html carries no `id:` fields, so the resume
	// cursor never advances and the same page is replayed on every reconnect.
	//
	// This is deliberately narrower than rejecting anything unexpected: the Raw
	// fallback in parseLogPayload exists so that a change to the *frame* shape
	// still surfaces the line. The check here separates that ("the api changed
	// what it sends") from "this response did not come from the api". A missing
	// Content-Type is accepted, since that is a header a proxy may strip from an
	// otherwise valid stream.
	if err := checkEventStream(resp.Header.Get("Content-Type")); err != nil {
		return err
	}

	// headers came back with a usable status: the difference between a quiet
	// stream and a host that never answered.
	if onConnect != nil {
		onConnect()
	}

	return decodeLogSSE(resp.Body, sink, onCursor)
}

// checkEventStream reports whether a 2xx response actually carried an sse
// stream. The returned error is deliberately not an *APIError, so
// IsPermanentStreamError treats it as transient: an intermediary answering for
// the api is the same class of fault as the 5xx a follow already reconnects
// through. A snapshot has no retry, so it fails immediately and names what came
// back instead.
func checkEventStream(contentType string) error {
	if contentType == "" {
		return nil
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return fmt.Errorf("log stream returned an unreadable content-type %q; this response did not come from the runpod api", contentType)
	}
	if !strings.EqualFold(mediaType, "text/event-stream") {
		return fmt.Errorf("log stream returned %s instead of text/event-stream; this response did not come from the runpod api (a proxy or captive portal may be answering for it)", mediaType)
	}
	return nil
}

// decodeLogSSE parses an SSE byte stream, calling sink for every data frame.
//
// Frames are separated by a blank line and only `data:` and `id:` are read
// (`event:`, `retry:` and `:` comments are ignored). Prod emits LF-only
// separators today, but \r\n is tolerated because the SSE spec allows it and a
// proxy may rewrite line endings.
func decodeLogSSE(body io.Reader, sink LogSink, onCursor func(string)) error {
	reader := bufio.NewReader(body)
	var data []string
	dataBytes := 0
	id := ""
	truncated := false

	flush := func() error {
		payload := strings.Join(data, "\n")
		data = data[:0]
		dataBytes = 0
		eventID := id
		id = ""
		wasTruncated := truncated
		truncated = false

		// No data field at all (or one carrying nothing) is a keepalive: advance
		// the resume point without emitting a line the user never saw.
		if strings.TrimSpace(payload) == "" {
			if eventID != "" && onCursor != nil {
				onCursor(eventID)
			}
			return nil
		}

		entry := parseLogPayload(payload)
		entry.Truncated = wasTruncated
		if err := sink(entry); err != nil {
			// tagged so Follow can tell a dead consumer from a dropped
			// connection: one is terminal, the other is what it retries.
			return &sinkError{err: err}
		}
		// Only now that the line has actually been delivered. Advancing the
		// cursor first would let a reconnect resume *past* a line that was never
		// printed, silently losing it.
		if eventID != "" && onCursor != nil {
			onCursor(eventID)
		}
		return nil
	}

	for {
		line, lineTruncated, readErr := readLimitedLine(reader, logLineMax)
		if lineTruncated {
			truncated = true
		}
		// A final frame with no trailing newline still has to be delivered, so
		// the line is processed before the error is acted on.
		trimmed := strings.TrimRight(line, "\r\n")

		if trimmed == "" && line != "" {
			if err := flush(); err != nil {
				return err
			}
		} else if trimmed != "" {
			switch {
			case strings.HasPrefix(trimmed, ":"):
				// comment / keepalive
			case strings.HasPrefix(trimmed, "data:"):
				value := sseFieldValue(trimmed, "data:")
				// Cap the reassembled frame too: the per-line cap alone does not
				// bound a stream that sends millions of short data lines with no
				// blank line to end the frame.
				if dataBytes+len(value) > logFrameMax {
					truncated = true
				} else {
					data = append(data, value)
					dataBytes += len(value)
				}
			case strings.HasPrefix(trimmed, "id:"):
				id = sseFieldValue(trimmed, "id:")
			}
		}

		if readErr != nil {
			if err := flush(); err != nil {
				return err
			}
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
}

// readLimitedLine reads one line, keeping at most max bytes of it.
//
// bufio.Reader.ReadString grows without bound, so a container that writes a very
// large payload with no newline (a dumped blob, a \r-only progress bar) made the
// process allocate the whole thing -- measured at ~2x the line size -- with no
// natural end, since these streams never close. ReadSlice bounds each read to the
// reader's buffer instead, so the remainder of an oversized line is drained and
// discarded rather than accumulated.
func readLimitedLine(reader *bufio.Reader, max int) (string, bool, error) {
	var builder strings.Builder
	truncated := false

	for {
		slice, err := reader.ReadSlice('\n')
		if len(slice) > 0 {
			switch {
			case builder.Len() >= max:
				truncated = true
			case builder.Len()+len(slice) > max:
				builder.Write(slice[:max-builder.Len()])
				truncated = true
			default:
				builder.Write(slice)
			}
		}
		// ErrBufferFull means this line is longer than the reader's buffer, not
		// that the stream failed: keep draining until the newline.
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return builder.String(), truncated, err
	}
}

// sseFieldValue strips a field name and the single optional space the spec
// allows after the colon. A second space is part of the value.
func sseFieldValue(line, prefix string) string {
	return strings.TrimPrefix(strings.TrimPrefix(line, prefix), " ")
}

// sinkError marks a failure that came from the caller's sink rather than from the
// stream, so a follow can treat a vanished consumer as terminal.
type sinkError struct{ err error }

func (e *sinkError) Error() string { return e.err.Error() }

func (e *sinkError) Unwrap() error { return e.err }

// parseLogPayload decodes one data payload into an entry. A payload that is not
// the documented json object is preserved verbatim under Raw rather than
// dropped, so an api change is visible instead of silent.
func parseLogPayload(payload string) LogEntry {
	var entry LogEntry
	decoder := json.NewDecoder(strings.NewReader(payload))
	if err := decoder.Decode(&entry); err != nil {
		return LogEntry{Raw: payload}
	}
	// json.Unmarshal accepts bare primitives into a struct only for `null`; any
	// other non-object is an error and is handled above. A `null` payload
	// decodes into the zero entry, which carries no information.
	if entry.Source == "" && entry.Line == "" && entry.TS == "" {
		return LogEntry{Raw: payload}
	}
	return entry
}

// normalizeStreamEnd maps the end of a bounded read to an error or to success.
//
// Only the cancellation Snapshot itself performs may be swallowed. An earlier
// version discarded every non-fatal error once the deadline had passed, which
// meant a server fault that took longer than --max-wait to arrive (a slow 503, a
// host that accepts the connection and never answers) printed nothing, said
// nothing, and exited 0 -- indistinguishable from a pod that simply had no logs,
// and a different exit code for the same fault depending on latency.
//
// connected reports whether response headers ever came back. Without that, "the
// stream was healthy and quiet" and "we never got a reply at all" are the same
// observation: both end with a deadline error and no entries.
func normalizeStreamEnd(err error, connected bool) error {
	if err == nil {
		return nil
	}
	// A non-2xx is the server's own verdict and is never a side effect of our
	// cancellation, so it is reported whenever it arrives.
	if isAPIError(err) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		if !connected {
			// Observed against prod: the api withholds response headers entirely
			// until it has a line to send, so a filter that matches nothing looks
			// identical to an unreachable host -- neither ever answers. Say both,
			// because the caller can act on either and we cannot tell them apart.
			return NewTimeoutError("timed out waiting for the log stream to respond. the api sends nothing until it has a line, so this can also mean no logs match --since/--source, or the pod has not started writing yet")
		}
		// the deliberate end of a bounded read.
		return nil
	}
	return err
}

// isAPIError reports whether err came from a non-2xx response.
func isAPIError(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr)
}

// IsPermanentStreamError reports whether retrying or reconnecting would hit the
// same rejection. It mirrors the reasoning in internal/waitfor: auth and
// not-found are permanent for a given id, while 429/5xx and transport failures
// are the transient conditions a reconnect exists for.
func IsPermanentStreamError(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.HTTPStatus() {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusUnprocessableEntity:
		return true
	}
	switch apiErr.ErrorCode() {
	case "bad_request", "unauthorized", "forbidden", "not_found", "no_credentials":
		return true
	}
	return false
}
