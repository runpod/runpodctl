package exec

import (
	"fmt"
	"strings"

	"github.com/runpod/runpodctl/cmd/project"
)

func PythonOverSSH(podID string, file string, pythonCommand string) error {
	sshConn, err := project.PodSSHConnection(podID)
	if err != nil {
		return fmt.Errorf("getting SSH connection: %w", err)
	}

	// Copy the file to the pod using Rsync
	if err := sshConn.Rsync(file, "/tmp/"+file, false); err != nil {
		return fmt.Errorf("copying file to pod: %w", err)
	}

	// Run the file on the pod
	pythonCommand = strings.TrimSpace(pythonCommand)
	if pythonCommand == "" {
		pythonCommand = "python3"
	}
	if err := sshConn.RunCommand(pythonCommand + " /tmp/" + file); err != nil {
		return fmt.Errorf("running Python command: %w", err)
	}

	return nil
}
