package cmd

import (
	"fmt"
	"os"

	"cli/cmd/config"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var version string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:     "runpod",
	Aliases: []string{"runpodctl"},
	Short:   "CLI for runpod.io",
	Long:    "runpod is a CLI tool to manage your resources on https://runpod.io",
	Version: version,
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute(ver string) {
	version = ver
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

// init adds all child commands to the root command and sets flags appropriately.
func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.AddCommand(config.ConfigCmd)
	// rootCmd.AddCommand(connectCmd)
	// rootCmd.AddCommand(copyCmd)

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(projectCmd)
	rootCmd.AddCommand(updateCmd)

	// Hidden commands
	rootCmd.AddCommand(createCmd)
	rootCmd.AddCommand(getCmd)
	rootCmd.AddCommand(removeCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)

	rootCmd.AddCommand(receiveCmd)
	rootCmd.AddCommand(sendCmd)
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
			fmt.Println("Runpod config file not found, please run runpodctl config to create it")
		}
		viper.SetConfigType("toml")
		//make .runpod folder if not exists
		err := os.MkdirAll(configPath, os.ModePerm)
		cobra.CheckErr(err)
		err = viper.WriteConfigAs(config.ConfigFile)
		cobra.CheckErr(err)
	}
}
