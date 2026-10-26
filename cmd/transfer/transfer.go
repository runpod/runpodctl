package transfer

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/schollz/croc/v9/src/models"
	"github.com/schollz/croc/v9/src/utils"
	"github.com/spf13/cobra"
)

// Relay represents a croc relay server
type Relay struct {
	Address  string `json:"address"`
	Password string `json:"password"`
	Ports    string `json:"ports"`
}

// RelayResponse is the response from the relay list endpoint
type RelayResponse struct {
	Relays []Relay `json:"relays"`
}

// relayURL is fetched at runtime by every released binary, so cmd/croc/ keeps
// existing purely to hold relays.json at this path. The Go files that used to
// sit beside it were an unreferenced copy of the client in cmd/transfer,
// carrying the same unvalidated receive path; they were deleted rather than
// fixed in parallel. Moving the json needs a deprecation window, not a commit.
var relayURL = "https://raw.githubusercontent.com/runpod/runpodctl/main/cmd/croc/relays.json"

var sendCode string

// minSendCodeLen is the minimum length of a user-supplied --code. croc only
// checks its 6-character minimum after the "-<relay index>" suffix is added, so
// a 4-character code would pass there; requiring 6 before the suffix keeps the
// user's own phrase from being trivially short.
const minSendCodeLen = 6

func validateSendCode(code string) error {
	if code != "" && len(code) < minSendCodeLen {
		return fmt.Errorf("code %q is too short: --code must be at least %d characters", code, minSendCodeLen)
	}
	return nil
}

// SendCmd is the send command
var SendCmd = &cobra.Command{
	Use:   "send <file>",
	Args:  cobra.MinimumNArgs(1),
	Short: "send files or folders",
	Long: `send files or folders to a pod or any computer using croc.

send prints a one-time code ending in a relay index; run 'runpodctl receive <code>'
on the other machine (e.g. inside a pod) with that exact code to complete the
transfer.

with --code, the code is not one-time: it is your codephrase plus the relay index
that send prints (e.g. my-secret-14), and the receiver must type the printed value.`,
	Example: `  # on this machine: prints a one-time code like 5254-respect-amen-emotion-14
  runpodctl send ./model.safetensors

  # then, on the pod (or any other computer), use the exact code send printed:
  runpodctl receive 5254-respect-amen-emotion-14

  # with a fixed codephrase (minimum 6 characters), send prints my-secret-<relay index>:
  runpodctl send --code my-secret ./model.safetensors`,
	RunE: runSend,
}

// ReceiveCmd is the receive command
var ReceiveCmd = &cobra.Command{
	Use:   "receive <code>",
	Args:  cobra.ExactArgs(1),
	Short: "receive files or folders",
	Long: `receive files or folders from a pod or any computer using croc.

use the exact code printed by 'runpodctl send' on the sending machine, including
the trailing relay index. if the sender used --code, type the printed
<codephrase>-<relay index>, not the bare codephrase.`,
	Example: `  # receive using the exact code printed by 'runpodctl send'
  runpodctl receive 5254-respect-amen-emotion-14

  # sender used --code my-secret and send printed my-secret-14
  runpodctl receive my-secret-14`,
	RunE: runReceive,
}

func init() {
	SendCmd.Flags().StringVar(&sendCode, "code", "", "codephrase used to connect")
}

func getRelays() ([]Relay, error) {
	client := &http.Client{Timeout: 2 * time.Minute}
	res, err := client.Get(relayURL)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var response RelayResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		return nil, err
	}

	return response.Relays, nil
}

