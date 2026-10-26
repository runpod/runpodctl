package doctor

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/runpod/runpodctl/cmd/ssh"
	internalapi "github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/configenv"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	sshcrypto "golang.org/x/crypto/ssh"
)

const (
	statusPass = "pass"
	statusFail = "fail"

	keyFlagName = "api-key"

	// the saved config holds the api key, so it stays private to the user. root
	// initConfig applies the same modes; doctor reasserts them on its own write
	// rather than relying on that having run.
	configDirPerm  = 0o700
	configFilePerm = 0o600
)

// setupPrompt is shown before the stdin read, on stderr so stdout stays the
// report.
const setupPrompt = `
no api key found.

to get your api key:
  1. go to https://www.runpod.io/console/user/settings
  2. click 'api keys' and create a new key
  3. copy the key and paste it below

enter your runpod api key: `

var apiKeyFlag string

// Cmd is the doctor command
var Cmd = &cobra.Command{
	Use:   "doctor",
	Short: "diagnose and fix cli issues",
	Long: `check runpod connectivity and fix configuration issues

when no api key is configured, doctor prompts on stderr and reads one from
stdin. --api-key always validates the given key and saves it, replacing
any saved key. a key is only saved after the api accepts it. doctor exits
non-zero whenever it ends unhealthy; the report is still printed on stdout.`,
	Example: `  # interactive: prompts for the api key and saves it
  runpodctl doctor

  # non-interactive (ci, containers, agents): pipe the key on stdin, which
  # keeps it out of shell history and ps. read only when no key is configured
  printf %s "$MY_RUNPOD_KEY" | runpodctl doctor

  # or pass it as a flag, which also replaces an already saved key
  runpodctl doctor --api-key rpa_yourkey`,
	RunE: runDoctor,
}

func init() {
	Cmd.Flags().StringVar(&apiKeyFlag, keyFlagName, "",
		"api key to validate and save, replacing any saved key (visible in shell history and ps; prefer stdin)")
}

type checkResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Details string `json:"details,omitempty"`
	Error   string `json:"error,omitempty"`
	Warning string `json:"warning,omitempty"`
	Fixed   bool   `json:"fixed,omitempty"`
}

type doctorReport struct {
	Checks  []checkResult `json:"checks"`
	Healthy bool          `json:"healthy"`
}

