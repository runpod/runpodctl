package cmd

import (
	"fmt"
	"os"

	"github.com/runpod/runpod/cmd/legacy"
	"github.com/runpod/runpod/cmd/pod"
	"github.com/runpod/runpod/cmd/project"
	"github.com/runpod/runpod/cmd/registry"
	"github.com/runpod/runpod/cmd/serverless"
	"github.com/runpod/runpod/cmd/template"
	"github.com/runpod/runpod/cmd/transfer"
	"github.com/runpod/runpod/cmd/volume"
	"github.com/runpod/runpod/internal/api"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var version string
var outputFormat string

// rootCmd is the base command
var rootCmd = &cobra.Command{
	Use:   "runpod",
	Short: "cli for runpod.io",
	Long: `runpod cli - manage gpu pods, serverless endpoints, and more on runpod.

resources:
  pod         manage gpu pods
  serverless  manage serverless endpoints (alias: sls)
  template    manage templates (alias: tpl)
  volume      manage network volumes (alias: vol)
  registry    manage container registry auth (alias: reg)

utilities:
  ssh         manage ssh keys and connections
  send        send files using croc
  receive     receive files using croc
  project     manage serverless projects
  config      configure cli settings

legacy commands (deprecated): get, create, remove, start, stop`,
}

// GetRootCmd returns the root command
func GetRootCmd() *cobra.Command {
	return rootCmd
}

func init() {
	cobra.OnInitialize(initConfig)
	registerCommands()
}

func registerCommands() {
	// Global flags
	rootCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "json", "output format (json, yaml, table)")

	// Core resource commands
	rootCmd.AddCommand(pod.Cmd)
	rootCmd.AddCommand(serverless.Cmd)
	rootCmd.AddCommand(template.Cmd)
	rootCmd.AddCommand(volume.Cmd)
	rootCmd.AddCommand(registry.Cmd)

	// Utility commands
	rootCmd.AddCommand(sshCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(transfer.SendCmd)
	rootCmd.AddCommand(transfer.ReceiveCmd)

	// Project commands
	projectCmd := &cobra.Command{
		Use:   "project",
		Short: "manage serverless projects",
		Long:  "create, develop, build, and deploy serverless projects",
	}
	projectCmd.AddCommand(project.NewProjectCmd)
	projectCmd.AddCommand(project.StartProjectCmd)
	projectCmd.AddCommand(project.DeployProjectCmd)
	projectCmd.AddCommand(project.BuildProjectCmd)
	rootCmd.AddCommand(projectCmd)

	// Version command
	rootCmd.AddCommand(versionCmd)

	// Legacy commands (hidden, for backwards compatibility)
	rootCmd.AddCommand(legacy.GetCmd)
	rootCmd.AddCommand(legacy.CreateCmd)
	rootCmd.AddCommand(legacy.RemoveCmd)
	rootCmd.AddCommand(legacy.StartCmd)
	rootCmd.AddCommand(legacy.StopCmd)

	// Version flag
	rootCmd.Version = version
	rootCmd.Flags().BoolP("version", "v", false, "print the version of runpod")
	rootCmd.SetVersionTemplate(`runpod {{ .Version }}
`)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "print the version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("runpod %s\n", version)
	},
}

// Execute runs the root command
func Execute(ver string) {
	version = ver
	api.Version = ver
	rootCmd.Version = ver

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, `{"error":"%s"}`+"\n", err.Error())
		os.Exit(1)
	}
}

// initConfig reads config file and ENV variables
func initConfig() {
	home, err := os.UserHomeDir()
	cobra.CheckErr(err)
	configPath := home + "/.runpod"
	viper.AddConfigPath(configPath)
	viper.SetConfigType("toml")
	viper.SetConfigName("config.toml")

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		// config loaded
	} else {
		// legacy: try to migrate old config
		viper.SetConfigType("yaml")
		viper.AddConfigPath(home)
		viper.SetConfigName(".runpod.yaml")
		if yamlReadErr := viper.ReadInConfig(); yamlReadErr == nil {
			fmt.Fprintln(os.Stderr, "migrating config from ~/.runpod.yaml to ~/.runpod/config.toml")
		}
		viper.SetConfigType("toml")
		// make .runpod folder if not exists
		err := os.MkdirAll(configPath, os.ModePerm)
		cobra.CheckErr(err)
		viper.WriteConfigAs(configPath + "/config.toml") //nolint:errcheck
	}
}
