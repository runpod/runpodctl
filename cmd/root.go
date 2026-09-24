package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/runpod/runpodctl/cmd/billing"
	"github.com/runpod/runpodctl/cmd/config"
	"github.com/runpod/runpodctl/cmd/datacenter"
	"github.com/runpod/runpodctl/cmd/doctor"
	"github.com/runpod/runpodctl/cmd/gpu"
	"github.com/runpod/runpodctl/cmd/hub"
	"github.com/runpod/runpodctl/cmd/legacy"
	"github.com/runpod/runpodctl/cmd/model"
	"github.com/runpod/runpodctl/cmd/pod"
	"github.com/runpod/runpodctl/cmd/project"
	"github.com/runpod/runpodctl/cmd/registry"
	"github.com/runpod/runpodctl/cmd/serverless"
	"github.com/runpod/runpodctl/cmd/template"
	"github.com/runpod/runpodctl/cmd/transfer"
	"github.com/runpod/runpodctl/cmd/user"
	"github.com/runpod/runpodctl/cmd/volume"
	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var version string
var outputFormat string
var configInitErr error

// rootCmd is the base command
var rootCmd = &cobra.Command{
	Use:   "runpodctl",
	Short: "cli for runpod.io",
	Long: `runpodctl - manage your ai system.

getting started:
  1. get your api key at https://www.runpod.io/console/user/settings
  2. run: runpodctl doctor (will prompt for key and save it)
  or: export RUNPOD_API_KEY=your-key

resources:
  pod            manage gpu pods
  serverless     manage serverless endpoints (alias: sls)
  template       manage templates (alias: tpl)
  hub            browse the runpod hub
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

deprecated
  get, create, remove, start, stop, exec, project, config, get models`,
}

// GetRootCmd returns the root command
func GetRootCmd() *cobra.Command {
	return rootCmd
}

func init() {
	cobra.OnInitialize(initConfig)
	// disable default completion command, we have our own
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	// Execute emits a single flat JSON error object; silence Cobra's own
	// plain-text error re-print and its usage dump on runtime failures. Usage is
	// re-printed explicitly for genuine usage errors (bad flags/args) in Execute.
	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true
	// tag flag-parsing failures as usage errors so Execute can show usage for
	// them (but not for runtime errors). inherited by subcommands via cobra.
	rootCmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return &usageError{cmd: c, err: err}
	})
	// by default cobra runs only the *closest* PersistentPreRun(E), so a
	// subcommand that defines one (exec and config both do, for their deprecation
	// notices) would shadow the --output guard below and run its body with an
	// unvalidated value. traversing runs the root hook first, then the
	// subcommand's, for every command in the tree.
	cobra.EnableTraverseRunHooks = true
	// reject an unsupported --output before any command body runs, so a typo
	// fails loudly instead of silently returning json. wrapped as a usageError
	// because it is an invocation mistake, not a runtime failure — the message
	// deliberately names the flag rather than starting with a word in
	// usageErrorPrefixes, so classification comes from the type, not the string.
	rootCmd.PersistentPreRunE = func(c *cobra.Command, _ []string) error {
		// `help <cmd>` is exempt: it emits help text, never json or yaml, and every
		// other help path already skips this guard because cobra returns before the
		// hooks for --help. rejecting only this one spelling would mean a bad
		// --output somewhere in your history stops you from reading the help that
		// tells you which values are valid.
		if c.Name() == "help" {
			return nil
		}
		if err := output.ValidateFormat(outputFormat); err != nil {
			return &usageError{cmd: c, err: err}
		}
		return configInitErr
	}
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
	rootCmd.AddCommand(hub.Cmd)

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

	// Help command (lowercase description)
	rootCmd.SetHelpCommand(newHelpCmd(rootCmd))

	// Legacy commands (hidden, for backwards compatibility)
	rootCmd.AddCommand(legacy.GetCmd)
	rootCmd.AddCommand(legacy.CreateCmd)
	rootCmd.AddCommand(legacy.RemoveCmd)
	rootCmd.AddCommand(legacy.StartCmd)
	rootCmd.AddCommand(legacy.StopCmd)

	// Legacy config command (hidden, still works with --apiKey flag)
	config.ConfigCmd.Hidden = true
	config.ConfigCmd.Short = "deprecated: use 'runpodctl doctor'"
	config.ConfigCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		fmt.Fprintln(os.Stderr, "warning: 'runpodctl config' is deprecated, use 'runpodctl doctor' instead")
	}
	rootCmd.AddCommand(config.ConfigCmd)

	// Version flag
	rootCmd.Version = version
	rootCmd.Flags().BoolP("version", "v", false, "print the version of runpodctl")
	rootCmd.SetVersionTemplate(`runpodctl {{ .Version }}
`)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "print the version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("runpodctl %s\n", version)
	},
}

