package cmd

import (
	"cli/services"
	"fmt"

	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to your RunPod account",
	Run: func(cmd *cobra.Command, args []string) {
		err := services.StartLoginProcess()
		if err != nil {
			// Handle error
			fmt.Println(err)
		}
		fmt.Println("Login successful.")
	},
}

func init() {
	startCmd.AddCommand(loginCmd)
}
