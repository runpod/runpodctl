package croc

import (
	"fmt"

	"github.com/spf13/cobra"
)

const scpHelp = `
----Summary----
The built-in scp command, available as part of the POSIX standard, can be used to securely copy files between your local machine
and a runpod instance you already have SSH access to.

This is not part of runpodctl - we provide this documentation for your convenience only.

Linux, MacOS, and Windows 10 all come with scp pre-installed.

--- Quick Start -----
The following quick-start guide: obtained via the tldr.sh project under the MIT license, provides a brief overview of how to use scp. https://tldr.sh/

Secure copy.
Copy files between hosts using Secure Copy Protocol over SSH.
More information: <https://man.openbsd.org/scp>.

Copy a local file to a remote host:

	scp path/to/local_file remote_host:path/to/remote_file

Use a specific port when connecting to the remote host:

	scp -P port path/to/local_file remote_host:path/to/remote_file

Copy a file from a remote host to a local directory:

	scp remote_host:path/to/remote_file path/to/local_directory

Recursively copy the contents of a directory from a remote host to a local directory:

	scp -r remote_host:path/to/remote_directory path/to/local_directory

Copy a file between two remote hosts transferring through the local host:

	scp -3 host1:path/to/remote_file host2:path/to/remote_directory

Use a specific username when connecting to the remote host:

	scp path/to/local_file remote_username@remote_host:path/to/remote_directory

Use a specific SSH private key for authentication with the remote host:

	scp -i ~/.ssh/private_key path/to/local_file remote_host:path/to/remote_file

Use a specific proxy when connecting to the remote host:

	scp -J proxy_username@proxy_host path/to/local_file remote_host:path/to/remote_file`

var SCPHelp = &cobra.Command{
	Use:   "scp-help",
	Short: "help for using scp (secure copy over SSH)",
	Args:  cobra.NoArgs,
	Run:   func(cmd *cobra.Command, args []string) { fmt.Println(scpHelp) },
}
