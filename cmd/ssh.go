package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/runpod/runpodctl/cmd/ssh"
	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"
	"github.com/runpod/runpodctl/internal/sshconnect"

	"github.com/spf13/cobra"
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
		if sshKeyName == "" && sshKeyFingerprint == "" {
			return fmt.Errorf("either --fingerprint or --name must be provided")
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

var (
	sshKeyFile        string
	sshKey            string
	sshKeyName        string
	sshKeyFingerprint string
	sshVerbose        bool
)

func init() {
	sshCmd.AddCommand(sshListKeysCmd)
	sshCmd.AddCommand(sshAddKeyCmd)
	sshCmd.AddCommand(sshRemoveKeyCmd)
	sshCmd.AddCommand(sshInfoCmd)
	sshCmd.AddCommand(sshConnectCmd)

	sshAddKeyCmd.Flags().StringVar(&sshKey, "key", "", "the public key to add")
	sshAddKeyCmd.Flags().StringVar(&sshKeyFile, "key-file", "", "file containing the public key")
	sshRemoveKeyCmd.Flags().StringVar(&sshKeyFingerprint, "fingerprint", "", "fingerprint of the key to remove")
	sshRemoveKeyCmd.Flags().StringVar(&sshKeyName, "name", "", "name of the key to remove")

	sshInfoCmd.Flags().BoolVarP(&sshVerbose, "verbose", "v", false, "include pod id and name in output")
	sshConnectCmd.Flags().BoolVarP(&sshVerbose, "verbose", "v", false, "include pod id and name in output")
}

func runSSHListKeys(cmd *cobra.Command, args []string) error {
	client, err := api.NewGraphQLClient()
	if err != nil {
		output.Error(err)
		return err
	}

	_, keys, err := client.GetPublicSSHKeys()
	if err != nil {
		output.Error(err)
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
			output.Error(err)
			return fmt.Errorf("failed to generate ssh key: %w", err)
		}
	} else if sshKeyFile != "" {
		publicKey, err = os.ReadFile(sshKeyFile)
		if err != nil {
			output.Error(err)
			return fmt.Errorf("failed to read key file: %w", err)
		}
	} else {
		publicKey = []byte(sshKey)
	}

	client, err := api.NewGraphQLClient()
	if err != nil {
		output.Error(err)
		return err
	}

	if err := client.AddPublicSSHKey(publicKey); err != nil {
		output.Error(err)
		return fmt.Errorf("failed to add ssh key: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(map[string]interface{}{"added": true}, &output.Config{Format: format})
}

func runSSHRemoveKey(cmd *cobra.Command, args []string) error {
	client, err := api.NewGraphQLClient()
	if err != nil {
		return err
	}

	if err := client.RemovePublicSSHKey(sshKeyName, sshKeyFingerprint); err != nil {
		return fmt.Errorf("failed to remove ssh key: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(map[string]interface{}{"removed": true}, &output.Config{Format: format})
}

func runSSHInfo(cmd *cobra.Command, args []string) error {
	return runSSHInfoWithArgs(cmd, args, false)
}

func runSSHConnectLegacy(cmd *cobra.Command, args []string) error {
	return runSSHInfoWithArgs(cmd, args, true)
}

func runSSHInfoWithArgs(cmd *cobra.Command, args []string, allowAll bool) error {
	client, err := api.NewGraphQLClient()
	if err != nil {
		output.Error(err)
		return err
	}

	pods, err := client.GetPods()
	if err != nil {
		output.Error(err)
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
		if conn == nil {
			return output.Print(map[string]interface{}{
				"error":  "pod not ready",
				"id":     pod.ID,
				"name":   pod.Name,
				"status": pod.DesiredStatus,
			}, &output.Config{Format: format})
		}
		return output.Print(conn, &output.Config{Format: format})
	}

	errData := map[string]interface{}{"error": fmt.Sprintf("pod '%s' not found", nameOrID)}
	data, _ := json.Marshal(errData)
	fmt.Fprintln(os.Stderr, string(data))
	return fmt.Errorf("pod '%s' not found", nameOrID)
}

func confirmAddKey() bool {
	fmt.Fprint(os.Stderr, "would you like to add an ssh key to your account? (y/n) ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	return strings.ToLower(scanner.Text()) == "y"
}

func promptKeyName() string {
	fmt.Fprint(os.Stderr, "please enter a name for this key (default 'RunPod-Key-Go'): ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	keyName := scanner.Text()
	if keyName == "" {
		return "RunPod-Key-Go"
	}
	return strings.ReplaceAll(keyName, " ", "-")
}
