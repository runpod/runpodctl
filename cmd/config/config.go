package config

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var ConfigFile string
var apiKey string
var apiUrl string

var ConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "CLI Config",
	Long:  "RunPod CLI Config Settings",
	Run: func(c *cobra.Command, args []string) {
		err := viper.WriteConfig()
		cobra.CheckErr(err)

		fmt.Println("saved apiKey into config file: " + ConfigFile)
	},
}

func init() {
	ConfigCmd.Flags().StringVar(&apiKey, "apiKey", "", "runpod api key")
	viper.BindPFlag("apiKey", ConfigCmd.Flags().Lookup("apiKey"))
	viper.SetDefault("apiKey", "")

	ConfigCmd.Flags().StringVar(&apiUrl, "apiUrl", "", "runpod api url")
	viper.BindPFlag("apiUrl", ConfigCmd.Flags().Lookup("apiUrl"))
	viper.SetDefault("apiUrl", "https://api.runpod.io/graphql")
}
