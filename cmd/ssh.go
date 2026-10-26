package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/runpod/runpodctl/cmd/ssh"
	"github.com/runpod/runpodctl/internal/agent"
	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"
	"github.com/runpod/runpodctl/internal/podssh"
	"github.com/runpod/runpodctl/internal/sshconnect"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var sshCmd = &cobra.Command{
	Use:   "ssh",
	Short: "manage ssh keys and connections",
	Long:  "manage ssh keys and show ssh info for pods. uses the api key from RUNPOD_API_KEY or ~/.runpod/config.toml (runpodctl doctor).",
}

var sshListKeysCmd = &cobra.Command{
	Use:   "list-keys",
	Short: "list all ssh keys",
	Long:  "list all ssh keys associated with your account",
	RunE:  runSSHListKeys,
}

var sshAddKeyCmd = &cobra.Command{
	Use:   "add-key",
	Short: "add an ssh key",
	Long:  "add an ssh key to your account",
	RunE:  runSSHAddKey,
}

var sshRemoveKeyCmd = &cobra.Command{
	Use:   "remove-key",
	Short: "remove an ssh key",
	Long:  "remove an ssh key from your account",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		// --invalid with --name or --fingerprint is refused by cobra
		// (MarkFlagsMutuallyExclusive), as a usage error
		if sshKeyInvalid {
			return nil
		}
		if sshKeyName == "" && sshKeyFingerprint == "" {
			return fmt.Errorf("either --fingerprint, --name or --invalid must be provided")
		}
		return nil
	},
	RunE: runSSHRemoveKey,
}

var sshInfoCmd = &cobra.Command{
	Use:   "info <pod-id>",
	Short: "show ssh info for a pod",
	Long:  "show ssh info for a pod (command + key). does not connect.",
	Args:  cobra.ExactArgs(1),
	RunE:  runSSHInfo,
}

var sshConnectCmd = &cobra.Command{
	Use:        "connect [pod-id]",
	Short:      "deprecated: use 'runpodctl ssh info'",
	Long:       "deprecated alias for 'runpodctl ssh info'",
	Args:       cobra.MaximumNArgs(1),
	Deprecated: "use 'runpodctl ssh info' instead",
	Hidden:     true,
	RunE:       runSSHConnectLegacy,
}

var sshForgetCmd = &cobra.Command{
	Use:   "forget <pod-id>",
	Short: "forget the host key recorded for a pod",
	Long: "forget the host key that 'exec' and 'project' recorded for a pod in ~/.runpod/ssh/known_hosts, " +
		"so the next connection trusts the key the pod then offers. use it after a stop/start or update changed " +
		"the pod's host key; if the pod did not restart, a mismatch may be an intercepted connection. " +
		"'ssh info' is unaffected: the ssh command it prints uses your own ~/.ssh/known_hosts.",
	Args: cobra.ExactArgs(1),
	RunE: runSSHForget,
}

var (
	sshKeyFile        string
	sshKey            string
	sshKeyName        string
	sshKeyFingerprint string
	sshKeyInvalid     bool
	sshRemoveInvalid  bool
	sshVerbose        bool
)

func init() {
	sshCmd.AddCommand(sshListKeysCmd)
	sshCmd.AddCommand(sshAddKeyCmd)
	sshCmd.AddCommand(sshRemoveKeyCmd)
	sshCmd.AddCommand(sshInfoCmd)
	sshCmd.AddCommand(sshConnectCmd)
	sshCmd.AddCommand(sshForgetCmd)

	sshAddKeyCmd.Flags().StringVar(&sshKey, "key", "", "the public key to add")
	sshAddKeyCmd.Flags().StringVar(&sshKeyFile, "key-file", "", "file containing the public key")
	sshRemoveKeyCmd.Flags().StringVar(&sshKeyFingerprint, "fingerprint", "", "fingerprint of the key to remove")
	sshRemoveKeyCmd.Flags().StringVar(&sshKeyName, "name", "", "name of the key to remove")
	sshAddKeyCmd.Flags().BoolVar(&sshRemoveInvalid, "remove-invalid", false, "also delete saved entries that aren't valid public keys, which block every key change, without asking")
	sshRemoveKeyCmd.Flags().BoolVar(&sshRemoveInvalid, "remove-invalid", false, "also delete saved entries that aren't valid public keys, which block every key change, without asking")
	sshRemoveKeyCmd.Flags().BoolVar(&sshKeyInvalid, "invalid", false, "remove every saved entry that isn't a valid public key (they block all key changes)")
	sshRemoveKeyCmd.MarkFlagsMutuallyExclusive("invalid", "name")
	sshRemoveKeyCmd.MarkFlagsMutuallyExclusive("invalid", "fingerprint")

	sshInfoCmd.Flags().BoolVarP(&sshVerbose, "verbose", "v", false, "include pod id and name in output")
	sshConnectCmd.Flags().BoolVarP(&sshVerbose, "verbose", "v", false, "include pod id and name in output")
}

