package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	runpod "github.com/runpod/go-sdk/v2"
	"github.com/runpod/go-sdk/v2/lib/types"

	"github.com/runpod/runpodctl/internal/clierr"
)

// sdkPathPrefix is the prefix every path in the SDK's spec carries.
const sdkPathPrefix = "/v2"

// newSDK builds the generated SDK client on the cli's own transport, so the
// requests go where they always went (RUNPOD_API_URL and restApiUrl include the
// /v2 path the SDK adds itself) and retry exactly as before.
//
// BREAKING: a rest api url that does not parse now fails when the client is
// built, naming the url, instead of at the first rest call.
func newSDK(baseURL, apiKey, userAgent string, timeout time.Duration) (*runpod.Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid rest api url %q: %w", baseURL, err)
	}
	basePath := strings.TrimRight(u.Path, "/")
	u.Path, u.RawPath = "", ""

	return runpod.New(
		runpod.WithAPIKey(apiKey),
		runpod.WithUserAgent(userAgent),
		runpod.WithBaseURL(u.String()),
		runpod.WithHTTPClient(&http.Client{
			Timeout:       timeout,
			CheckRedirect: noRedirects,
			Transport:     &restTransport{base: http.DefaultTransport, basePath: basePath},
		}),
	)
}

// restTransport moves the SDK's requests under the configured base path and
// applies the cli's retry policy. The SDK's own retry layer is not used: it
// replays POST and PATCH on a 429, and a rest v2 pod create can answer 429
// after the pod was already made (see retryableStatus).
type restTransport struct {
	base     http.RoundTripper
	basePath string
}

func (t *restTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Path = t.basePath + strings.TrimPrefix(req.URL.Path, sdkPathPrefix)
	if req.URL.RawPath != "" {
		req.URL.RawPath = t.basePath + strings.TrimPrefix(req.URL.RawPath, sdkPathPrefix)
	}

	for attempt := 1; ; attempt++ {
		resp, err := t.base.RoundTrip(req)
		var retry bool
		var header http.Header
		switch {
		case err != nil:
			// a network error may or may not have reached the server
			retry = idempotentMethod(req.Method)
		case retryableStatus(req.Method, resp.StatusCode):
			retry, header = true, resp.Header
		}
		if !retry || !waitToRetry(req.Context(), header, attempt) {
			return resp, err
		}
		if resp != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
		}
	}
}

// sdkRequest makes one generated-client call.
type sdkRequest func(ctx context.Context, sdk *runpod.Client) (*http.Response, error)

// sdkCall runs one generated-client call and decodes a 2xx body into T. Errors
// come out exactly as the hand-written client reported them (APIError codes,
// redirects named, "request failed" for transport errors).
func sdkCall[T any](c *Client, call sdkRequest) (*T, error) {
	body, err := c.sdkSend(call)
	if err != nil {
		return nil, err
	}
	var out T
	if len(body) == 0 {
		return &out, nil
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &out, nil
}

// sdkSend is sdkCall for callers that need the raw 2xx body, e.g. a delete or a
// response the cli checks for emptiness first.
func (c *Client) sdkSend(call sdkRequest) ([]byte, error) {
	ctx, cancel := c.callContext()
	defer cancel()
	return sdkSendContext(ctx, c.sdk, call)
}

// sdkSendContext is sdkSend under the caller's context.
func sdkSendContext(ctx context.Context, sdk *runpod.Client, call sdkRequest) ([]byte, error) {
	resp, err := call(ctx, sdk)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		// redirects are never followed (see noRedirects); name it, or the
		// caller only sees an opaque status or an html body.
		return nil, &APIError{
			Status:  resp.StatusCode,
			Code:    codeForStatus(resp.StatusCode),
			Message: fmt.Sprintf("api redirected (status %d) to %q; redirects are not followed", resp.StatusCode, resp.Header.Get("Location")),
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseAPIError(body, resp.StatusCode)
	}
	return body, nil
}

// sdkAllPages walks a cursor-paginated list. fetch gets the cursor for the next
// page (nil for the first) and returns that page's pagination block; label
// names the list in errors, as the route path did before.
func sdkAllPages(label string, fetch func(cursor *string) (types.Pagination, error)) error {
	seen := map[string]bool{}
	var cursor *string
	for {
		page, err := fetch(cursor)
		if err != nil {
			return err
		}
		if !page.HasNextPage {
			return nil
		}
		next := page.NextCursor
		// more pages but no way to ask for them: stopping here would hand back
		// a partial list as if it were the whole thing
		if next == nil || *next == "" {
			return fmt.Errorf("the api reported more pages of %s but no cursor to fetch them", label)
		}
		// a cursor the api already handed out would loop forever
		if seen[*next] {
			return fmt.Errorf("the api repeated page cursor %q while listing %s", *next, label)
		}
		seen[*next] = true
		cursor = next
	}
}

// withCursor asks for the page after cursor, for list operations whose
// generated client takes no cursor parameter. A nil cursor changes nothing.
func withCursor(cursor *string) func(context.Context, *http.Request) error {
	return func(_ context.Context, req *http.Request) error {
		if cursor != nil {
			q := req.URL.Query()
			q.Set("cursor", *cursor)
			req.URL.RawQuery = q.Encode()
		}
		return nil
	}
}

// nonEmpty is a pointer to s, or nil for an empty s, for the optional list
// fields the hand-written client left out when empty.
func nonEmpty(s []string) *[]string {
	if len(s) == 0 {
		return nil
	}
	return &s
}

// dropEmptyStrings removes the named top-level fields from a JSON request body
// when they are "". A generated request type always sends its required fields,
// where the hand-written client left an empty one out, and the API rejects the
// two differently; dropping it keeps the error a user gets unchanged.
func dropEmptyStrings(fields ...string) func(context.Context, *http.Request) error {
	return func(_ context.Context, req *http.Request) error {
		if req.Body == nil {
			return nil
		}
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			return err
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal(raw, &body); err != nil {
			return err
		}
		for _, f := range fields {
			if string(body[f]) == `""` {
				delete(body, f)
			}
		}
		if raw, err = json.Marshal(body); err != nil {
			return err
		}
		req.Body = io.NopCloser(bytes.NewReader(raw))
		req.ContentLength = int64(len(raw))
		req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil }
		return nil
	}
}

// requireID rejects an id that would leave an empty or dot path segment: v2
// redirects "/registries/" to the collection, so a get would print an empty
// object and a delete would report success without deleting anything.
func requireID(id string) error {
	switch {
	case id == "":
		return clierr.Usagef("a resource id is required")
	case strings.Contains(id, "/") || id == "." || id == "..":
		return clierr.Usagef("invalid resource id %q", id)
	}
	return nil
}
