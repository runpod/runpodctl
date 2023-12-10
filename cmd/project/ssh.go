package project

import (
	"cli/api"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

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
	podId   string
	session *ssh.Session
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

	// Create a session
	session, err := client.NewSession()
	if err != nil {
		client.Close()
		fmt.Println("Failed to create session: %s", err)
		return nil, err
	}
	return &SSHConnection{podId: podId, session: session}, nil

}
