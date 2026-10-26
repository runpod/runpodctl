package configenv

import (
	"os"

	"github.com/spf13/viper"
)

const (
	APIKeyEnv      = "RUNPOD_API_KEY"
	RESTURLEnv     = "RUNPOD_API_URL"
	RESTV2URLEnv   = "RUNPOD_REST_V2_URL"
	GraphQLURLEnv  = "RUNPOD_GRAPHQL_URL"
	InvokeURLEnv   = "RUNPOD_INVOKE_URL"
	S3AccessKeyEnv = "RUNPOD_S3_ACCESS_KEY"
	S3SecretKeyEnv = "RUNPOD_S3_SECRET_KEY"
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

// RESTV2URL is the base url for the v2 rest api. It is a *third* control-plane
// host, separate from RESTURL: this cli's crud still runs on rest v1
// (rest.runpod.io/v1), while log streaming and the worker listing only exist on
// v2 (api.runpod.io/v2). They are overridden separately so pointing one at a dev
// host does not silently move the other. Empty means use the prod default.
func RESTV2URL() string {
	return envOrConfig(RESTV2URLEnv, "restV2ApiUrl")
}

// InvokeURL is the base url for *invoking* serverless endpoints, which is a
// different service from the control plane (RESTURL/GraphQLURL) and so is
// overridden separately. Empty means use the prod default.
func InvokeURL() string {
	return envOrConfig(InvokeURLEnv, "invokeUrl")
}

// S3AccessKey and S3SecretKey are the s3 api keys network volume transfers
// sign with. They are separate credentials from the api key.
func S3AccessKey() string {
	return envOrConfig(S3AccessKeyEnv, "s3AccessKey")
}

func S3SecretKey() string {
	return envOrConfig(S3SecretKeyEnv, "s3SecretKey")
}

func envOrConfig(envKey, configKey string) string {
	if value := os.Getenv(envKey); value != "" {
		return value
	}
	return viper.GetString(configKey)
}
