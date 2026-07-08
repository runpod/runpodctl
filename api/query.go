package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/runpod/runpodctl/internal/agent"
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
		fmt.Println("API key not found")
		return nil, errors.New("API key not found")
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
