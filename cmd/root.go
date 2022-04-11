package cmd

import (
	"os"

	"cli/cmd/config"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var version string

// rootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "runpodctl",
	Short: "runpodctl for runpod.io",
	Long:  "runpodctl is a CLI tool to manage your pods for runpod.io",
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute(ver string) {
	version = ver
	err := RootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)
	RootCmd.AddCommand(config.ConfigCmd)
	RootCmd.AddCommand(getCmd)
	RootCmd.AddCommand(removeCmd)
	RootCmd.AddCommand(startCmd)
	RootCmd.AddCommand(stopCmd)
	RootCmd.AddCommand(versionCmd)
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	home, err := os.UserHomeDir()
	cobra.CheckErr(err)

	viper.AddConfigPath(home)
	viper.SetConfigType("yaml")
	viper.SetConfigName(".runpod")
	config.ConfigFile = home + "/.runpod.yaml"

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		// fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	} else {
		err := viper.WriteConfigAs(config.ConfigFile)
		cobra.CheckErr(err)
	}
}
