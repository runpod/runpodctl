package transfer

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
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

var relayURL = "https://raw.githubusercontent.com/runpod/runpodctl/main/cmd/croc/relays.json"

var sendCode string

// SendCmd is the send command
var SendCmd = &cobra.Command{
	Use:   "send <file>",
	Args:  cobra.MinimumNArgs(1),
	Short: "send files or folders",
	Long:  "send files or folders to a pod or any computer using croc",
	Run:   runSend,
}

// ReceiveCmd is the receive command
var ReceiveCmd = &cobra.Command{
	Use:   "receive <code>",
	Args:  cobra.ExactArgs(1),
	Short: "receive files or folders",
	Long:  "receive files or folders from a pod or any computer using croc",
	Run:   runReceive,
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

func runSend(cmd *cobra.Command, args []string) {
	logger := log.New(os.Stderr, "runpod-send: ", 0)

	src, err := filepath.Abs(args[0])
	if err != nil {
		logger.Fatalf("error getting absolute path of %s: %v", args[0], err)
	}

	switch _, err := os.Stat(src); {
	case errors.Is(err, os.ErrNotExist):
		logger.Fatalf("file or folder %q does not exist", src)
	case err != nil:
		logger.Fatalf("error reading file or folder %q: %v", src, err)
	}

	relays, err := getRelays()
	if err != nil {
		logger.Print(err)
		logger.Fatal("could not get list of relays. please contact support for help!")
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
		return
	}

	cr, err := New(crocOptions)
	if err != nil {
		fmt.Println(err)
		return
	}

	if err = cr.Send(minimalFileInfos, emptyFoldersToTransfer, totalNumberFolders); err != nil {
		fmt.Println(err)
	}
}

func runReceive(cmd *cobra.Command, args []string) {
	logger := log.New(os.Stderr, "runpod-receive: ", 0)

	relays, err := getRelays()
	if err != nil {
		logger.Fatal("there was an issue getting the relay list. please try again.")
	}

	sharedSecretCode := args[0]
	split := strings.Split(sharedSecretCode, "-")
	if len(split) < 2 {
		logger.Fatalf("malformed code %q: expected at least 2 parts separated by dashes, but got %v. please retry 'runpodctl send' to generate a valid code.", sharedSecretCode, len(split))
	}

	relayIndex, err := strconv.Atoi(split[len(split)-1])
	if err != nil {
		logger.Fatalf("malformed relay, please retry 'runpodctl send' to generate a valid code.")
	}

	if relayIndex < 0 || relayIndex >= len(relays) {
		logger.Fatalf("relay index %d not found; please retry 'runpodctl send' to generate a valid code.", relayIndex)
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
		logger.Fatalf("croc: %v", err)
	}

	if err = cr.Receive(); err != nil {
		logger.Fatalf("croc: receive: %v", err)
	}
}
