package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// collect runs the decoder over a literal stream and returns what a sink saw.
func collect(t *testing.T, stream string) ([]LogEntry, []string) {
	t.Helper()
	var entries []LogEntry
	var cursors []string
	err := decodeLogSSE(strings.NewReader(stream), func(entry LogEntry) error {
		entries = append(entries, entry)
		return nil
	}, func(id string) { cursors = append(cursors, id) })
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return entries, cursors
}

// The exact bytes prod emits, captured off GET /v2/pods/{id}/logs: an `id:` line
// carrying an rfc3339 cursor, a `data:` line carrying the frame, lf-only line
// endings, and one space after each colon.
func TestDecodeLogSSEParsesProdFrames(t *testing.T) {
	stream := "id: 2026-08-13T18:05:46Z\n" +
		"data: {\"source\":\"system\",\"line\":\"create container alpine:latest\",\"ts\":\"2026-08-13T18:05:46Z\"}\n" +
		"\n" +
		"id: 2026-08-13T18:06:15.101277472Z\n" +
		"data: {\"source\":\"container\",\"line\":\"con693 log line 14\",\"ts\":\"2026-08-13T18:06:15.101277472Z\"}\n" +
		"\n"

	entries, cursors := collect(t, stream)

	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2: %+v", len(entries), entries)
	}
	if entries[0].Source != "system" || entries[0].Line != "create container alpine:latest" {
		t.Errorf("first entry = %+v", entries[0])
	}
	if entries[1].Source != "container" || entries[1].Line != "con693 log line 14" {
		t.Errorf("second entry = %+v", entries[1])
	}
	// the cursor is what a reconnect resumes from, so the last one must be the
	// most recent frame's id.
	if len(cursors) != 2 || cursors[1] != "2026-08-13T18:06:15.101277472Z" {
		t.Errorf("cursors = %v", cursors)
	}
}

func TestDecodeLogSSEToleratesCRLF(t *testing.T) {
	stream := "id: 1\r\ndata: {\"line\":\"a\"}\r\n\r\n"

	entries, cursors := collect(t, stream)

	if len(entries) != 1 || entries[0].Line != "a" {
		t.Fatalf("entries = %+v", entries)
	}
	// a \r left on the value would poison the Last-Event-ID header on reconnect.
	if len(cursors) != 1 || cursors[0] != "1" {
		t.Errorf("cursors = %q, want [1]", cursors)
	}
}

func TestDecodeLogSSEIgnoresCommentsAndUnknownFields(t *testing.T) {
	stream := ": keepalive\n" +
		"event: message\n" +
		"retry: 3000\n" +
		"data: {\"line\":\"kept\"}\n" +
		"\n"

	entries, _ := collect(t, stream)

	if len(entries) != 1 || entries[0].Line != "kept" {
		t.Fatalf("entries = %+v", entries)
	}
}

// A byte-level truncation mid-frame is what a dropped connection looks like. The
// partial line still has to be delivered rather than swallowed, because the
// alternative is silently losing the last thing a dying container said.
func TestDecodeLogSSEFlushesFinalFrameWithoutTrailingBlankLine(t *testing.T) {
	stream := "id: 9\ndata: {\"line\":\"last gasp\"}"

	entries, cursors := collect(t, stream)

	if len(entries) != 1 || entries[0].Line != "last gasp" {
		t.Fatalf("entries = %+v", entries)
	}
	if len(cursors) != 1 || cursors[0] != "9" {
		t.Errorf("cursors = %v", cursors)
	}
}

