package registry

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

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

the password can come from --password, from stdin with --password-stdin, or from
an interactive prompt when neither flag is given. --password-stdin keeps the
credential out of the process table and your shell history.`,
	Example: `  # read the password from a pipe
  printenv REGISTRY_TOKEN | runpodctl registry create --name ghcr --username me --password-stdin

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
	createCmd.Flags().StringVar(&createPassword, "password", "", "registry password; see also --password-stdin")
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
	fd := int(os.Stdin.Fd())

	// ReadPassword disables echo and restores it from a deferred ioctl, which a
	// signal's default disposition never reaches: a ctrl-c at the prompt would
	// kill the process with echo still off and leave the caller's shell typing
	// blind until they ran `stty sane`. Restore it from a handler instead.
	state, err := term.GetState(fd)
	if err != nil {
		return "", fmt.Errorf("failed to read the terminal state: %w", err)
	}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)
	defer signal.Stop(sigCh)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case sig := <-sigCh:
			_ = term.Restore(fd, state)
			fmt.Fprintln(stderr)
			// 128 + the signal number is the shell convention for "died on a
			// signal". The deferred restores below are deliberately skipped: the
			// terminal is already back and there is nothing else to unwind at a
			// prompt.
			os.Exit(signalExitCode(sig))
		case <-done:
		}
	}()

	fmt.Fprint(stderr, "registry password: ")
	secret, err := term.ReadPassword(fd)
	// ReadPassword swallows the user's newline, so the next line of output would
	// otherwise start on the prompt line.
	fmt.Fprintln(stderr)
	if err != nil {
		return "", fmt.Errorf("failed to read the password from the terminal: %w", err)
	}
	return string(secret), nil
}

func signalExitCode(sig os.Signal) int {
	if signalNumber, ok := sig.(syscall.Signal); ok {
		return 128 + int(signalNumber)
	}
	return 1
}

// stdinIsTerminal reports whether stdin is a terminal we can prompt on, as
// opposed to a pipe or a redirected file.
var stdinIsTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// resolvePassword picks the registry password out of the three supported
// sources. It takes the reader and writer rather than touching os.Stdin/Stderr
// directly so the whole matrix is testable without a tty.
//
// --password stays a first-class choice: it is the caller's call whether argv
// exposure matters for their environment, and scripts already pass it that way.
func resolvePassword(stdin io.Reader, stderr io.Writer, flagValue string, fromStdin bool) (string, error) {
	switch {
	case fromStdin:
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("failed to read the password from stdin: %w", err)
		}
		// strip only the trailing line ending a pipe or heredoc adds. everything
		// else survives: a gcr.io / artifact registry credential is a whole
		// service-account json key (username _json_key), so the value is
		// legitimately multi-line, and the api accepts it. leading and inner
		// whitespace can be significant too.
		secret := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
		if secret == "" {
			return "", clierr.Usagef("the password read from stdin is empty")
		}
		return secret, nil

	case flagValue != "":
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
	// build the client before touching the password sources: a missing api key
	// should fail here, not after consuming a one-shot stdin secret or making the
	// user type a password that can never be submitted.
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	password, err := resolvePassword(cmd.InOrStdin(), cmd.ErrOrStderr(), createPassword, createPasswordStdin)
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