func runDoctor(cmd *cobra.Command, args []string) error {
	providedKey := strings.TrimSpace(apiKeyFlag)
	if cmd.Flags().Changed(keyFlagName) && providedKey == "" {
		return fmt.Errorf("--%s is empty", keyFlagName)
	}
	report := &doctorReport{
		Checks:  []checkResult{},
		Healthy: true,
	}

	// check 1: api key configured
	apiKeyCheck := checkAPIKey(providedKey, os.Stdin, os.Stderr)
	report.Checks = append(report.Checks, apiKeyCheck)
	if apiKeyCheck.Status == statusFail {
		report.Healthy = false
	}
	if apiKeyCheck.Fixed {
		fmt.Fprintf(os.Stderr, "\napi key %s\n\n", apiKeyCheck.Details)
	}
	if apiKeyCheck.Warning != "" {
		fmt.Fprintf(os.Stderr, "warning: %s\n", apiKeyCheck.Warning)
	}

	// check 2: api connectivity (only if we have an api key)
	if apiKeyCheck.Status == statusPass {
		connectCheck := checkAPIConnectivity()
		report.Checks = append(report.Checks, connectCheck)
		if connectCheck.Status == statusFail {
			report.Healthy = false
		}

		// check 3: ssh key setup (only if api works)
		if connectCheck.Status == statusPass {
			sshCheck := checkSSHKey()
			report.Checks = append(report.Checks, sshCheck)
			if sshCheck.Status == statusFail {
				report.Healthy = false
			}
		}
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	if err := output.Print(report, &output.Config{Format: format}); err != nil {
		return err
	}
	// ci, scripts and agents gate on the exit code, so any unhealthy run fails.
	if !report.Healthy {
		return unhealthyError(report)
	}
	return nil
}

// unhealthyError names the first failing check, so the stderr error is
// actionable on its own while the full report stays on stdout.
func unhealthyError(report *doctorReport) error {
	for _, check := range report.Checks {
		if check.Status == statusFail {
			return fmt.Errorf("doctor: %s check failed: %s", check.Name, check.Error)
		}
	}
	return errors.New("doctor: unhealthy, see the report on stdout")
}

// checkAPIKey passes when a key is already configured and none was provided.
// A provided key (--api-key), or one read from in when nothing is configured,
// is validated against the api and saved only if it is accepted. The prompt
// goes to promptOut; what was saved or replaced is reported in the result.
func checkAPIKey(providedKey string, in io.Reader, promptOut io.Writer) checkResult {
	result := checkResult{Name: "api_key"}

	apiKey := strings.TrimSpace(providedKey)
	if apiKey == "" {
		if configenv.APIKey() != "" {
			result.Status = statusPass
			return result
		}
		key, err := promptAPIKey(in, promptOut)
		if err != nil {
			result.Status = statusFail
			result.Error = err.Error()
			return result
		}
		apiKey = key
	}

	if err := validateAPIKey(apiKey); err != nil {
		result.Status = statusFail
		result.Error = fmt.Sprintf("api key not saved: %v", err)
		return result
	}

	saved := viper.GetString("apiKey")
	replaced := saved != "" && saved != apiKey
	path, err := saveAPIKey(apiKey)
	if err != nil {
		result.Status = statusFail
		result.Error = fmt.Sprintf("failed to save config: %v", err)
		return result
	}
	result.Details = "saved to " + path
	if replaced {
		result.Details = "saved to " + path + ", replacing the key saved there before"
	}
	if env := os.Getenv(configenv.APIKeyEnv); env != "" && env != apiKey {
		result.Warning = configenv.APIKeyEnv + " is set and takes precedence over the saved key; unset it to use the key just saved"
	}

	result.Status = statusPass
	result.Fixed = true
	return result
}

// promptAPIKey reads one key from in. There is no tty check: the instructions
// go to out either way, and a pipe works as long as it carries a key, with
// or without a trailing newline (printf %s "$KEY" | runpodctl doctor).
func promptAPIKey(in io.Reader, out io.Writer) (string, error) {
	if _, err := fmt.Fprint(out, setupPrompt); err != nil {
		return "", fmt.Errorf("failed to write the api key prompt: %w", err)
	}

	input, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("failed to read input (pipe the key on stdin or use --%s): %w", keyFlagName, err)
	}
	key := strings.TrimSpace(input)
	if key == "" {
		return "", fmt.Errorf("no api key provided (pipe the key on stdin or use --%s)", keyFlagName)
	}
	return key, nil
}

// validateAPIKey asks the api who the key belongs to, so a mistyped or revoked
// key is reported instead of saved.
func validateAPIKey(apiKey string) error {
	client, err := internalapi.NewClientWithAPIKey(apiKey)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}
	user, err := client.GetUser()
	if err != nil {
		return fmt.Errorf("the api did not accept it: %w", err)
	}
	if user == nil || user.ID == "" {
		return errors.New("the api returned no account for it")
	}
	return nil
}

