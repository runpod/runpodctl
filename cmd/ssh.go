package cmd

import (
	"github.com/runpod/runpodctl/cmd/ssh"

	"github.com/spf13/cobra"
)

var sshCmd = &cobra.Command{
	Use:   "ssh",
	Short: "SSH keys and commands",
	Long:  "SSH key management and connection to pods",
}

func init() {
	sshCmd.AddCommand(ssh.ListKeysCmd)
	sshCmd.AddCommand(ssh.AddKeyCmd)
}