// A multi-line data field is one logical frame per the sse spec, so the payload
// has to be reassembled before it is decoded. Split across a json token boundary
// here: a literal newline *inside* a json string would be invalid json and would
// (correctly) fall through to Raw, which would not prove the join happened.
func TestDecodeLogSSEJoinsMultiLineData(t *testing.T) {
	stream := "data: {\"line\":\n" +
		"data: \"split payload\"}\n" +
		"\n"

	entries, _ := collect(t, stream)

	if len(entries) != 1 {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[0].Line != "split payload" {
		t.Errorf("line = %q, want %q (raw = %q)", entries[0].Line, "split payload", entries[0].Raw)
	}
}

// A payload that is not the documented object must stay visible as text: dropping
// it would make an api change look like an idle pod.
func TestDecodeLogSSEKeepsNonJSONPayloadAsRaw(t *testing.T) {
	stream := "data: not json at all\n\ndata: 42\n\ndata: null\n\n"

	entries, _ := collect(t, stream)

	if len(entries) != 3 {
		t.Fatalf("entries = %d: %+v", len(entries), entries)
	}
	for i, want := range []string{"not json at all", "42", "null"} {
		if entries[i].Raw != want {
			t.Errorf("entry %d raw = %q, want %q", i, entries[i].Raw, want)
		}
		if entries[i].Line != "" {
			t.Errorf("entry %d should carry no line: %+v", i, entries[i])
		}
	}
}

// An id with no data is a cursor-only keepalive: it advances the resume point
// without emitting a log line the user never saw.
func TestDecodeLogSSEAdvancesCursorOnEmptyEvent(t *testing.T) {
	stream := "id: cursor-only\n\ndata: {\"line\":\"real\"}\n\n"

	entries, cursors := collect(t, stream)

	if len(entries) != 1 || entries[0].Line != "real" {
		t.Fatalf("entries = %+v", entries)
	}
	if len(cursors) != 1 || cursors[0] != "cursor-only" {
		t.Errorf("cursors = %v, want [cursor-only]", cursors)
	}
}

func TestDecodeLogSSEStopsOnSinkError(t *testing.T) {
	sentinel := errors.New("stdout closed")
	calls := 0

	err := decodeLogSSE(strings.NewReader("data: {\"line\":\"a\"}\n\ndata: {\"line\":\"b\"}\n\n"), func(LogEntry) error {
		calls++
		return sentinel
	}, nil)

	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want %v", err, sentinel)
	}
	if calls != 1 {
		t.Errorf("sink called %d times, want 1 (should stop at the first error)", calls)
	}
}

