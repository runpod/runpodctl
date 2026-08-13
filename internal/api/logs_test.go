package api

import (
	"context"
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
			if got := isFatalLogStreamError(testCase.err); got != testCase.want {
				t.Errorf("isFatalLogStreamError(%v) = %v, want %v", testCase.err, got, testCase.want)
			}
		})
	}
}
