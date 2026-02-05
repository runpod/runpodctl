package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion",
	Short: "install shell completion",
	Long:  "install shell completion for runpodctl (auto-detects your shell)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return installCompletion()
	},
}

func detectShell() string {
	// check $SHELL env var
	shell := os.Getenv("SHELL")
	if shell != "" {
		base := filepath.Base(shell)
		switch base {
		case "bash":
			return "bash"
		case "zsh":
			return "zsh"
		case "fish":
			return "fish"
		}
	}

	// check if running in powershell (Windows)
	if os.Getenv("PSModulePath") != "" {
		return "powershell"
	}

	// fallback: check common shell env vars
	if os.Getenv("BASH_VERSION") != "" {
		return "bash"
	}
	if os.Getenv("ZSH_VERSION") != "" {
		return "zsh"
	}
	if os.Getenv("FISH_VERSION") != "" {
		return "fish"
	}

	// default to bash if we can't detect
	fmt.Fprintln(os.Stderr, "could not detect shell, defaulting to bash. specify shell: runpodctl completion [bash|zsh|fish|powershell]")
	return "bash"
}

func installCompletion() error {
	shell := detectShell()
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("could not get home directory: %w", err)
	}

	var configFile string
	var completionLine string

	switch shell {
	case "bash":
		configFile = filepath.Join(home, ".bashrc")
		completionLine = "source <(runpodctl completion generate bash)"
	case "zsh":
		configFile = filepath.Join(home, ".zshrc")
		completionLine = "source <(runpodctl completion generate zsh)"
	case "fish":
		configFile = filepath.Join(home, ".config", "fish", "completions", "runpodctl.fish")
		// for fish, we write the completion directly
		if err := os.MkdirAll(filepath.Dir(configFile), 0755); err != nil {
			return fmt.Errorf("could not create fish completions dir: %w", err)
		}
		f, err := os.Create(configFile)
		if err != nil {
			return fmt.Errorf("could not create fish completion file: %w", err)
		}
		defer f.Close()
		if err := rootCmd.GenFishCompletion(f, true); err != nil {
			return fmt.Errorf("could not generate fish completion: %w", err)
		}
		fmt.Fprintf(os.Stderr, "completion installed to %s\n", configFile)
		fmt.Fprintln(os.Stderr, "restart your shell or run: source "+configFile)
		return nil
	case "powershell":
		fmt.Fprintln(os.Stderr, "for powershell, add this to your profile:")
		fmt.Fprintln(os.Stderr, "  runpodctl completion generate powershell | Out-String | Invoke-Expression")
		return nil
	default:
		return fmt.Errorf("unknown shell: %s", shell)
	}

	// check if already installed
	content, err := os.ReadFile(configFile)
	if err == nil && strings.Contains(string(content), "runpodctl completion") {
		fmt.Fprintf(os.Stderr, "completion already installed in %s\n", configFile)
		return nil
	}

	// append to config file
	f, err := os.OpenFile(configFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("could not open %s: %w", configFile, err)
	}
	defer f.Close()

	if _, err := f.WriteString("\n# runpodctl cli completion\n" + completionLine + "\n"); err != nil {
		return fmt.Errorf("could not write to %s: %w", configFile, err)
	}

	fmt.Fprintf(os.Stderr, "completion installed to %s\n", configFile)
	fmt.Fprintln(os.Stderr, "restart your shell or run: source "+configFile)
	return nil
}

// generateCmd outputs the completion script (for advanced usage / piping)
var generateCompletionCmd = &cobra.Command{
	Use:       "generate [bash|zsh|fish|powershell]",
	Short:     "output completion script",
	Long:      "output the completion script for manual installation or piping",
	ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
	Args:      cobra.MaximumNArgs(1),
	Hidden:    true, // hidden - most users just need 'runpodctl completion'
	RunE: func(cmd *cobra.Command, args []string) error {
		var shell string
		if len(args) > 0 {
			shell = args[0]
		} else {
			shell = detectShell()
		}

		switch shell {
		case "bash":
			return cmd.Root().GenBashCompletion(os.Stdout)
		case "zsh":
			return cmd.Root().GenZshCompletion(os.Stdout)
		case "fish":
			return cmd.Root().GenFishCompletion(os.Stdout, true)
		case "powershell":
			return cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
		default:
			return fmt.Errorf("unknown shell: %s (supported: bash, zsh, fish, powershell)", shell)
		}
	},
}

func init() {
	completionCmd.AddCommand(generateCompletionCmd)
}
