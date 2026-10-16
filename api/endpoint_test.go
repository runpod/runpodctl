package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// serveGraphQL points Query at a server that answers every request with body.
func serveGraphQL(t *testing.T, status int, body string) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "test-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("RUNPOD_GRAPHQL_URL", server.URL)
}

// malformedErrorBody is the payload that used to panic the unchecked
// firstErr["message"].(string) assertion.
const malformedErrorBody = `{"errors":[{"message":123}]}`

func TestEndpointMutations_MalformedErrorDoesNotPanic(t *testing.T) {
	calls := map[string]func() error{
		"CreateTemplate": func() error {
			_, err := CreateTemplate(&CreateTemplateInput{Name: "t"})
			return err
		},
		"CreateEndpoint": func() error {
			_, err := CreateEndpoint(&CreateEndpointInput{Name: "e"})
			return err
		},
		"UpdateEndpointTemplate": func() error {
			return UpdateEndpointTemplate("ep", "tpl")
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			serveGraphQL(t, http.StatusOK, malformedErrorBody)
			if err := call(); err == nil {
				t.Error("expected an error for a graphql error payload")
			}
		})
	}
}

func TestEndpointMutations_NonOKKeepsServerMessage(t *testing.T) {
	serveGraphQL(t, http.StatusUnauthorized, `{"error":"bad key"}`)
	_, err := CreateEndpoint(&CreateEndpointInput{Name: "e"})
	if err == nil || !strings.Contains(err.Error(), "statuscode 401") || !strings.Contains(err.Error(), "bad key") {
		t.Errorf("expected status and server message in error, got %v", err)
	}
}

func TestEndpointMutations_NonStringIDDoesNotPanic(t *testing.T) {
	t.Run("CreateTemplate", func(t *testing.T) {
		serveGraphQL(t, http.StatusOK, `{"data":{"saveTemplate":{"id":7}}}`)
		if _, err := CreateTemplate(&CreateTemplateInput{Name: "t"}); err == nil {
			t.Error("expected error for non-string template id")
		}
	})
	t.Run("CreateEndpoint", func(t *testing.T) {
		serveGraphQL(t, http.StatusOK, `{"data":{"saveEndpoint":{"id":7}}}`)
		if _, err := CreateEndpoint(&CreateEndpointInput{Name: "e"}); err == nil {
			t.Error("expected error for non-string endpoint id")
		}
	})
}

func TestEndpointMutations_Success(t *testing.T) {
	serveGraphQL(t, http.StatusOK, `{"data":{"saveEndpoint":{"id":"ep1"}}}`)
	id, err := CreateEndpoint(&CreateEndpointInput{Name: "e"})
	if err != nil || id != "ep1" {
		t.Errorf("got (%q, %v), want (ep1, nil)", id, err)
	}
}
