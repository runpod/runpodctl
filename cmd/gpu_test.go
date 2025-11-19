package cmd

import (
	"cmd/diagnostic"

	"github.com/spf13/cobra"
)

var gpuTestCmd = &cobra.Command{
	Use:   "gpu-test",
	Short: "GPU test commands",
	Long:  "Commands for testing GPU functionality",
}

func init() {
	gpuTestCmd.AddCommand(diagnostic.GpuDiagnosticsCmd)
}