func TestLogStreamOptionsQuery(t *testing.T) {
	zero := 0
	hundred := 100

	cases := []struct {
		name string
		opts LogStreamOptions
		want string
	}{
		// tail=0 must survive as an explicit param: it is "no backfill", and
		// omitting it would silently get the api default of 100.
		{"tail zero is explicit", LogStreamOptions{Tail: &zero}, "tail=0"},
		{"tail set", LogStreamOptions{Tail: &hundred}, "tail=100"},
		{"source omitted for both", LogStreamOptions{}, ""},
		{"source set", LogStreamOptions{Source: "container"}, "source=container"},
		{"since set", LogStreamOptions{Since: "2026-08-13T18:00:00Z"}, "since=2026-08-13T18%3A00%3A00Z"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := testCase.opts.query().Encode(); got != testCase.want {
				t.Errorf("query = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestLogPathsEscapeIDs(t *testing.T) {
	if got := PodLogsPath("abc123"); got != "/pods/abc123/logs" {
		t.Errorf("pod path = %q", got)
	}
	if got := WorkerLogsPath("ep1", "w1"); got != "/serverless/ep1/workers/w1/logs" {
		t.Errorf("worker path = %q", got)
	}
	// an id is user input and lands in the path, so it must not be able to change
	// which route is called.
	if got := PodLogsPath("../../evil"); strings.Contains(got, "../") {
		t.Errorf("path traversal not escaped: %q", got)
	}
}

// newTestLogClient points a LogClient at a test server.
func newTestLogClient(baseURL string) *LogClient {
	return &LogClient{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		apiKey:     "test-key",
		httpClient: &http.Client{},
		userAgent:  "test",
	}
}

// Snapshot must end on the quiet gap rather than hanging on a stream the server
// never closes -- which is every real log stream.
func TestSnapshotReturnsWhenBackfillDrains(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 3; i++ {
			fmt.Fprintf(w, "id: %d\ndata: {\"line\":\"line %d\"}\n\n", i, i)
		}
		w.(http.Flusher).Flush()
		// hold the connection open, as the real endpoint does, and emit nothing.
		<-r.Context().Done()
	}))
	defer server.Close()

	var got []LogEntry
	start := time.Now()
	err := newTestLogClient(server.URL).Snapshot(context.Background(), "/pods/p/logs", LogStreamOptions{}, 10*time.Second, func(entry LogEntry) error {
		got = append(got, entry)
		return nil
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("entries = %d, want 3", len(got))
	}
	// it must not have waited for the 10s ceiling.
	if elapsed > 5*time.Second {
		t.Errorf("took %s: should have returned on the quiet gap, not the ceiling", elapsed)
	}
}

// Regression: the quiet gap must not be armed before the first line arrives.
// Connecting to prod costs a tls handshake plus ~0.4s to first byte, which on its
// own exceeded the gap -- every snapshot returned zero lines and exit 0, which
// reads as "this pod has no logs" rather than "the client gave up too early".
func TestSnapshotWaitsForSlowFirstByte(t *testing.T) {
	firstByteDelay := 3 * logSnapshotQuiet

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		time.Sleep(firstByteDelay)
		fmt.Fprint(w, "id: 1\ndata: {\"line\":\"arrived late\"}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	var got []LogEntry
	err := newTestLogClient(server.URL).Snapshot(context.Background(), "/pods/p/logs", LogStreamOptions{}, 10*time.Second, func(entry LogEntry) error {
		got = append(got, entry)
		return nil
	})

	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(got) != 1 || got[0].Line != "arrived late" {
		t.Fatalf("entries = %+v, want the line that arrived after the quiet gap", got)
	}
}

// A container that logs faster than the quiet gap must still not turn a
// non-follow snapshot into an endless stream.
func TestSnapshotStopsAtMaxWait(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; ; i++ {
			select {
			case <-r.Context().Done():
				return
			default:
			}
			fmt.Fprintf(w, "id: %d\ndata: {\"line\":\"chatty %d\"}\n\n", i, i)
			w.(http.Flusher).Flush()
			time.Sleep(20 * time.Millisecond)
		}
	}))
	defer server.Close()

	count := 0
	start := time.Now()
	err := newTestLogClient(server.URL).Snapshot(context.Background(), "/pods/p/logs", LogStreamOptions{}, 700*time.Millisecond, func(LogEntry) error {
		count++
		return nil
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if count == 0 {
		t.Fatal("expected some entries")
	}
	if elapsed > 3*time.Second {
		t.Errorf("took %s, want ~700ms: max-wait did not bound the read", elapsed)
	}
}

func TestSnapshotReturnsAPIErrorWithCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		// the shape prod actually returns.
		fmt.Fprint(w, `{"detail":"pod not found","status":404,"title":"Not Found"}`)
	}))
	defer server.Close()

	err := newTestLogClient(server.URL).Snapshot(context.Background(), "/pods/nope/logs", LogStreamOptions{}, time.Second, func(LogEntry) error {
		return nil
	})

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v (%T), want *APIError", err, err)
	}
	if apiErr.Error() != "pod not found" {
		t.Errorf("message = %q, want the unwrapped detail", apiErr.Error())
	}
	if apiErr.ErrorCode() != "not_found" {
		t.Errorf("code = %q, want not_found", apiErr.ErrorCode())
	}
}