func runSend(cmd *cobra.Command, args []string) error {
	if err := validateSendCode(sendCode); err != nil {
		return err
	}

	src, err := filepath.Abs(args[0])
	if err != nil {
		return fmt.Errorf("failed to resolve path %s: %w", args[0], err)
	}

	switch _, err := os.Stat(src); {
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("file or folder %q does not exist", src)
	case err != nil:
		return fmt.Errorf("failed to read %q: %w", src, err)
	}

	relays, err := getRelays()
	if err != nil {
		return fmt.Errorf("could not get list of relays: %w", err)
	}

	// Test all relays' RTT in parallel, performs 2 pings and selects from top 3 fastest
	_, best := TestAllRelaysRTT(relays, 2, 3)
	randIndex := best.Index
	relay := relays[randIndex]

	crocOptions := Options{
		Curve:         "p256",
		Debug:         false,
		DisableLocal:  true,
		HashAlgorithm: "xxhash",
		IsSender:      true,
		NoPrompt:      true,
		Overwrite:     true,
		RelayAddress:  relay.Address,
		RelayPassword: relay.Password,
		RelayPorts:    strings.Split(relay.Ports, ","),
		SharedSecret:  sendCode,
		ZipFolder:     true,
	}

	if crocOptions.RelayAddress != models.DEFAULT_RELAY {
		crocOptions.RelayAddress6 = ""
	} else if crocOptions.RelayAddress6 != models.DEFAULT_RELAY6 {
		crocOptions.RelayAddress = ""
	}

	if len(crocOptions.SharedSecret) == 0 {
		crocOptions.SharedSecret = utils.GetRandomName()
	}

	crocOptions.SharedSecret = crocOptions.SharedSecret + "-" + strconv.Itoa(randIndex)
	fmt.Println(crocOptions.SharedSecret) // output to stdout so user or send-ssh can see it

	minimalFileInfos, emptyFoldersToTransfer, totalNumberFolders, err := GetFilesInfo(args, crocOptions.ZipFolder)
	if err != nil {
		// was a bare return: a failure here produced no output at all and exit 0,
		// so `runpodctl send x && echo ok` printed ok on a silent failure.
		return fmt.Errorf("failed to read %s: %w", src, err)
	}

	cr, err := New(crocOptions)
	if err != nil {
		// was fmt.Println to stdout + exit 0.
		return fmt.Errorf("croc: %w", err)
	}

	if err = cr.Send(minimalFileInfos, emptyFoldersToTransfer, totalNumberFolders); err != nil {
		// was fmt.Println to stdout + exit 0.
		return fmt.Errorf("croc: send: %w", err)
	}

	return nil
}

func runReceive(cmd *cobra.Command, args []string) error {
	relays, err := getRelays()
	if err != nil {
		return fmt.Errorf("could not get list of relays: %w", err)
	}

	sharedSecretCode := args[0]
	split := strings.Split(sharedSecretCode, "-")
	if len(split) < 2 {
		return fmt.Errorf("malformed code %q: expected at least 2 parts separated by dashes, got %v; re-run 'runpodctl send' for a valid code", sharedSecretCode, len(split))
	}

	relayIndex, err := strconv.Atoi(split[len(split)-1])
	if err != nil {
		return fmt.Errorf("malformed relay in code %q; re-run 'runpodctl send' for a valid code", sharedSecretCode)
	}

	if relayIndex < 0 || relayIndex >= len(relays) {
		return fmt.Errorf("relay index %d not found; re-run 'runpodctl send' for a valid code", relayIndex)
	}
	relay := relays[relayIndex]

	crocOptions := Options{
		Curve:         "p256",
		Debug:         false,
		DisableLocal:  true,
		HashAlgorithm: "xxhash",
		IsSender:      false,
		NoPrompt:      true,
		Overwrite:     true,
		RelayAddress:  relay.Address,
		RelayPassword: relay.Password,
		RelayPorts:    strings.Split(relay.Ports, ","),
		SharedSecret:  sharedSecretCode,
	}

	if crocOptions.RelayAddress != models.DEFAULT_RELAY {
		crocOptions.RelayAddress6 = ""
	} else if crocOptions.RelayAddress6 != models.DEFAULT_RELAY6 {
		crocOptions.RelayAddress = ""
	}

	cr, err := New(crocOptions)
	if err != nil {
		return fmt.Errorf("croc: %w", err)
	}

	if err = cr.Receive(); err != nil {
		return fmt.Errorf("croc: receive: %w", err)
	}

	return nil
}
