package project

import (
	"bufio"
	"bytes"
	"cli/api"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"
	"golang.org/x/crypto/ssh"
)

const (
	pollInterval = 1 * time.Second
	maxPollTime  = 5 * time.Minute // Adjusted for clarity
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

func (sshConn *SSHConnection) Rsync(localDir string, remoteDir string, quiet bool) error {
	rsyncCmdArgs := []string{"--compress", "--archive", "--verbose", "--no-owner", "--no-group"}

	// Retrieve and apply ignore patterns
	patterns, err := GetIgnoreList()
	if err != nil {
		return fmt.Errorf("getting ignore list: %w", err)
	}
	for _, pat := range patterns {
		rsyncCmdArgs = append(rsyncCmdArgs, "--exclude", pat)
	}

	//Filter from .runpodignore
	rsyncCmdArgs = append(rsyncCmdArgs, "--filter=:- .runpodignore")

	// Prepare SSH options for rsync
	sshOptions := fmt.Sprintf("ssh %s", strings.Join(sshConn.getSshOptions(), " "))
	rsyncCmdArgs = append(rsyncCmdArgs, "-e", sshOptions, localDir, fmt.Sprintf("root@%s:%s", sshConn.podIp, remoteDir))

	// Perform a dry run to check if files need syncing
	dryRunArgs := append(rsyncCmdArgs, "--dry-run")
	dryRunCmd := exec.Command("rsync", dryRunArgs...)
	var dryRunBuf bytes.Buffer
	dryRunCmd.Stdout = &dryRunBuf
	dryRunCmd.Stderr = &dryRunBuf
	if err := dryRunCmd.Run(); err != nil {
		return fmt.Errorf("running rsync dry run: %w", err)
	}
	dryRunOutput := dryRunBuf.String()

	// Parse the dry run output to determine if files need syncing
	filesNeedSyncing := false
	scanner := bufio.NewScanner(strings.NewReader(dryRunOutput))
	for scanner.Scan() {
		line := scanner.Text()

		if line == "" || strings.Contains(line, "sending incremental file list") || strings.Contains(line, "total size is") || strings.Contains(line, "bytes/sec") || strings.Contains(line, "building file list") {
			continue
		}

		filename := filepath.Base(line)
		if filename == "" || filename == "." || strings.HasSuffix(line, "/") {
			continue
		}

		filesNeedSyncing = true
		break
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanning dry run output: %w", err)
	}

	// Add quiet flag if requested
	if quiet {
		rsyncCmdArgs = append(rsyncCmdArgs, "--quiet")
	}

	if filesNeedSyncing {
		fmt.Println("Syncing files...")

		cmd := exec.Command("rsync", rsyncCmdArgs...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("executing rsync command: %w", err)
		}
	}

	return nil
}

// hasChanges checks if there are any modified files in localDir since lastSyncTime.
func hasChanges(localDir string, lastSyncTime time.Time) (bool, string) {
	var firstModifiedFile string = ""

	err := filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				// Handle the case where a file has been removed
				fmt.Printf("Detected a removed file at: %s\n", path)
				return errors.New("change detected") // Stop walking
			}
			return err
		}

		// Check if the file was modified after the last sync time
		if info.ModTime().After(lastSyncTime) {
			firstModifiedFile = path
			return filepath.SkipDir // Skip the rest of the directory if a change is found
		}

		return nil
	})

	if err != nil {
		fmt.Printf("Error walking through directory: %v\n", err)
		return false, ""
	}

	return firstModifiedFile != "", firstModifiedFile
}

func (sshConn *SSHConnection) SyncDir(localDir string, remoteDir string) {
	syncFiles := func() {
		// fmt.Println("Syncing files...")
		err := sshConn.Rsync(localDir, remoteDir, true)
		if err != nil {
			fmt.Printf(" error: %v\n", err)
			return
		}
	}

	// Start listening for events in a separate goroutine.
	go func() {
		lastSyncTime := time.Now()
		for {
			time.Sleep(100 * time.Millisecond)
			hasChanged, firstModifiedFile := hasChanges(localDir, lastSyncTime)
			if hasChanged {
				fmt.Printf("Local changes detected in %s\n", firstModifiedFile)
				syncFiles()
				lastSyncTime = time.Now()
			}
		}
	}()

	<-make(chan struct{})
}

// RunCommand runs a command on the remote pod.
func (conn *SSHConnection) RunCommand(command string) error {
	return conn.RunCommands([]string{command})
}

// RunCommands runs a list of commands on the remote pod.
func (sshConn *SSHConnection) RunCommands(commands []string) error {
	stdoutColor, stderrColor := color.New(color.FgGreen), color.New(color.FgRed)

	for _, command := range commands {
		session, err := sshConn.client.NewSession()
		if err != nil {
			return fmt.Errorf("failed to create SSH session: %w", err)
		}
		defer session.Close()

		// Set up pipes for stdout and stderr
		stdout, err := session.StdoutPipe()
		if err != nil {
			return fmt.Errorf("failed to get stdout pipe: %w", err)
		}
		go scanAndPrint(stdout, stdoutColor, sshConn.podId, showPrefixInPodLogs)

		stderr, err := session.StderrPipe()
		if err != nil {
			return fmt.Errorf("failed to get stderr pipe: %w", err)
		}
		go scanAndPrint(stderr, stderrColor, sshConn.podId, showPrefixInPodLogs)

		// Run the command
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

// Utility function to scan and print output from SSH sessions.
func scanAndPrint(pipe io.Reader, color *color.Color, podID string, showPodIdPrefix bool) {
	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		if showPodIdPrefix {
			color.Printf("[%s] ", podID)
		}
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

	//loop until pod ready

	fmt.Print("Waiting for Pod to come online... ")
	//look up ip and ssh port for pod id
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

	// Configure the SSH client
	config := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(privateKey),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	// Connect to the SSH server
	host := fmt.Sprintf("%s:%d", podIp, podPort)
	client, err := ssh.Dial("tcp", host, config)
	if err != nil {
		return nil, fmt.Errorf("establishing SSH connection to %s: %w", host, err)
	}

	return &SSHConnection{podId: podId, client: client, podIp: podIp, podPort: podPort, sshKeyPath: sshKeyPath}, nil

}
