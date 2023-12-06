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
	if !(pod.DesiredStatus == "RUNNING" && pod.Runtime != nil && pod.Runtime.Ports != nil) {
		return "", 0, errors.New("pod ports not ready for ssh conn")
	}
	for _, port := range pod.Runtime.Ports {
		if port.PrivatePort == 22 {
			return podIp, port.PublicPort, nil
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
	home, _ := os.Getwd()
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
	//look up ip and ssh port for pod id
	var podIp string
	var podPort int
	for podIp, podPort, err = getPodSSHInfo(podId); err != nil && int(time.Now().Sub(startTime)) < maxPollTimeSeconds; {
		time.Sleep(time.Second * time.Duration(pollIntervalSeconds))
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
	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", podIp, podPort), config)
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