func newHelpCmd(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "help [command]",
		Short: "help about any command",
		Long:  "help about any command",
		Args:  cobra.ArbitraryArgs,
		Run: func(cmd *cobra.Command, args []string) {
			target := root
			if len(args) == 0 {
				_ = target.Help()
				return
			}
			c, _, err := target.Find(args)
			if err != nil || c == nil {
				fmt.Fprintf(os.Stderr, "unknown help topic %q\n", args)
				_ = target.Help()
				return
			}
			_ = c.Help()
		},
	}
}

// usageError marks an error caused by incorrect cli usage (bad flags, wrong
// arg count, unknown command) as opposed to a runtime/API failure. Execute
// prints usage text only for these, keeping runtime errors to a clean JSON
// object.
type usageError struct {
	cmd *cobra.Command
	err error
}

func (e *usageError) Error() string     { return e.err.Error() }
func (e *usageError) Unwrap() error     { return e.err }
func (e *usageError) ErrorCode() string { return "usage_error" }

// usageErrorPrefixes are the stable leading strings Cobra uses for argument and
// command validation errors. Flag-parsing errors are already wrapped via
// SetFlagErrorFunc; these cover the arg/command validators that are not.
//
// COMMAND AUTHORS: do not start a *runtime* error message with any of these
// words. A plain `fmt.Errorf("invalid argument …")` returned from a command
// would be classified as a usage error and dump the usage text. Typed
// *api.APIError / *api.GraphQLError are exempt (they bail out in asUsageError
// before this list), so this only bites hand-rolled errors. Prefer naming the
// flag, e.g. `invalid --scale-by %q`, which does not match.
//
// This list mirrors Cobra's internal message strings; re-check it when bumping
// the Cobra dependency. TestAsUsageError in root_test.go pins the current set.
var usageErrorPrefixes = []string{
	"unknown command",
	"unknown flag",
	"unknown shorthand flag",
	"invalid argument",
	"accepts ", // "accepts N arg(s)…" and "accepts at most/between…"
	"requires at least",
	// cobra's ValidateRequiredFlags runs after flag parsing, so it does not go
	// through SetFlagErrorFunc and has to be matched here (20 MarkFlagRequired
	// sites, e.g. `template create` with no --name/--image).
	"required flag(s)",
	// ValidateFlagGroups is in the same post-parse position as
	// ValidateRequiredFlags above, so MarkFlagsMutuallyExclusive violations
	// (registry create's --password / --password-stdin) also land here.
	"if any flags in the group",
}

// asUsageError reports whether err represents a usage error, returning a
// *usageError (wrapping when needed) carrying the command to print usage for.
func asUsageError(cmd *cobra.Command, err error) (*usageError, bool) {
	var ue *usageError
	if errors.As(err, &ue) {
		return ue, true
	}
	// typed api/graphql errors are runtime failures, never usage errors — bail
	// before the string fallback so a server message that happens to start with
	// a usage-ish word (e.g. "invalid argument: ...") keeps its real code and
	// doesn't trigger a usage dump.
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		return nil, false
	}
	var gqlErr *api.GraphQLError
	if errors.As(err, &gqlErr) {
		return nil, false
	}
	// the remaining fallback matches cobra's arg/command validators, which
	// return plain errors (flag errors are already typed via SetFlagErrorFunc).
	msg := err.Error()
	for _, p := range usageErrorPrefixes {
		if strings.HasPrefix(msg, p) {
			return &usageError{cmd: cmd, err: err}, true
		}
	}
	return nil, false
}

// Execute runs the root command
func Execute(ver string) {
	version = ver
	api.Version = ver
	rootCmd.Version = ver

	err := rootCmd.Execute()
	if err == nil {
		return
	}

	// best-effort: find the command that was invoked so usage text (when shown)
	// matches the subcommand rather than the root.
	invoked := rootCmd
	if len(os.Args) > 1 {
		if found, _, findErr := rootCmd.Find(os.Args[1:]); findErr == nil && found != nil {
			invoked = found
		}
	}

	if ue, ok := asUsageError(invoked, err); ok {
		// emit the flat JSON error (with a stable "usage_error" code) then the
		// usage text, so agents get the machine-readable object and humans get help.
		output.Error(ue)
		cmd := ue.cmd
		if cmd == nil {
			cmd = invoked
		}
		fmt.Fprint(os.Stderr, cmd.UsageString())
	} else {
		output.Error(err)
	}
	os.Exit(1)
}

// the api key is stored in ~/.runpod/config.toml, so neither that file nor the
// directory holding it may be readable by other users on a shared machine.
// viper and MkdirAll only apply these modes to what they create, which is why
// tightenConfigPermissions exists alongside them.
const (
	configDirPerm  = 0o700
	configFilePerm = 0o600
)

// initConfig reads config file and ENV variables
func initConfig() {
	configInitErr = loadConfig()
}