// The reconnect contract: resume from the last event id, and do not re-send the
// original tail (which would replay lines the user already saw).
func TestFollowReconnectsFromLastEventID(t *testing.T) {
	var mu sync.Mutex
	var seenCursors []string
	var seenTails []string
	attempts := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		attempt := attempts
		seenCursors = append(seenCursors, r.Header.Get("Last-Event-ID"))
		seenTails = append(seenTails, r.URL.Query().Get("tail"))
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "id: cursor-%d\ndata: {\"line\":\"from attempt %d\"}\n\n", attempt, attempt)
		w.(http.Flusher).Flush()
		// drop the connection, which is what Follow has to survive.
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var lines []string
	var notices int
	tail := 100
	err := newTestLogClient(server.URL).Follow(ctx, "/pods/p/logs", LogStreamOptions{Tail: &tail}, func(entry LogEntry) error {
		lines = append(lines, entry.Line)
		if len(lines) == 3 {
			// three connections is enough to prove it reconnects and advances.
			cancel()
		}
		return nil
	}, func(string) { notices++ })

	if err != nil {
		t.Fatalf("follow: %v", err)
	}
	if len(lines) < 3 {
		t.Fatalf("lines = %v, want at least 3 across reconnects", lines)
	}

	mu.Lock()
	defer mu.Unlock()
	if seenCursors[0] != "" {
		t.Errorf("first attempt sent Last-Event-ID %q, want empty", seenCursors[0])
	}
	if seenCursors[1] != "cursor-1" || seenCursors[2] != "cursor-2" {
		t.Errorf("reconnect cursors = %v, want [_ cursor-1 cursor-2]", seenCursors)
	}
	// tail stays on the query string; the server ignores it when Last-Event-ID is
	// present, which is why leaving it is safe and not a duplicate-replay bug.
	if seenTails[1] != "100" {
		t.Errorf("tail on reconnect = %q", seenTails[1])
	}
	if notices == 0 {
		t.Error("expected a reconnect notice on stderr")
	}
}

// A 404 is the same answer every time, so retrying it is an infinite loop against
// a typo'd id.
func TestFollowDoesNotRetryFatalErrors(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"detail":"pod not found","status":404}`)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := newTestLogClient(server.URL).Follow(ctx, "/pods/nope/logs", LogStreamOptions{}, func(LogEntry) error {
		return nil
	}, nil)

	if err == nil {
		t.Fatal("expected the 404 to be returned, not retried")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
}

// Ctrl-c during a follow is the normal way out and must not look like a failure.
func TestFollowReturnsNilOnContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "id: 1\ndata: {\"line\":\"a\"}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	err := newTestLogClient(server.URL).Follow(ctx, "/pods/p/logs", LogStreamOptions{}, func(LogEntry) error {
		cancel()
		return nil
	}, nil)

	if err != nil {
		t.Errorf("err = %v, want nil after cancel", err)
	}
}

// Regression: a slow server fault used to be discarded whenever the deadline had
// also passed, so a degraded log store printed nothing and exited 0. A 5xx is the
// server's own verdict and is never a side effect of our own cancellation, so it
// is reported however long it took to arrive.
func TestSnapshotReportsSlowServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"detail":"log store unavailable","status":503}`)
	}))
	defer server.Close()

	err := newTestLogClient(server.URL).Snapshot(context.Background(), "/pods/p/logs", LogStreamOptions{}, 5*time.Second, func(LogEntry) error {
		return nil
	})

	if err == nil {
		t.Fatal("the 503 was swallowed; the command would exit 0 with no output")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.HTTPStatus() != http.StatusServiceUnavailable {
		t.Fatalf("err = %v, want the 503 preserved", err)
	}
	if apiErr.ErrorCode() != "server_error" {
		t.Errorf("code = %q, want server_error", apiErr.ErrorCode())
	}
}

