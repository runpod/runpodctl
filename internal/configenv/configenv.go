package configenv

import (
	"os"

	"github.com/spf13/viper"
)

const (
	APIKeyEnv     = "RUNPOD_API_KEY"
	RESTURLEnv    = "RUNPOD_API_URL"
	GraphQLURLEnv = "RUNPOD_GRAPHQL_URL"
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

func envOrConfig(envKey, configKey string) string {
	if value := os.Getenv(envKey); value != "" {
		return value
	}
	return viper.GetString(configKey)
}
