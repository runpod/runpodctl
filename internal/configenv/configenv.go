// Package configenv resolves settings from the environment, then the config file.
package configenv

import (
	"os"
	"strings"

	"github.com/spf13/viper"
)

// Environment variables that override the config file.
const (
	// #nosec G101 -- an env var name, not a credential
	APIKeyEnv      = "RUNPOD_API_KEY"
	RESTURLEnv     = "RUNPOD_API_URL"
	GraphQLURLEnv  = "RUNPOD_GRAPHQL_URL"
	InvokeURLEnv   = "RUNPOD_INVOKE_URL"
	S3AccessKeyEnv = "RUNPOD_S3_ACCESS_KEY"
	// #nosec G101 -- an env var name, not a credential
	S3SecretKeyEnv = "RUNPOD_S3_SECRET_KEY"
)

// sdkAPIKey is where the Runpod SDK's `rp login` (and the older `flash login`)
// saves the key in the shared ~/.runpod/config.toml: a `[default]` table with
// `api_key`. Reading it here means one login works for both tools.
const sdkAPIKey = "default.api_key"

// APIKey prefers this cli's own key and falls back to the shared sdk key. A
// whitespace-only value counts as unset, matching how the sdk reads it.
func APIKey() string {
	for _, value := range []string{os.Getenv(APIKeyEnv), viper.GetString("apiKey"), viper.GetString(sdkAPIKey)} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

// RESTURL is the base url for the rest control plane.
func RESTURL() string {
	return envOrConfig(RESTURLEnv, "restApiUrl")
}

// GraphQLURL is the graphql api url.
func GraphQLURL() string {
	return envOrConfig(GraphQLURLEnv, "apiUrl")
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

// S3SecretKey is the secret half of the s3 api keys; see S3AccessKey.
func S3SecretKey() string {
	return envOrConfig(S3SecretKeyEnv, "s3SecretKey")
}

func envOrConfig(envKey, configKey string) string {
	if value := os.Getenv(envKey); value != "" {
		return value
	}
	return viper.GetString(configKey)
}