func loadConfig() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("could not locate home directory: %w", err)
	}

	configDir := filepath.Join(home, ".runpod")
	configFile := filepath.Join(configDir, "config.toml")
	legacyFile := filepath.Join(home, ".runpod.yaml")

	// nothing below is fatal. a read-only or foreign-owned config must not stop
	// commands that authenticate through RUNPOD_API_KEY, and skipping a repair
	// leaves the file no more exposed than it already was.
	if err := os.MkdirAll(configDir, configDirPerm); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not create config directory: %v\n", err)
	}
	tightenConfigPermissions(configDir, configFile, legacyFile)

	// SetConfigPermissions applies to every viper write, so the `config`,
	// `doctor` and `project` commands inherit 0600 without their own calls.
	viper.SetConfigPermissions(configFilePerm)
	// SetConfigFile, not AddConfigPath + SetConfigName: viper 1.19 does not
	// update ConfigFileUsed after WriteConfigAs, so with a *searched* path every
	// later WriteConfig() aims at whatever the search last matched — which on a
	// fresh machine is nothing at all, and `config --apiKey` fails with "Config
	// File ".runpod.yaml" Not Found". Naming the file up front pins all of them
	// to the toml.
	viper.SetConfigFile(configFile)
	viper.SetConfigType("toml")

	viper.AutomaticEnv()

	switch err := viper.ReadInConfig(); {
	case err == nil:
		// config loaded
	case configFileMissing(err):
		// no toml yet: adopt ~/.runpod.yaml if the user still has one, then
		// create the toml. the legacy file is read through its own viper so a
		// half-done migration cannot leave the global one pointed at the yaml.
		legacy := viper.New()
		legacy.SetConfigFile(legacyFile)
		legacy.SetConfigType("yaml")
		switch legacyErr := legacy.ReadInConfig(); {
		case legacyErr == nil:
			fmt.Fprintln(os.Stderr, "migrating config from ~/.runpod.yaml to ~/.runpod/config.toml")
			if mergeErr := viper.MergeConfigMap(legacy.AllSettings()); mergeErr != nil {
				fmt.Fprintf(os.Stderr, "warning: could not migrate %s: %v\n", legacyFile, mergeErr)
			}
		case configFileMissing(legacyErr):
			// nothing to migrate
		default:
			fmt.Fprintf(os.Stderr, "warning: could not read %s, not migrating it: %v\n", legacyFile, legacyErr)
		}
		if writeErr := viper.SafeWriteConfigAs(configFile); writeErr != nil {
			fmt.Fprintf(os.Stderr, "warning: could not create %s: %v\n", configFile, writeErr)
		}
	default:
		// unreadable or malformed. this used to fall into the branch above,
		// which truncated a config that still held the user's api key. warn and
		// leave it: the commands that need a key report no_credentials.
		fmt.Fprintf(os.Stderr, "warning: could not read %s: %v\n", configFile, err)
	}

	return nil
}

// configFileMissing reports whether ReadInConfig failed only because there is
// no config file. SetConfigFile makes viper return the raw *fs.PathError rather
// than its own ConfigFileNotFoundError, so both are accepted.
func configFileMissing(err error) bool {
	var notFound viper.ConfigFileNotFoundError
	return errors.Is(err, fs.ErrNotExist) || errors.As(err, &notFound)
}

// tightenConfigPermissions warns rather than fails, for the same reason as the
// rest of loadConfig.
func tightenConfigPermissions(configDir, configFile, legacyFile string) {
	for _, target := range []struct {
		path string
		mode os.FileMode
		dir  bool
	}{
		{configDir, configDirPerm, true},
		{configFile, configFilePerm, false},
		// the legacy config is remediated but never deleted: it is the user's
		// file, and the toml already wins on every read.
		{legacyFile, configFilePerm, false},
	} {
		if err := restrictConfigPath(target.path, target.mode, target.dir); err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
	}
}

// restrictConfigPath follows symlinks (stow, home-manager, a dotfile linked into
// /workspace on a pod) and checks and narrows the file they point at. chmod only
// removes bits, so following a link cannot widen access to anything.
func restrictConfigPath(path string, mode os.FileMode, directory bool) error {
	resolved, err := filepath.EvalSymlinks(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("could not inspect %s: %w", path, err)
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		return fmt.Errorf("could not inspect %s: %w", resolved, err)
	}
	// resolved has no links left in it, so one here was swapped in since.
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("config path changed while restricting permissions: %s", path)
	}
	if directory && !info.IsDir() || !directory && !info.Mode().IsRegular() {
		return fmt.Errorf("unexpected config file type, not restricting permissions: %s", resolved)
	}
	if runtime.GOOS == "windows" || info.Mode().Perm()&0o077 == 0 {
		return nil
	}

	file, err := os.Open(resolved)
	if err != nil {
		return fmt.Errorf("could not open %s to restrict permissions: %w", resolved, err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return fmt.Errorf("could not inspect opened config path %s: %w", resolved, err)
	}
	if !os.SameFile(info, opened) {
		return fmt.Errorf("config path changed while restricting permissions: %s", path)
	}
	if err := file.Chmod(mode); err != nil {
		return fmt.Errorf("could not restrict permissions on %s: %w", resolved, err)
	}
	return nil
}
