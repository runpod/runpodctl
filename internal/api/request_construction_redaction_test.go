package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// badSignedURL is an unparseable url carrying a presigned signature in its
// query string; http.NewRequest returns a *url.Error that quotes it verbatim.
const badSignedURL = "https://bad host/x?X-Amz-Signature=deadbeef0123456789"

// assertRedactedURLChain pins that a request-construction error quotes no
// credentials, in the message or in the *url.Error anywhere in its chain.
func assertRedactedURLChain(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected a request-construction error")
	}
	if strings.Contains(err.Error(), "deadbeef") {
		t.Fatalf("error leaks the url query: %v", err)
	}
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		t.Fatalf("expected a *url.Error in the chain, got %T: %v", err, err)
	}
	if strings.Contains(urlErr.URL, "deadbeef") {
		t.Fatalf("the url.Error in the chain still carries the query: %v", urlErr.URL)
	}
}

func TestGraphQLClientMalformedURLErrorIsRedacted(t *testing.T) {
	c := &GraphQLClient{url: badSignedURL, apiKey: "test-key", httpClient: &http.Client{}, userAgent: "test"}
	_, err := c.Query(GraphQLInput{Query: "query test"})
	assertRedactedURLChain(t, err)
}

func TestClientMalformedURLErrorIsRedacted(t *testing.T) {
	c := &Client{baseURL: badSignedURL, apiKey: "test-key", httpClient: &http.Client{}, userAgent: "test"}
	_, err := c.send(context.Background(), http.MethodGet, badSignedURL, nil)
	assertRedactedURLChain(t, err)
	if !errors.Is(err, errRequestConstruction) {
		t.Fatalf("the construction sentinel must survive redaction: %v", err)
	}
}

func TestLogClientMalformedURLErrorIsRedacted(t *testing.T) {
	c := &LogClient{baseURL: badSignedURL, apiKey: "test-key", httpClient: &http.Client{}, userAgent: "test"}
	err := c.stream(context.Background(), "/logs", LogStreamOptions{}, "", func(LogEntry) error { return nil }, nil, nil)
	assertRedactedURLChain(t, err)
}
