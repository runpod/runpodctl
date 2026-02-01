package doctor

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/runpod/runpod/api"
	"github.com/runpod/runpod/cmd/ssh"
	internalapi "github.com/runpod/runpod/internal/api"
	"github.com/runpod/runpod/internal/output"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	sshcrypto "golang.org/x/crypto/ssh"
)

// Cmd is the doctor command
var Cmd = &cobra.Command{
	Use:   "doctor",
	Short: "diagnose and fix cli issues",
	Long:  "check runpod connectivity and fix configuration issues",
	RunE:  runDoctor,
}

type checkResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Details string `json:"details,omitempty"`
	Error   string `json:"error,omitempty"`
	Fixed   bool   `json:"fixed,omitempty"`
}

type doctorReport struct {
	Checks  []checkResult `json:"checks"`
	Healthy bool          `json:"healthy"`
}

func runDoctor(cmd *cobra.Command, args []string) error {
	report := &doctorReport{
		Checks:  []checkResult{},
		Healthy: true,
	}

	// check 1: api key configured
	apiKeyCheck := checkAPIKey()
	report.Checks = append(report.Checks, apiKeyCheck)
	if apiKeyCheck.Status == "fail" && !apiKeyCheck.Fixed {
		report.Healthy = false
	}

	// check 2: api connectivity (only if we have an api key)
	if apiKeyCheck.Status == "pass" || apiKeyCheck.Fixed {
		connectCheck := checkAPIConnectivity()
		report.Checks = append(report.Checks, connectCheck)
		if connectCheck.Status == "fail" {
			report.Healthy = false
		}

		// check 3: ssh key setup (only if api works)
		if connectCheck.Status == "pass" {
			sshCheck := checkSSHKey()
			report.Checks = append(report.Checks, sshCheck)
			if sshCheck.Status == "fail" && !sshCheck.Fixed {
				report.Healthy = false
			}
		}
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(report, &output.Config{Format: format})
}

func checkAPIKey() checkResult {
	result := checkResult{Name: "api_key"}

	apiKey := os.Getenv("RUNPOD_API_KEY")
	if apiKey == "" {
		apiKey = viper.GetString("apiKey")
	}

	if apiKey != "" {
		result.Status = "pass"
		return result
	}

	result.Status = "fail"
	result.Error = "no api key configured"

	// try to fix: prompt for api key
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "no api key found.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "to get your api key:")
	fmt.Fprintln(os.Stderr, "  1. go to https://www.runpod.io/console/user/settings")
	fmt.Fprintln(os.Stderr, "  2. click 'api keys' and create a new key")
	fmt.Fprintln(os.Stderr, "  3. copy the key and paste it below")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprint(os.Stderr, "enter your runpod api key: ")
	
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		result.Error = "failed to read input"
		return result
	}

	apiKey = strings.TrimSpace(input)
	if apiKey == "" {
		result.Error = "no api key provided"
		return result
	}

	// save to config
	viper.Set("apiKey", apiKey)
	home, _ := os.UserHomeDir()
	configPath := home + "/.runpod"
	os.MkdirAll(configPath, 0700)
	
	if err := viper.WriteConfig(); err != nil {
		if err := viper.WriteConfigAs(configPath + "/config.toml"); err != nil {
			result.Error = fmt.Sprintf("failed to save config: %v", err)
			return result
		}
	}

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "api key saved to %s/config.toml\n", configPath)
	fmt.Fprintln(os.Stderr, "")

	result.Fixed = true
	result.Status = "pass"
	return result
}

func checkAPIConnectivity() checkResult {
	result := checkResult{Name: "api_connectivity"}

	client, err := internalapi.NewClient()
	if err != nil {
		result.Status = "fail"
		result.Error = fmt.Sprintf("failed to create client: %v", err)
		return result
	}

	// try to get user info to verify connectivity
	user, err := client.GetUser()
	if err != nil {
		result.Status = "fail"
		result.Error = fmt.Sprintf("api request failed: %v", err)
		return result
	}

	if user == nil || user.ID == "" {
		result.Status = "fail"
		result.Error = "invalid response from api"
		return result
	}

	result.Status = "pass"
	result.Details = fmt.Sprintf("user: %s", user.Email)
	return result
}

func checkSSHKey() checkResult {
	result := checkResult{Name: "ssh_key"}

	// check for local ssh key
	publicKey, err := ssh.GetLocalSSHKey()
	if err != nil {
		result.Status = "fail"
		result.Error = fmt.Sprintf("failed to check local ssh key: %v", err)
		return result
	}

	localKeyExists := publicKey != nil

	// generate if not found
	if publicKey == nil {
		fmt.Fprintln(os.Stderr, "generating ssh key...")
		publicKey, err = ssh.GenerateSSHKeyPair("RunPod-Key-Go")
		if err != nil {
			result.Status = "fail"
			result.Error = fmt.Sprintf("failed to generate ssh key: %v", err)
			return result
		}
		result.Fixed = true
	}

	// check if key exists in cloud
	_, cloudKeys, err := api.GetPublicSSHKeys()
	if err != nil {
		result.Status = "fail"
		result.Error = fmt.Sprintf("failed to get cloud ssh keys: %v", err)
		return result
	}

	// parse local key
	localPubKey, _, _, _, err := sshcrypto.ParseAuthorizedKey(publicKey)
	if err != nil {
		result.Status = "fail"
		result.Error = fmt.Sprintf("failed to parse local key: %v", err)
		return result
	}
	localFingerprint := sshcrypto.FingerprintSHA256(localPubKey)

	// check if exists in cloud
	keyInCloud := false
	for _, cloudKey := range cloudKeys {
		if cloudKey.Fingerprint == localFingerprint {
			keyInCloud = true
			break
		}
	}

	// add if not in cloud
	if !keyInCloud {
		fmt.Fprintln(os.Stderr, "adding ssh key to runpod...")
		if err := api.AddPublicSSHKey(publicKey); err != nil {
			result.Status = "fail"
			result.Error = fmt.Sprintf("failed to add ssh key: %v", err)
			return result
		}
		result.Fixed = true
	}

	result.Status = "pass"
	result.Details = fmt.Sprintf("local_key: %t, synced_to_cloud: %t", localKeyExists, keyInCloud || result.Fixed)
	return result
}
