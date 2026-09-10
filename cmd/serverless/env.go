package serverless

import (
	"fmt"

	"github.com/runpod/runpodctl/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var envCmd = &cobra.Command{
	Use:   "env <endpoint-id>",
	Short: "show an endpoint's own live environment variable override",
	Long: `show the environment variable override configured directly on a
serverless endpoint (GraphQL Endpoint.env). this is the override that
reaches the running container, not the template's default env that
` + "`runpodctl template get`" + ` reports.

an empty result means the endpoint has no override and runs on its
template's default env.

the override can contain secrets (an HF_TOKEN, an api key) -- don't paste
its output verbatim into a ticket or chat.`,
	Example: `  # the env var override configured directly on an endpoint
  runpodctl serverless env abc123`,
	Args: cobra.ExactArgs(1),
	RunE: runEnv,
}

func init() {
	Cmd.AddCommand(envCmd)
}

// EndpointEnvResult is the output of `runpodctl serverless env`.
type EndpointEnvResult struct {
	EndpointID  string               `json:"endpointId"`
	EnvOverride []api.EndpointEnvVar `json:"envOverride"`
}

func runEnv(cmd *cobra.Command, args []string) error {
	result, err := buildEndpointEnvResult(args[0])
	if err != nil {
		return err
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(result, &output.Config{Format: format})
}

func buildEndpointEnvResult(endpointID string) (*EndpointEnvResult, error) {
	env, err := api.GetEndpointEnvOverride(endpointID)
	if err != nil {
		return nil, fmt.Errorf("failed to get env override for endpoint %s: %w", endpointID, err)
	}

	return &EndpointEnvResult{
		EndpointID:  endpointID,
		EnvOverride: env,
	}, nil
}