// Regression: a host that accepts the connection and never replies is not the
// same as a healthy stream that had nothing to say, and must not exit 0 silently.
func TestSnapshotReportsTimeoutWhenNeverConnected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	err := newTestLogClient(server.URL).Snapshot(context.Background(), "/pods/p/logs", LogStreamOptions{}, 200*time.Millisecond, func(LogEntry) error {
		return nil
	})

	if err == nil {
		t.Fatal("a host that never answered reported success")
	}
	var coder interface{ ErrorCode() string }
	if !errors.As(err, &coder) || coder.ErrorCode() != "timeout" {
		t.Errorf("err = %v, want a timeout code", err)
	}
}

// The converse: a stream that connects, says nothing and is cut off by the budget
// is a legitimately empty snapshot, not a failure.
func TestSnapshotSucceedsWhenConnectedButQuiet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	err := newTestLogClient(server.URL).Snapshot(context.Background(), "/pods/p/logs", LogStreamOptions{}, 200*time.Millisecond, func(LogEntry) error {
		return nil
	})

	if err != nil {
		t.Errorf("err = %v, want nil: the stream was healthy and simply quiet", err)
	}
}

// Regression: a sink failure is the consumer going away, which reconnecting
// cannot fix. It used to take the retry branch and hot-loop against the api --
// and because a delivered frame resets the backoff, it never even slowed down.
func TestFollowStopsOnSinkError(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "id: 1\ndata: {\"line\":\"a\"}\n\n")
		w.(http.Flusher).Flush()
	}))
	defer server.Close()

	sentinel := errors.New("stdout closed")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := newTestLogClient(server.URL).Follow(ctx, "/pods/p/logs", LogStreamOptions{}, func(LogEntry) error {
		return sentinel
	}, nil)

	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the sink error returned", err)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1: a dead consumer must not be retried", attempts)
	}
}

// Regression: the cursor used to advance before the line was handed to the sink,
// so a failed delivery moved the resume point past a line that was never printed.
func TestCursorAdvancesOnlyAfterDelivery(t *testing.T) {
	var cursors []string

	err := decodeLogSSE(strings.NewReader("id: c1\ndata: {\"line\":\"a\"}\n\n"), func(LogEntry) error {
		return errors.New("sink down")
	}, func(id string) { cursors = append(cursors, id) })

	if err == nil {
		t.Fatal("expected the sink error to propagate")
	}
	if len(cursors) != 0 {
		t.Errorf("cursors = %v, want none: nothing was delivered", cursors)
	}
}

// A sink error has to stay matchable through the wrapper Follow uses to classify
// it, or callers lose the cause.
func TestSinkErrorUnwraps(t *testing.T) {
	sentinel := errors.New("broken pipe")

	err := decodeLogSSE(strings.NewReader("data: {\"line\":\"a\"}\n\n"), func(LogEntry) error {
		return sentinel
	}, nil)

	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want it to unwrap to %v", err, sentinel)
	}
}

// Regression: ReadString grew without bound, so one newline-less line let a
// container drive the cli's memory to roughly twice the line size -- with no
// natural end, because these streams never close.
func TestDecodeLogSSECapsOversizedLine(t *testing.T) {
	huge := strings.Repeat("x", 4*logLineMax)

	var entries []LogEntry
	err := decodeLogSSE(strings.NewReader("data: "+huge+"\n\n"), func(entry LogEntry) error {
		entries = append(entries, entry)
		return nil
	}, nil)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if len(entries[0].Raw) > logLineMax {
		t.Errorf("kept %d bytes, want at most the %d cap", len(entries[0].Raw), logLineMax)
	}
	if !entries[0].Truncated {
		t.Error("an oversized line must be marked truncated, not silently shortened")
	}
}

