package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"time"

	"github.com/spf13/viper"
)

type Input struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

func Query(input Input) (res *http.Response, err error) {
	jsonValue, err := json.Marshal(input)
	if err != nil {
		return
	}

	apiKey := os.Getenv("RUNPOD_API_KEY")
	if apiKey == "" {
		apiKey = viper.GetString("apiKey")
	}
	req, err := http.NewRequest("POST", viper.GetString("apiUrl")+"?api_key="+apiKey, bytes.NewBuffer(jsonValue))
	if err != nil {
		return
	}
	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{Timeout: time.Second * 5}
	return client.Do(req)
}
