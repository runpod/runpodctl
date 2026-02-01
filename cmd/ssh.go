package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/runpod/runpod/internal/api"
	"github.com/runpod/runpod/internal/output"

	"github.com/spf13/cobra"
)

var sshCmd = &cobra.Command{
	Use:   "ssh",
	Short: "manage ssh keys and connections",
	Long:  "manage ssh keys and show connection commands for pods",
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

var sshConnectCmd = &cobra.Command{
	Use:   "connect [pod-id]",
	Short: "show ssh connect command for pods",
	Long:  "show the ssh connect command for a specific pod or all pods",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runSSHConnect,
}

var (
	sshKeyFile string
	sshKey     string
	sshVerbose bool
)

func init() {
	sshCmd.AddCommand(sshListKeysCmd)
	sshCmd.AddCommand(sshAddKeyCmd)
	sshCmd.AddCommand(sshConnectCmd)

	sshAddKeyCmd.Flags().StringVar(&sshKey, "key", "", "the public key to add")
	sshAddKeyCmd.Flags().StringVar(&sshKeyFile, "key-file", "", "file containing the public key")

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
		publicKey, err = generateSSHKeyPair(keyName)
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

func runSSHConnect(cmd *cobra.Command, args []string) error {
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

	if len(args) == 0 {
		// Show connect info for all pods
		var connections []map[string]interface{}
		for _, pod := range pods {
			conn := getConnectInfo(pod)
			if conn != nil {
				connections = append(connections, conn)
			}
		}
		return output.Print(map[string]interface{}{"connections": connections}, &output.Config{Format: format})
	}

	// Show connect info for specific pod
	nameOrID := args[0]
	for _, pod := range pods {
		if pod.ID == nameOrID || pod.Name == nameOrID {
			conn := getConnectInfo(pod)
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
	}

	errData := map[string]interface{}{"error": fmt.Sprintf("pod '%s' not found", nameOrID)}
	data, _ := json.Marshal(errData)
	fmt.Fprintln(os.Stderr, string(data))
	return fmt.Errorf("pod '%s' not found", nameOrID)
}

func getConnectInfo(pod *api.LegacyPod) map[string]interface{} {
	if pod.Runtime == nil || pod.Runtime.Ports == nil {
		return nil
	}

	for _, port := range pod.Runtime.Ports {
		if port.IsIpPublic && port.PrivatePort == 22 {
			return map[string]interface{}{
				"id":      pod.ID,
				"name":    pod.Name,
				"command": fmt.Sprintf("ssh root@%s -p %d", port.Ip, port.PublicPort),
				"ip":      port.Ip,
				"port":    port.PublicPort,
			}
		}
	}

	return nil
}

func confirmAddKey() bool {
	fmt.Fprint(os.Stderr, "would you like to add an ssh key to your account? (y/n) ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	return strings.ToLower(scanner.Text()) == "y"
}

func promptKeyName() string {
	fmt.Fprint(os.Stderr, "please enter a name for this key (default 'runpod-cli-key'): ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	keyName := scanner.Text()
	if keyName == "" {
		return "runpod-cli-key"
	}
	return strings.ReplaceAll(keyName, " ", "-")
}