// saveAPIKey writes apiKey to the config file and returns the file's path.
// It rewrites only what the file already holds plus the key: the global viper
// also carries env and flag values, which must not leak into the file. The new
// file is written beside the old one and renamed over it, so a failed write
// leaves the old config intact.
func saveAPIKey(apiKey string) (string, error) {
	path := viper.ConfigFileUsed()
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("could not locate home directory: %w", err)
		}
		path = filepath.Join(home, ".runpod", "config.toml")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, configDirPerm); err != nil {
		return "", fmt.Errorf("could not create config directory: %w", err)
	}

	file := viper.New()
	file.SetConfigFile(path)
	file.SetConfigType("toml")
	if _, err := os.Stat(path); err == nil {
		// a file that did not parse at startup must not be replaced with just
		// the key; that would drop whatever else it holds (e.g. the sdk's
		// [default] key). leave it to the user.
		if err := file.ReadInConfig(); err != nil {
			return "", fmt.Errorf("could not read %s, not overwriting it (fix or remove the file, then retry): %w", path, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("could not check %s: %w", path, err)
	}
	file.Set("apiKey", apiKey)

	tmp, err := os.CreateTemp(dir, ".config-*.toml")
	if err != nil {
		return "", fmt.Errorf("could not create a temporary config file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once renamed
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Chmod(tmpPath, configFilePerm); err != nil {
		return "", fmt.Errorf("could not restrict permissions on %s: %w", tmpPath, err)
	}
	if err := file.WriteConfigAs(tmpPath); err != nil {
		return "", err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return "", fmt.Errorf("could not replace %s: %w", path, err)
	}
	viper.Set("apiKey", apiKey)
	return path, nil
}

func checkAPIConnectivity() checkResult {
	result := checkResult{Name: "api_connectivity"}

	client, err := internalapi.NewClient()
	if err != nil {
		result.Status = statusFail
		result.Error = fmt.Sprintf("failed to create client: %v", err)
		return result
	}

	// try to get user info to verify connectivity
	user, err := client.GetUser()
	if err != nil {
		result.Status = statusFail
		result.Error = fmt.Sprintf("api request failed: %v", err)
		return result
	}

	if user == nil || user.ID == "" {
		result.Status = statusFail
		result.Error = "invalid response from api"
		return result
	}

	result.Status = statusPass
	result.Details = fmt.Sprintf("user: %s", user.Email)
	return result
}

func checkSSHKey() checkResult {
	result := checkResult{Name: "ssh_key"}

	// check for local ssh key
	publicKey, err := ssh.GetLocalSSHKey()
	if err != nil {
		result.Status = statusFail
		result.Error = fmt.Sprintf("failed to check local ssh key: %v", err)
		return result
	}

	localKeyExists := publicKey != nil

	// generate if not found
	if publicKey == nil {
		fmt.Fprintln(os.Stderr, "generating ssh key...")
		publicKey, err = ssh.GenerateSSHKeyPair(ssh.DefaultKeyName)
		if err != nil {
			result.Status = statusFail
			result.Error = fmt.Sprintf("failed to generate ssh key: %v", err)
			return result
		}
		result.Fixed = true
	}

	// check if key exists in cloud
	client, err := internalapi.NewClient()
	if err != nil {
		result.Status = statusFail
		result.Error = fmt.Sprintf("failed to get cloud ssh keys: %v", err)
		return result
	}
	cloudKeys, err := client.GetPublicSSHKeys()
	if err != nil {
		result.Status = statusFail
		result.Error = fmt.Sprintf("failed to get cloud ssh keys: %v", err)
		return result
	}

	// parse local key
	localPubKey, _, _, _, err := sshcrypto.ParseAuthorizedKey(publicKey)
	if err != nil {
		result.Status = statusFail
		result.Error = fmt.Sprintf("failed to parse local key: %v", err)
		return result
	}
	localFingerprint := sshcrypto.FingerprintSHA256(localPubKey)

	// check if exists in cloud
	keyInCloud := false
	for _, cloudKey := range cloudKeys {
		// a copy v2 rejects doesn't count: AddPublicSSHKey then reports the
		// conflict, and the error says how to clear it
		if cloudKey.Fingerprint == localFingerprint && cloudKey.AcceptedByV2 {
			keyInCloud = true
			break
		}
	}

	// add if not in cloud
	if !keyInCloud {
		fmt.Fprintln(os.Stderr, "adding ssh key to runpod...")
		if err := client.AddPublicSSHKey(publicKey, false); err != nil {
			result.Status = statusFail
			var invalid *internalapi.InvalidStoredSSHKeysError
			if errors.As(err, &invalid) {
				err = invalid.ForSync()
			}
			result.Error = fmt.Sprintf("failed to add ssh key: %v", err)
			return result
		}
		result.Fixed = true
	}

	result.Status = statusPass
	result.Details = fmt.Sprintf("local_key: %t, synced_to_cloud: %t", localKeyExists, keyInCloud || result.Fixed)
	return result
}
