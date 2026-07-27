package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/runpod/runpodctl/internal/agent"
	internalapi "github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/configenv"
	"github.com/spf13/viper"
)

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
		return nil, internalapi.ErrNoCredentials
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

	client := &http.Client{Timeout: timeout}
	return client.Do(req)
}
