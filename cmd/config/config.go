package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var ConfigFile string
var ApiKey string

// ConfigCmd represents the config command
var ConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "CLI Config",
	Long:  "RunPod CLI Config Settings",
	Run: func(c *cobra.Command, args []string) {
		ApiKey = strings.TrimSpace(ApiKey)
		if len(ApiKey) == 0 {
			cobra.CheckErr(errors.New("apiKey cannot be empty"))
		}
		err := viper.WriteConfig()
		cobra.CheckErr(err)

		fmt.Println("saved apiKey into config file: " + ConfigFile)
	},
}

func init() {
	ConfigCmd.Flags().StringVar(&ApiKey, "apiKey", "", "runpod api key")
	ConfigCmd.MarkFlagRequired("apiKey")
	viper.BindPFlag("apiKey", ConfigCmd.Flags().Lookup("apiKey"))
	viper.SetDefault("apiKey", "")
}
