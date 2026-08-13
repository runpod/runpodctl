package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
)

// LogEntry is one frame of a log stream.
//
// Source is "container" (the workload's own stdout/stderr) or "system" (the
// platform's own narration: image pull progress, container create/start). Raw
// holds the payload verbatim when it was not the documented json object, so a
// wire change surfaces as visible text rather than a silently dropped line.
// WorkerID is set only by the serverless fan-in, where lines from several
// workers share one stream and would otherwise be unattributable.
type LogEntry struct {
	Source   string `json:"source,omitempty"`
	Line     string `json:"line,omitempty"`
	TS       string `json:"ts,omitempty"`
	WorkerID string `json:"workerId,omitempty"`
	Raw      string `json:"raw,omitempty"`
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
	go func() {
		streamErr <- c.stream(ctx, path, opts, "", func(entry LogEntry) error {
			select {
			case entries <- entry:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}, nil)
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
				return normalizeStreamEnd(ctx, <-streamErr)
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
			// maxWait elapsed, or the caller cancelled. Either way the request
			// itself did not fail, so report success unless the stream did.
			return normalizeStreamEnd(ctx, <-streamErr)
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
		})

		if ctx.Err() != nil {
			// cancelled by the caller (ctrl-c, or a parent deadline): the
			// expected way a follow ends.
			return nil
		}
		if err != nil {
			// A request that the api rejects the same way every time must not be
			// retried, or a typo'd id becomes an infinite loop.
			if isFatalLogStreamError(err) {
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
func (c *LogClient) stream(ctx context.Context, path string, opts LogStreamOptions, cursor string, sink LogSink, onCursor func(string)) error {
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

	return decodeLogSSE(resp.Body, sink, onCursor)
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
	id := ""

	flush := func() error {
		if len(data) == 0 {
			// an id with no data is a keepalive cursor, not a log line: advance
			// the resume point without emitting anything.
			if id != "" && onCursor != nil {
				onCursor(id)
			}
			id = ""
			return nil
		}
		payload := strings.Join(data, "\n")
		data = data[:0]
		eventID := id
		id = ""
		if strings.TrimSpace(payload) == "" {
			return nil
		}
		entry := parseLogPayload(payload)
		if eventID != "" && onCursor != nil {
			onCursor(eventID)
		}
		return sink(entry)
	}

	for {
		line, readErr := reader.ReadString('\n')
		// A final frame with no trailing newline still has to be delivered, so
		// the line is processed before the error is acted on.
		trimmed := strings.TrimRight(line, "\r\n")

		if trimmed == "" && line != "" || (readErr != nil && trimmed == "") {
			if err := flush(); err != nil {
				return err
			}
		} else if trimmed != "" {
			switch {
			case strings.HasPrefix(trimmed, ":"):
				// comment / keepalive
			case strings.HasPrefix(trimmed, "data:"):
				data = append(data, sseFieldValue(trimmed, "data:"))
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

// sseFieldValue strips a field name and the single optional space the spec
// allows after the colon. A second space is part of the value.
func sseFieldValue(line, prefix string) string {
	return strings.TrimPrefix(strings.TrimPrefix(line, prefix), " ")
}

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
// A context cancellation is how Snapshot stops the stream deliberately, so the
// resulting transport error is not a failure of the command.
func normalizeStreamEnd(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil && !isFatalLogStreamError(err) {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	return err
}

// isFatalLogStreamError reports whether retrying or reconnecting would hit the
// same rejection. It mirrors the reasoning in internal/waitfor: auth and
// not-found are permanent for a given id, while 429/5xx and transport failures
// are the transient conditions a reconnect exists for.
func isFatalLogStreamError(err error) bool {
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
