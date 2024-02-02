package project

import (
	"bufio"
	"cli/api"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"
	"golang.org/x/crypto/ssh"
)

func getPodSSHInfo(podId string) (podIp string, podPort int, err error) {
	pods, err := api.GetPods()
	if err != nil {
		return "", 0, err
	}
	var pod api.Pod
	for _, p := range pods {
		if p.Id == podId {
			pod = *p
		}
	}
	//is pod ready for ssh yet?
	if pod.DesiredStatus != "RUNNING" {
		return "", 0, errors.New("pod desired status not RUNNING")
	}
	if pod.Runtime == nil {
		return "", 0, errors.New("pod runtime is nil")
	}
	if pod.Runtime.Ports == nil {
		return "", 0, errors.New("pod runtime ports is nil")
	}
	for _, port := range pod.Runtime.Ports {
		if port.PrivatePort == 22 {
			return port.Ip, port.PublicPort, nil
		}
	}
	return "", 0, errors.New("no SSH port exposed on Pod")
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
	rsyncCmdArgs := []string{"-avz", "--no-owner", "--no-group"}
	patterns, err := GetIgnoreList()
	if err != nil {
		return err
	}
	for _, pat := range patterns {
		rsyncCmdArgs = append(rsyncCmdArgs, "--exclude", pat)
	}
	if quiet {
		rsyncCmdArgs = append(rsyncCmdArgs, "--quiet")
	}

	sshOptions := strings.Join(sshConn.getSshOptions(), " ")
	rsyncCmdArgs = append(rsyncCmdArgs, "-e", fmt.Sprintf("ssh %s", sshOptions))
	rsyncCmdArgs = append(rsyncCmdArgs, localDir, fmt.Sprintf("root@%s:%s", sshConn.podIp, remoteDir))
	cmd := exec.Command("rsync", rsyncCmdArgs...)
	cmd.Stdout = os.Stdout
	if err := cmd.Run(); err != nil {
		fmt.Println("could not run rsync command: ", err)
		return err
	}
	return nil
}

// hasChanges checks if there are any modified files in localDir since lastSyncTime.
func hasChanges(localDir string, lastSyncTime time.Time) (bool, string) {
	var hasModifications bool
	var firstModifiedFile string

	err := filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				// Handle the case where a file has been removed
				fmt.Printf("Detected a removed file at: %s\n", path)
				hasModifications = true
				return errors.New("change detected") // Stop walking
			}
			return err
		}

		// Check if the file was modified after the last sync time
		if info.ModTime().After(lastSyncTime) {
			hasModifications = true
			firstModifiedFile = path
			return filepath.SkipDir // Skip the rest of the directory if a change is found
		}

		return nil
	})

	if err != nil {
		fmt.Printf("Error walking through directory: %v\n", err)
		return false, ""
	}

	return hasModifications, firstModifiedFile
}

func (sshConn *SSHConnection) SyncDir(localDir string, remoteDir string) {
	syncFiles := func() {
		fmt.Println("Syncing files...")
		sshConn.Rsync(localDir, remoteDir, true)
	}

	// Start listening for events.
	go func() {
		lastSyncTime := time.Now()
		for {
			time.Sleep(100 * time.Millisecond)
			hasChanged, firstModifiedFile := hasChanges(localDir, lastSyncTime)
			if hasChanged {
				fmt.Printf("Found changes in %s\n", firstModifiedFile)
				syncFiles()
				lastSyncTime = time.Now()
			}
		}
	}()

	// Block main goroutine forever.
	<-make(chan struct{})
}

func (sshConn *SSHConnection) RunCommand(command string) error {
	return sshConn.RunCommands([]string{command})
}

func (sshConn *SSHConnection) RunCommands(commands []string) error {

	stdoutColor := color.New(color.FgGreen)
	stderrColor := color.New(color.FgRed)

	for _, command := range commands {
		// Create a session
		session, err := sshConn.client.NewSession()
		if err != nil {
			fmt.Println("Failed to create session: %s", err)
			return err
		}

		stdout, err := session.StdoutPipe()
		if err != nil {
			return err
		}
		stderr, err := session.StderrPipe()
		if err != nil {
			return err
		}

		//listen to stdout
		go func() {
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				if showPrefixInPodLogs {
					stdoutColor.Printf("[%s] ", sshConn.podId)
				}
				fmt.Println(scanner.Text())
			}
		}()

		//listen to stderr
		go func() {
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				if showPrefixInPodLogs {
					stderrColor.Printf("[%s] ", sshConn.podId)
				}
				fmt.Println(scanner.Text())
			}
		}()
		fullCommand := strings.Join([]string{
			"source /root/.bashrc",
			"source /etc/rp_environment",
			"while IFS= read -r -d '' line; do export \"$line\"; done < /proc/1/environ",
			command,
		}, " && ")
		err = session.Run(fullCommand)
		if err != nil {
			session.Close()
			return err
		}
		session.Close()
	}
	return nil
}

func PodSSHConnection(podId string) (*SSHConnection, error) {
	//check ssh key exists
	home, _ := os.UserHomeDir()
	sshFilePath := filepath.Join(home, ".runpod", "ssh", "RunPod-Key-Go")
	privateKeyBytes, err := os.ReadFile(sshFilePath)
	if err != nil {
		fmt.Println("failed to get private key")
		return nil, err
	}
	privateKey, err := ssh.ParsePrivateKey(privateKeyBytes)
	if err != nil {
		fmt.Println("failed to parse private key")
		return nil, err
	}
	//loop until pod ready
	pollIntervalSeconds := 1
	maxPollTimeSeconds := 300
	startTime := time.Now()
	fmt.Print("Waiting for Pod to come online... ")
	//look up ip and ssh port for pod id
	var podIp string
	var podPort int
	for podIp, podPort, err = getPodSSHInfo(podId); err != nil && time.Since(startTime) < time.Duration(maxPollTimeSeconds*int(time.Second)); {
		time.Sleep(time.Duration(pollIntervalSeconds * int(time.Second)))
		podIp, podPort, err = getPodSSHInfo(podId)
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
		fmt.Println("Failed to dial for SSH conn: %s", err)
		return nil, err
	}

	return &SSHConnection{podId: podId, client: client, podIp: podIp, podPort: podPort, sshKeyPath: sshFilePath}, nil

}
