package registry

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/clierr"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "create a new registry auth",
	Long: `create a new container registry authentication

the password is a long-lived credential, so prefer --password-stdin: a value
passed with --password is visible in the process table for the lifetime of the
command and is written to your shell history. with neither flag, an interactive
terminal is prompted without echo.`,
	Example: `  # read the password from a pipe (preferred)
  echo "$REGISTRY_TOKEN" | runpodctl registry create --name ghcr --username me --password-stdin

  # read it from a file without exposing it to the process table
  runpodctl registry create --name ghcr --username me --password-stdin < token.txt

  # prompt for it, without echo
  runpodctl registry create --name ghcr --username me`,
	Args: cobra.NoArgs,
	RunE: runCreate,
}

var (
	createName          string
	createUsername      string
	createPassword      string
	createPasswordStdin bool
)

func init() {
	createCmd.Flags().StringVar(&createName, "name", "", "registry auth name (required)")
	createCmd.Flags().StringVar(&createUsername, "username", "", "registry username (required)")
	createCmd.Flags().StringVar(&createPassword, "password", "", "registry password; insecure, prefer --password-stdin")
	createCmd.Flags().BoolVar(&createPasswordStdin, "password-stdin", false, "read the registry password from stdin")

	createCmd.MarkFlagRequired("name")     //nolint:errcheck
	createCmd.MarkFlagRequired("username") //nolint:errcheck
	// --password is deliberately not marked required: cobra enforces required flags
	// before RunE, which would reject the --password-stdin and prompt paths before
	// they can supply the value. resolvePassword enforces it instead.
	createCmd.MarkFlagsMutuallyExclusive("password", "password-stdin")
}

// promptPassword is a package variable so tests can exercise the prompt branch
// without a controlling terminal.
var promptPassword = func(stderr io.Writer) (string, error) {
	fmt.Fprint(stderr, "registry password: ")
	secret, err := term.ReadPassword(int(os.Stdin.Fd()))
	// ReadPassword swallows the user's newline, so the next line of output would
	// otherwise start on the prompt line.
	fmt.Fprintln(stderr)
	if err != nil {
		return "", fmt.Errorf("failed to read the password from the terminal: %w", err)
	}
	return string(secret), nil
}

// stdinIsTerminal reports whether stdin is a terminal we can prompt on, as
// opposed to a pipe or a redirected file.
var stdinIsTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// resolvePassword picks the registry password out of the three supported
// sources. It takes the reader and writer rather than touching os.Stdin/Stderr
// directly so the whole matrix is testable without a tty.
//
// A password on the command line is not refused, only warned about: scripts
// already pass it that way and breaking them is not this change's job.
func resolvePassword(stdin io.Reader, stderr io.Writer, flagValue string, fromStdin bool) (string, error) {
	switch {
	case fromStdin:
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("failed to read the password from stdin: %w", err)
		}
		// strip only the trailing line ending a pipe or heredoc adds. leading and
		// inner whitespace can be part of the credential and must survive.
		secret := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
		if secret == "" {
			return "", clierr.Usagef("the password read from stdin is empty")
		}
		// a token never spans lines, so this is a redirected file with extra
		// content rather than a credential. the api would reject it with a less
		// obvious error.
		if strings.ContainsAny(secret, "\r\n") {
			return "", clierr.Usagef("the password read from stdin spans multiple lines")
		}
		return secret, nil

	case flagValue != "":
		fmt.Fprintln(stderr, "warning: --password is insecure; it is visible in the process table and your shell history. use --password-stdin instead.")
		return flagValue, nil

	case stdinIsTerminal():
		secret, err := promptPassword(stderr)
		if err != nil {
			return "", err
		}
		if secret == "" {
			return "", clierr.Usagef("no password entered")
		}
		return secret, nil

	default:
		// non-interactive with no password source is a scripting mistake, and
		// silently prompting into a pipe would hang instead of reporting it.
		return "", clierr.Usagef("a password is required; pass --password-stdin to read it from stdin, or --password")
	}
}

func runCreate(cmd *cobra.Command, args []string) error {
	password, err := resolvePassword(cmd.InOrStdin(), cmd.ErrOrStderr(), createPassword, createPasswordStdin)
	if err != nil {
		return err
	}

	client, err := api.NewClient()
	if err != nil {
		return err
	}

	req := &api.ContainerRegistryAuthCreateRequest{
		Name:     createName,
		Username: createUsername,
		Password: password,
	}

	auth, err := client.CreateContainerRegistryAuth(req)
	if err != nil {
		return fmt.Errorf("failed to create registry auth: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(auth, &output.Config{Format: format})
}