func runSSHListKeys(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	keys, err := client.GetPublicSSHKeys()
	if err != nil {
		return fmt.Errorf("failed to get ssh keys: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(map[string]interface{}{"keys": keys}, &output.Config{Format: format})
}

func runSSHAddKey(cmd *cobra.Command, args []string) error {
	var publicKey []byte
	var err error

	if sshKey == "" && sshKeyFile == "" {
		// Interactive mode
		if !confirmAddKey() {
			fmt.Fprintln(os.Stderr, "operation aborted")
			return nil
		}
		keyName := promptKeyName()
		publicKey, err = ssh.GenerateSSHKeyPair(keyName)
		if err != nil {
			return fmt.Errorf("failed to generate ssh key: %w", err)
		}
	} else if sshKeyFile != "" {
		publicKey, err = os.ReadFile(sshKeyFile)
		if err != nil {
			return fmt.Errorf("failed to read key file: %w", err)
		}
	} else {
		publicKey = []byte(sshKey)
	}

	client, err := api.NewClient()
	if err != nil {
		return err
	}

	err = withInvalidKeyPrompt(sshRemoveInvalid, func(removeInvalid bool) error {
		return client.AddPublicSSHKey(publicKey, removeInvalid)
	})
	if err != nil {
		return fmt.Errorf("failed to add ssh key: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(map[string]interface{}{"added": true}, &output.Config{Format: format})
}

func runSSHRemoveKey(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	if sshKeyInvalid {
		n, err := client.RemoveInvalidSSHKeys()
		if err != nil {
			return fmt.Errorf("failed to remove invalid ssh key entries: %w", err)
		}
		return output.Print(map[string]interface{}{"removed": n > 0, "removedCount": n}, &output.Config{Format: format})
	}

	err = withInvalidKeyPrompt(sshRemoveInvalid, func(removeInvalid bool) error {
		return client.RemovePublicSSHKey(sshKeyName, sshKeyFingerprint, removeInvalid)
	})
	if err != nil {
		return fmt.Errorf("failed to remove ssh key: %w", err)
	}
	return output.Print(map[string]interface{}{"removed": true}, &output.Config{Format: format})
}

func runSSHInfo(cmd *cobra.Command, args []string) error {
	return runSSHInfoWithArgs(cmd, args, false)
}

func runSSHConnectLegacy(cmd *cobra.Command, args []string) error {
	return runSSHInfoWithArgs(cmd, args, true)
}

func runSSHInfoWithArgs(cmd *cobra.Command, args []string, allowAll bool) error {
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	pods, err := client.GetPods()
	if err != nil {
		return fmt.Errorf("failed to get pods: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	keyInfo := sshconnect.ResolveKeyInfo(client)

	if allowAll && len(args) == 0 {
		connections := sshconnect.ListConnections(pods, keyInfo)
		return output.Print(map[string]interface{}{
			"connections": connections,
		}, &output.Config{Format: format})
	}

	// Show connect info for specific pod
	nameOrID := args[0]
	pod, conn := sshconnect.FindPodConnection(pods, nameOrID, keyInfo)
	if pod != nil {
		// the same derivation `pod get` uses, so both commands explain a
		// not-ready pod identically instead of saying only "pod not ready".
		state := sshconnect.PodState(pod)
		// A stopped pod keeps reporting stale runtime ports for a while, which
		// is enough to build an ssh command that cannot possibly connect. The
		// gate is "known down", not "not running": an unknown state is no
		// evidence the pod is down, so a connection built from live ports is
		// still worth returning.
		if conn == nil || state.IsKnownDown() {
			var runtimePorts []*api.LegacyPort
			if pod.Runtime != nil {
				runtimePorts = pod.Runtime.Ports
			}
			message := sshconnect.NotReadyMessage(pod.ID, state, sshconnect.SplitPorts(pod.Ports), runtimePorts)
			return output.Print(map[string]interface{}{
				"error":         message,
				"id":            pod.ID,
				"name":          pod.Name,
				"status":        pod.DesiredStatus,
				"runtimeStatus": string(state.Status),
			}, &output.Config{Format: format})
		}
		return output.Print(conn, &output.Config{Format: format})
	}

	// return only; Execute is the single error sink and tags the code.
	return api.NewNotFoundError("pod '%s' not found", nameOrID)
}

func runSSHForget(cmd *cobra.Command, args []string) error {
	podID := args[0]
	path, fingerprints, err := podssh.ForgetHostKey(podID)
	if err != nil {
		return fmt.Errorf("failed to forget host key for pod %s: %w", podID, err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(map[string]interface{}{
		"podId":        podID,
		"forgotten":    len(fingerprints) > 0,
		"fingerprints": fingerprints,
		"knownHosts":   path,
	}, &output.Config{Format: format})
}

func confirmAddKey() bool {
	fmt.Fprint(os.Stderr, "would you like to add an ssh key to your account? (y/n) ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	return strings.ToLower(scanner.Text()) == "y"
}

func promptKeyName() string {
	fmt.Fprintf(os.Stderr, "please enter a name for this key (default '%s'): ", ssh.DefaultKeyName)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	keyName := scanner.Text()
	if keyName == "" {
		return ssh.DefaultKeyName
	}
	return strings.ReplaceAll(keyName, " ", "-")
}

// invalidKeyPromptIsInteractive reports whether the prompt can be seen and
// answered by a person: stdin and stderr are terminals, and no ai agent is
// driving the cli (agents often run in a pseudo-terminal, and a prompt would
// hang them). otherwise the error names --remove-invalid instead.
var invalidKeyPromptIsInteractive = func() bool {
	return agent.Detect() == "" && term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd()))
}

// invalidKeyPromptIn and invalidKeyPromptOut are the prompt's ends; it goes to
// stderr so stdout stays the command's json.
var (
	invalidKeyPromptIn  io.Reader = os.Stdin
	invalidKeyPromptOut io.Writer = os.Stderr
)

// withInvalidKeyPrompt runs write, and when the account's key list holds saved
// entries v2 rejects, asks a person whether to delete them and retries. with
// removeInvalid set it deletes them without asking; non-interactive, the
// error is returned as is and names --remove-invalid.
func withInvalidKeyPrompt(removeInvalid bool, write func(removeInvalid bool) error) error {
	err := write(removeInvalid)
	var invalid *api.InvalidStoredSSHKeysError
	if removeInvalid || !errors.As(err, &invalid) || !invalidKeyPromptIsInteractive() {
		return err
	}
	n := len(invalid.Entries)
	fmt.Fprintf(invalidKeyPromptOut, "your account's ssh key list has %d saved %s that %s not a valid public key, which blocks every key change:\n",
		n, api.Plural(n, "entry", "entries"), api.Plural(n, "is", "are"))
	for _, entry := range invalid.Entries {
		fmt.Fprintf(invalidKeyPromptOut, "  %s\n", api.TruncateRunes(entry, 60))
	}
	fmt.Fprintf(invalidKeyPromptOut, "delete %s and continue? (y/n) ", api.Plural(n, "it", "them"))
	scanner := bufio.NewScanner(invalidKeyPromptIn)
	scanner.Scan()
	if strings.ToLower(strings.TrimSpace(scanner.Text())) != "y" {
		return err
	}
	return write(true)
}
