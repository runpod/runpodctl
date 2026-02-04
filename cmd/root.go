package cmd

import (
	"fmt"
	"os"

	"github.com/runpod/runpod/cmd/billing"
	"github.com/runpod/runpod/cmd/config"
	"github.com/runpod/runpod/cmd/datacenter"
	"github.com/runpod/runpod/cmd/doctor"
	"github.com/runpod/runpod/cmd/gpu"
	"github.com/runpod/runpod/cmd/legacy"
	"github.com/runpod/runpod/cmd/model"
	"github.com/runpod/runpod/cmd/pod"
	"github.com/runpod/runpod/cmd/project"
	"github.com/runpod/runpod/cmd/registry"
	"github.com/runpod/runpod/cmd/serverless"
	"github.com/runpod/runpod/cmd/template"
	"github.com/runpod/runpod/cmd/transfer"
	"github.com/runpod/runpod/cmd/user"
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
	Long: `runpod cli - manage gpu pods, serverless endpoints, and more.

getting started:
  1. get your api key at https://www.runpod.io/console/user/settings
  2. run: runpod doctor (will prompt for key and save it)
  or: export RUNPOD_API_KEY=your-key

resources:
  pod            manage gpu pods
  serverless     manage serverless endpoints (alias: sls)
  template       manage templates (alias: tpl)
  model          manage model repository
  network-volume manage network volumes (alias: nv)
  registry       manage container registry auth (alias: reg)

info:
  user           show account info and balance (alias: me)
  gpu            list available gpu types
  datacenter     list datacenters and availability (alias: dc)
  billing        view billing history

utilities:
  doctor         diagnose and fix cli issues
  ssh            manage ssh keys and connections
  send/receive   transfer files to/from pods

runpod v2 (formerly runpodctl) - legacy commands still supported
legacy (deprecated): (get, create, remove, start, stop, exec, config, get models)`,
}

// GetRootCmd returns the root command
func GetRootCmd() *cobra.Command {
	return rootCmd
}

func init() {
	cobra.OnInitialize(initConfig)
	// disable default completion command, we have our own
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	registerCommands()
}

func registerCommands() {
	// Global flags
	rootCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "json", "output format (json, yaml)")

	// Core resource commands
	rootCmd.AddCommand(pod.Cmd)
	rootCmd.AddCommand(serverless.Cmd)
	rootCmd.AddCommand(template.Cmd)
	rootCmd.AddCommand(model.Cmd)
	rootCmd.AddCommand(volume.Cmd)
	rootCmd.AddCommand(registry.Cmd)

	// Info commands
	rootCmd.AddCommand(user.Cmd)
	rootCmd.AddCommand(gpu.Cmd)
	rootCmd.AddCommand(datacenter.Cmd)
	rootCmd.AddCommand(billing.Cmd)

	// Utility commands
	rootCmd.AddCommand(sshCmd)
	rootCmd.AddCommand(doctor.Cmd)
	rootCmd.AddCommand(transfer.SendCmd)
	rootCmd.AddCommand(transfer.ReceiveCmd)
	rootCmd.AddCommand(execCmd)

	// Project commands (hidden - deprecated, will be replaced)
	projectCmd := &cobra.Command{
		Use:    "project",
		Short:  "manage serverless projects (deprecated)",
		Long:   "create, develop, build, and deploy serverless projects",
		Hidden: true,
	}
	projectCmd.AddCommand(project.NewProjectCmd)
	projectCmd.AddCommand(project.StartProjectCmd)
	projectCmd.AddCommand(project.DeployProjectCmd)
	projectCmd.AddCommand(project.BuildProjectCmd)
	rootCmd.AddCommand(projectCmd)

	// Version command
	rootCmd.AddCommand(versionCmd)

	// Completion command (replaces default cobra completion)
	rootCmd.AddCommand(completionCmd)

	// Update command
	rootCmd.AddCommand(updateCmd)

	// Legacy commands (hidden, for backwards compatibility)
	rootCmd.AddCommand(legacy.GetCmd)
	rootCmd.AddCommand(legacy.CreateCmd)
	rootCmd.AddCommand(legacy.RemoveCmd)
	rootCmd.AddCommand(legacy.StartCmd)
	rootCmd.AddCommand(legacy.StopCmd)

	// Legacy config command (hidden, still works with --apiKey flag)
	config.ConfigCmd.Hidden = true
	config.ConfigCmd.Short = "deprecated: use 'runpod doctor'"
	config.ConfigCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		fmt.Fprintln(os.Stderr, "warning: 'runpod config' is deprecated, use 'runpod doctor' instead")
	}
	rootCmd.AddCommand(config.ConfigCmd)

	// Version flag
	rootCmd.Version = version
	rootCmd.Flags().BoolP("version", "v", false, "print the version of runpod")
	rootCmd.SetVersionTemplate(`runpod {{ .Version }} (formerly runpodctl)
`)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "print the version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("runpod %s (formerly runpodctl)\n", version)
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
