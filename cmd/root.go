package cmd

import (
	"fmt"
	"os"

	"github.com/runpod/runpodctl/api"
	"github.com/runpod/runpodctl/cmd/config"
	"github.com/runpod/runpodctl/cmd/croc"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var version string

// Entrypoint for the CLI
var rootCmd = &cobra.Command{
	Use:   "runpodctl",
	Short: "CLI for runpod.io",
	Long:  "The RunPod CLI tool to manage resources on runpod.io and develop serverless applications.",
}

func GetRootCmd() *cobra.Command {
	return rootCmd
}

func init() {
	cobra.OnInitialize(initConfig)
	registerCommands()
}

func registerCommands() {
	rootCmd.AddCommand(config.ConfigCmd)
	// RootCmd.AddCommand(connectCmd)
	// RootCmd.AddCommand(copyCmd)
	rootCmd.AddCommand(createCmd)
	rootCmd.AddCommand(getCmd)
	rootCmd.AddCommand(removeCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(projectCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(sshCmd)

	// Remote File Execution
	rootCmd.AddCommand(execCmd)

	// file transfer via croc
	rootCmd.AddCommand(croc.ReceiveCmd)
	rootCmd.AddCommand(croc.SendCmd)

	// Version
	rootCmd.Version = version
	rootCmd.Flags().BoolP("version", "v", false, "Print the version of runpodctl")
	rootCmd.SetVersionTemplate(`{{printf "runpodctl %s\n" .Version}}`)
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute(ver string) {
	version = ver
	api.Version = ver
	rootCmd.Version = ver

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
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
		// legacy: try to migrate old config to new location
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
		// make .runpod folder if not exists
		err := os.MkdirAll(configPath, os.ModePerm)
		cobra.CheckErr(err)
		err = viper.WriteConfigAs(config.ConfigFile)
		cobra.CheckErr(err)
	}
}
