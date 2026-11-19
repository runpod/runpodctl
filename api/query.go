package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/runpod/runpodctl/internal/agent"
	internalapi "github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/configenv"
	"github.com/spf13/viper"
)

// ErrNoCredentials is the "no api key configured" sentinel, re-exported from
// internal/api so callers outside this module can match it with errors.Is
// instead of string-matching the message. It is the SAME value the rest client
// returns, so one errors.Is check covers every path.
var ErrNoCredentials = internalapi.ErrNoCredentials

type Input struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

const GraphQLTimeoutKey = "graphqlTimeout"
const DefaultGraphQLURL = "https://api.runpod.io/graphql"

func Query(input Input) (res *http.Response, err error) {
	if input.Variables == nil {
		input.Variables = map[string]interface{}{}
	}

	jsonValue, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}

	apiUrl := configenv.GraphQLURL()
	if apiUrl == "" {
		apiUrl = DefaultGraphQLURL
	}

	apiKey := configenv.APIKey()

	// Check if the API key is present
	if apiKey == "" {
		// no print: the error is returned and the caller emits it on stderr.
		// this used to print to *stdout*, corrupting the json contract for
		// anything piping runpodctl output into a parser. shares the typed
		// sentinel with the rest client so both report code no_credentials.
		return nil, ErrNoCredentials
	}

	req, err := http.NewRequest("POST", apiUrl, bytes.NewBuffer(jsonValue))
	if err != nil {
		return
	}

	sanitizedVersion := strings.TrimRight(Version, "\r\n")
	userAgent := "RunPod-CLI/" + sanitizedVersion + " (" + runtime.GOOS + "; " + runtime.GOARCH + ")" + agent.Suffix()

	req.Header.Add("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Authorization", "Bearer "+apiKey)

	timeout := viper.GetDuration(GraphQLTimeoutKey)
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	client := &http.Client{Timeout: timeout, CheckRedirect: refuseRedirect}
	return client.Do(req)
}

// ErrRedirectRefused is returned (wrapped in a *url.Error by net/http) when the
// graphql endpoint answers with a redirect. The client never follows one: it
// carries the api key and a mutation body, and go would replay the request to
// the new location (same-host-other-port forwards the Authorization header) or
// downgrade a post to a get and report success. Matches the rest client, which
// stops at the first response.
var ErrRedirectRefused = errors.New("graphql endpoint responded with a redirect; refusing to follow it (check the configured api url)")

// refuseRedirect fails the request instead of following a redirect. Returning an
// error (rather than http.ErrUseLastResponse) means every caller of Query sees a
// failure and none can mistake the 3xx for a result.
func refuseRedirect(*http.Request, []*http.Request) error {
	return ErrRedirectRefused
}
