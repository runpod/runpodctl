package ssh

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/runpod/runpodctl/api"

	"golang.org/x/crypto/ssh"
)

const (
	pollInterval = 1 * time.Second
	maxPollTime  = 5 * time.Minute
)

func getPodSSHInfo(podID string) (string, int, error) {
	pods, err := api.GetPods()
	if err != nil {
		return "", 0, fmt.Errorf("getting pods: %w", err)
	}

	for _, pod := range pods {
		if pod.Id != podID {
			continue
		}

		if pod.DesiredStatus != "RUNNING" {
			return "", 0, fmt.Errorf("pod desired status not RUNNING")
		}
		if pod.Runtime == nil {
			return "", 0, fmt.Errorf("pod runtime is missing")
		}
		if pod.Runtime.Ports == nil {
			return "", 0, fmt.Errorf("pod runtime ports are missing")
		}
		for _, port := range pod.Runtime.Ports {
			if port.PrivatePort == 22 {
				return port.Ip, port.PublicPort, nil
			}
		}
	}
	return "", 0, fmt.Errorf("no SSH port exposed on pod %s", podID)
}

type SSHConnection struct {
	podId      string
	podIp      string
	podPort    int
	client     *ssh.Client
	sshKeyPath string
}

func (sshConn *SSHConnection) getSshOptions() []string {
	return []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "LogLevel=ERROR",
		"-p", fmt.Sprint(sshConn.podPort),
		"-i", sshConn.sshKeyPath,
	}
}

func (sshConn *SSHConnection) Rsync(localPath string, remotePath string, quiet bool) error {
	rsyncArgs := []string{"--compress", "--archive"}
	
	if quiet {
		rsyncArgs = append(rsyncArgs, "--quiet")
	} else {
		rsyncArgs = append(rsyncArgs, "--verbose")
	}

	sshOptions := fmt.Sprintf("ssh %s", strings.Join(sshConn.getSshOptions(), " "))
	rsyncArgs = append(rsyncArgs, "-e", sshOptions, localPath, fmt.Sprintf("root@%s:%s", sshConn.podIp, remotePath))

	cmd := exec.Command("rsync", rsyncArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("executing rsync command: %w", err)
	}

	return nil
}

func (conn *SSHConnection) RunCommand(command string) error {
	return conn.RunCommands([]string{command})
}

func (sshConn *SSHConnection) RunCommands(commands []string) error {
	for _, command := range commands {
		session, err := sshConn.client.NewSession()
		if err != nil {
			return fmt.Errorf("failed to create SSH session: %w", err)
		}
		defer session.Close()

		stdout, err := session.StdoutPipe()
		if err != nil {
			return fmt.Errorf("failed to get stdout pipe: %w", err)
		}
		go scanAndPrint(stdout)

		stderr, err := session.StderrPipe()
		if err != nil {
			return fmt.Errorf("failed to get stderr pipe: %w", err)
		}
		go scanAndPrint(stderr)

		fullCommand := strings.Join([]string{
			"source /root/.bashrc",
			"source /etc/rp_environment",
			"while IFS= read -r -d '' line; do export \"$line\"; done < /proc/1/environ",
			command,
		}, " && ")

		if err := session.Run(fullCommand); err != nil {
			return fmt.Errorf("failed to run command %q: %w", command, err)
		}
	}
	return nil
}

func scanAndPrint(pipe io.Reader) {
	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		fmt.Println(scanner.Text())
	}
}

func PodSSHConnection(podId string) (*SSHConnection, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("getting user home directory: %w", err)
	}

	sshKeyPath := filepath.Join(homeDir, ".runpod", "ssh", "RunPod-Key-Go")
	privateKeyBytes, err := os.ReadFile(sshKeyPath)
	if err != nil {
		return nil, fmt.Errorf("reading private SSH key from %s: %w", sshKeyPath, err)
	}

	privateKey, err := ssh.ParsePrivateKey(privateKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("parsing private SSH key: %w", err)
	}

	fmt.Print("Waiting for Pod to come online... ")
	var podIp string
	var podPort int

	startTime := time.Now()
	for podIp, podPort, err = getPodSSHInfo(podId); err != nil && time.Since(startTime) < maxPollTime; {
		time.Sleep(pollInterval)
		podIp, podPort, err = getPodSSHInfo(podId)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get SSH info for pod %s: %w", podId, err)
	} else if time.Since(startTime) >= time.Duration(maxPollTime) {
		return nil, fmt.Errorf("timeout waiting for pod %s to come online", podId)
	}

	config := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(privateKey),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	host := fmt.Sprintf("%s:%d", podIp, podPort)
	client, err := ssh.Dial("tcp", host, config)
	if err != nil {
		return nil, fmt.Errorf("establishing SSH connection to %s: %w", host, err)
	}

	return &SSHConnection{podId: podId, client: client, podIp: podIp, podPort: podPort, sshKeyPath: sshKeyPath}, nil
}
