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
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/fsnotify/fsnotify"
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
	return "", 0, errors.New("no SSH port exposed on pod")
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
	rsyncCmd := []string{"rsync", "-avz", "--no-owner", "--no-group"}
	patterns, err := GetIgnoreList()
	if err != nil {
		return err
	}
	for _, pat := range patterns {
		rsyncCmd = append(rsyncCmd, "--exclude", pat)
	}
	if quiet {
		rsyncCmd = append(rsyncCmd, "--quiet")
	}

	rsyncCmd = append(rsyncCmd, sshConn.getSshOptions()...)
	rsyncStr := strings.Join(rsyncCmd, " ")
	cmd := exec.Command(rsyncStr)
	cmd.Stdout = os.Stdout
	if err := cmd.Run(); err != nil {
		fmt.Println("could not run rsync command: ", err)
		return err
	}
	return nil
}

func (sshConn *SSHConnection) SyncDir(localDir string, remoteDir string) {
	syncFiles := func() {
		fmt.Println("Syncing files...")
		sshConn.Rsync(localDir, remoteDir, true)
	}
	// Create new watcher.
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		fmt.Println(err)
	}
	defer watcher.Close()
	var mu sync.Mutex
	var timer *time.Timer

	// Start listening for events.
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					fmt.Println("Modified file:", event.Name)

					mu.Lock()
					if timer != nil {
						timer.Stop()
					}
					timer = time.AfterFunc(500*time.Millisecond, syncFiles)
					mu.Unlock()
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				fmt.Println("error:", err)
			}
		}
	}()

	// Add a path.
	err = watcher.Add(localDir)
	if err != nil {
		fmt.Println(err)
	}

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
		fmt.Println("running command", command)
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
				stdoutColor.Printf("[%s] ", sshConn.podId)
				fmt.Println(scanner.Text())
			}
		}()

		//listen to stderr
		go func() {
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				stderrColor.Printf("[%s] ", sshConn.podId)
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
	fmt.Print("Waiting for pod to come online... ")
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
		fmt.Println("Failed to dial for ssh conn: %s", err)
		return nil, err
	}

	return &SSHConnection{podId: podId, client: client, podIp: podIp, podPort: podPort, sshKeyPath: sshFilePath}, nil

}
