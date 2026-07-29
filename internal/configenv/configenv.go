package configenv

import (
	"os"

	"github.com/spf13/viper"
)

const (
	APIKeyEnv     = "RUNPOD_API_KEY"
	RESTURLEnv    = "RUNPOD_API_URL"
	GraphQLURLEnv = "RUNPOD_GRAPHQL_URL"
	InvokeURLEnv  = "RUNPOD_INVOKE_URL"
)

func APIKey() string {
	return envOrConfig(APIKeyEnv, "apiKey")
}

func RESTURL() string {
	return envOrConfig(RESTURLEnv, "restApiUrl")
}

func GraphQLURL() string {
	return envOrConfig(GraphQLURLEnv, "apiUrl")
}

// InvokeURL is the base url for *invoking* serverless endpoints, which is a
// different service from the control plane (RESTURL/GraphQLURL) and so is
// overridden separately. Empty means use the prod default.
func InvokeURL() string {
	return envOrConfig(InvokeURLEnv, "invokeUrl")
}

func envOrConfig(envKey, configKey string) string {
	if value := os.Getenv(envKey); value != "" {
		return value
	}
	return viper.GetString(configKey)
}
