package model

import (
	"strings"

	"github.com/runpod/runpodctl/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var (
	getProvider string
	getName     string
)

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Args:    cobra.ExactArgs(0),
	Short:   "list models",
	Long:    "list models in the runpod model repository",
	RunE:    runModelList,
}

var GetModelsCmd = &cobra.Command{
	Use:     "models",
	Aliases: []string{"model"},
	Args:    cobra.ExactArgs(0),
	Short:   "deprecated: use 'runpodctl model list'",
	Hidden:  true,
	RunE:    runModelList,
}

func init() {
	bindModelListFlags(listCmd)
	bindModelListFlags(GetModelsCmd)
}

func bindModelListFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&getProvider, "provider", "", "filter by provider")
	cmd.Flags().StringVar(&getName, "name", "", "filter by model name")
}

func runModelList(cmd *cobra.Command, args []string) error {
	input := &api.GetModelsInput{
		Provider: getProvider,
		Name:     getName,
	}

	models, err := api.GetModels(input)
	if err != nil {

		return err
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(models, &output.Config{Format: format})
}

func modelVersionHash(model *api.Model) string {
	if model == nil {
		return ""
	}

	for _, version := range model.Versions {
		if version != nil && strings.TrimSpace(version.Hash) != "" {
			return version.Hash
		}
	}

	return ""
}