// The frame cap bounds the other shape of the same attack: many short data lines
// with no blank line to end the frame.
func TestDecodeLogSSECapsOversizedFrame(t *testing.T) {
	var builder strings.Builder
	chunk := strings.Repeat("y", 4096)
	for builder.Len() < 3*logFrameMax {
		builder.WriteString("data: " + chunk + "\n")
	}
	builder.WriteString("\n")

	var entries []LogEntry
	if err := decodeLogSSE(strings.NewReader(builder.String()), func(entry LogEntry) error {
		entries = append(entries, entry)
		return nil
	}, nil); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if len(entries[0].Raw) > logFrameMax+len(chunk) {
		t.Errorf("frame kept %d bytes, want bounded near the %d cap", len(entries[0].Raw), logFrameMax)
	}
	if !entries[0].Truncated {
		t.Error("an oversized frame must be marked truncated")
	}
}

// Regression: `line` is part of the documented record shape, so a container
// printing a blank line must still emit the key rather than dropping it and
// turning `jq -r .line` into null.
func TestBlankLineKeepsDocumentedShape(t *testing.T) {
	entries, _ := collect(t, "data: {\"source\":\"container\",\"line\":\"\",\"ts\":\"2026-08-13T18:00:00Z\"}\n\n")
	if len(entries) != 1 {
		t.Fatalf("entries = %+v", entries)
	}

	encoded, err := json.Marshal(entries[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"source", "line", "ts"} {
		if _, present := decoded[key]; !present {
			t.Errorf("key %q missing from %s", key, encoded)
		}
	}
	// the optional ones stay optional.
	for _, key := range []string{"workerId", "raw", "truncated"} {
		if _, present := decoded[key]; present {
			t.Errorf("key %q should be omitted when empty: %s", key, encoded)
		}
	}
}

func TestIsFatalLogStreamError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"404 is fatal", &APIError{Status: 404}, true},
		{"401 is fatal", &APIError{Status: 401}, true},
		{"422 is fatal", &APIError{Status: 422}, true},
		{"no credentials is fatal", ErrNoCredentials, true},
		{"429 is transient", &APIError{Status: 429}, false},
		{"500 is transient", &APIError{Status: 500}, false},
		{"transport failure is transient", errors.New("connection reset"), false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := IsPermanentStreamError(testCase.err); got != testCase.want {
				t.Errorf("IsPermanentStreamError(%v) = %v, want %v", testCase.err, got, testCase.want)
			}
		})
	}
}

// Regression: a 2xx that is not an sse stream must not be decoded as one.
//
// A captive portal, a corporate proxy or a load balancer's own error page answers
// 200 with html. Fed to the decoder, every line of that page landed on stdout as a
// raw entry -- and since html carries no `id:` fields the resume cursor never
// advanced, so under --follow the same page was replayed on every reconnect.
func TestStreamRejectsNonEventStreamSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<html><body>sign in to continue</body></html>")
	}))
	defer server.Close()

	var got []LogEntry
	err := newTestLogClient(server.URL).Snapshot(context.Background(), "/pods/p/logs", LogStreamOptions{}, 5*time.Second, func(entry LogEntry) error {
		got = append(got, entry)
		return nil
	})

	if err == nil {
		t.Fatalf("snapshot succeeded on an html response; entries = %+v", got)
	}
	if len(got) != 0 {
		t.Errorf("emitted %d entries from an html body, want none: %+v", len(got), got)
	}
	if !strings.Contains(err.Error(), "text/html") {
		t.Errorf("error = %v, want it to name what came back instead", err)
	}
}

func TestCheckEventStreamAcceptsRealStreams(t *testing.T) {
	cases := []struct {
		contentType string
		wantErr     bool
	}{
		{"text/event-stream", false},
		{"text/event-stream; charset=utf-8", false},
		{"Text/Event-Stream", false},
		// a proxy may strip the header from an otherwise valid stream, so a
		// missing content-type is not treated as a wrong one.
		{"", false},
		{"text/html", true},
		{"application/json", true},
		{"text/event-stream;;", true},
	}

	for _, tc := range cases {
		err := checkEventStream(tc.contentType)
		if (err != nil) != tc.wantErr {
			t.Errorf("checkEventStream(%q) = %v, wantErr %v", tc.contentType, err, tc.wantErr)
		}
	}
}
