package ssh

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/runpod/runpodctl/api"

	"github.com/spf13/cobra"
)

// ListKeysCmd defines the command to list all SSH keys for the current user.
var ListKeysCmd = &cobra.Command{
	Use:   "list-keys",
	Short: "List all SSH keys",
	Long:  `List all the SSH keys associated with the current user's account.`,
	Run: func(cmd *cobra.Command, args []string) {
		_, keys, err := api.GetPublicSSHKeys()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting SSH keys: %v\n", err)
			return
		}

		if len(keys) == 0 {
			fmt.Println("No SSH keys found.")
			return
		}

		displaySSHKeys(keys)
	},
}

// displaySSHKeys prints the SSH keys in a tabulated format.
func displaySSHKeys(keys []api.SSHKey) {
	w := tabwriter.NewWriter(os.Stdout, 8, 8, 2, ' ', 0)

	fmt.Fprintln(w, "Name\tType\tFingerprint")
	fmt.Fprintln(w, "----\t----\t-----------")

	for _, key := range keys {
		if key.Name == "" {
			key.Name = "N/A"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", key.Name, key.Type, key.Fingerprint)
	}

	if err := w.Flush(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write keys to table: %v\n", err)
	}
}

var AddKeyCmd = &cobra.Command{
	Use:   "add-key",
	Short: "Adds an SSH key to the current user account",
	Long:  `Adds an SSH key to the current user account. If no key is provided, one will be generated.`,
	Run: func(cmd *cobra.Command, args []string) {
		key, _ := cmd.Flags().GetString("key")
		keyFile, _ := cmd.Flags().GetString("key-file")

		var publicKey []byte
		var err error

		if key == "" && keyFile == "" {
			if !confirmAddKey() {
				fmt.Println("Operation aborted.")
				return
			}
			keyName := promptKeyName()
			publicKey, err = GenerateSSHKeyPair(keyName)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to generate SSH key: %v\n", err)
				return
			}
		} else if keyFile != "" {
			publicKey, err = os.ReadFile(keyFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to read key file: %v\n", err)
				return
			}
		}

		if err := api.AddPublicSSHKey(publicKey); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to add the SSH key: %v\n", err)
			return
		}

		fmt.Println("The key has been added to your account.")
	},
}

func confirmAddKey() bool {
	fmt.Print("Would you like to add an SSH key to your account? (y/n) ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	return strings.ToLower(scanner.Text()) == "y"
}

func promptKeyName() string {
	fmt.Print("Please enter a name for this key (default 'RunPod-Key'): ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	keyName := scanner.Text()
	if keyName == "" {
		return "RunPod-Key"
	}
	return strings.ReplaceAll(keyName, " ", "-")
}

func init() {
	AddKeyCmd.Flags().String("key", "", "The public key to add.")
	AddKeyCmd.Flags().String("key-file", "", "The file containing the public key to add.")
}
