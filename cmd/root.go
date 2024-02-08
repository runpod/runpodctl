package cmd

import (
	"fmt"
	"os"

	"cli/api"
	"cli/cmd/config"
	"cli/cmd/croc"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var version string

// rootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:     "runpodctl",
	Aliases: []string{"runpod"},
	Short:   "CLI for runpod.io",
	Long:    "CLI tool to manage your pods for runpod.io",
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute(ver string) {
	version = ver
	api.Version = ver
	err := RootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)
	RootCmd.AddCommand(config.ConfigCmd)
	// RootCmd.AddCommand(connectCmd)
	// RootCmd.AddCommand(copyCmd)
	RootCmd.AddCommand(createCmd)
	RootCmd.AddCommand(getCmd)
	RootCmd.AddCommand(loginCmd)
	RootCmd.AddCommand(removeCmd)
	RootCmd.AddCommand(startCmd)
	RootCmd.AddCommand(stopCmd)
	RootCmd.AddCommand(versionCmd)
	RootCmd.AddCommand(projectCmd)
	RootCmd.AddCommand(updateCmd)

	RootCmd.AddCommand(croc.ReceiveCmd)
	RootCmd.AddCommand(croc.SendCmd)
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	home, err := os.UserHomeDir()
	cobra.CheckErr(err)
	configPath := home + "/.runpod"
	viper.AddConfigPath(configPath)
	viper.SetConfigType("toml")
	viper.SetConfigName("config.toml")
	config.ConfigFile = configPath + "/config.toml"

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		// fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	} else {
		//legacy: try to migrate old config to new location
		viper.SetConfigType("yaml")
		viper.AddConfigPath(home)
		viper.SetConfigName(".runpod.yaml")
		if yamlReadErr := viper.ReadInConfig(); yamlReadErr == nil {
			fmt.Println("Runpod config location has moved from ~/.runpod.yaml to ~/.runpod/config.toml")
			fmt.Println("migrating your existing config to ~/.runpod/config.toml")
		} else {
			fmt.Println("Runpod config file not found, please run `runpodctl config` to create it")
		}
		viper.SetConfigType("toml")
		//make .runpod folder if not exists
		err := os.MkdirAll(configPath, os.ModePerm)
		cobra.CheckErr(err)
		err = viper.WriteConfigAs(config.ConfigFile)
		cobra.CheckErr(err)
	}
}
