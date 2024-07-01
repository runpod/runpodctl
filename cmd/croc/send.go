package croc

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/schollz/croc/v9/src/models"
	"github.com/schollz/croc/v9/src/utils"
	"github.com/spf13/cobra"
)

type Relay struct {
	Address  string `json:"address"`
	Password string `json:"password"`
	Ports    string `json:"ports"`
}

type Response struct {
	Relays []Relay `json:"relays"`
}

var code string

var relayUrl = "https://raw.githubusercontent.com/runpod/runpodctl/main/cmd/croc/relays.json"

var SendCmd = &cobra.Command{
	Use:   "send [file0] [file1] ...",
	Args:  cobra.MinimumNArgs(1),
	Short: "send file(s), or folder",
	Long:  "send file(s), or folder to pod or any computer",
	Run: func(_ *cobra.Command, args []string) {
		log := log.New(os.Stderr, "runpodctl-send: ", 0)
		src, err := filepath.Abs(args[0])
		if err != nil {
			log.Fatalf("error getting absolute path of %s: %v", args[0], err)
		}
		switch _, err := os.Stat(src); {
		case errors.Is(err, os.ErrNotExist):
			log.Fatalf("file or folder %q does not exist", src)
		case err != nil:
			log.Fatalf("error reading file or folder %q: %v", src, err)
		}
		// Make a GET request to the URL
		relays, err := getRelays()
		if err != nil {
			log.Print(err)
			log.Fatal("Could not get list of relays. Please contact support for help!")
		}

		randIndex := rand.IntN(len(relays))
		// Choose a random relay from the array
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
			SharedSecret:  code,
			ZipFolder:     true,
		}
		if crocOptions.RelayAddress != models.DEFAULT_RELAY {
			crocOptions.RelayAddress6 = ""
		} else if crocOptions.RelayAddress6 != models.DEFAULT_RELAY6 {
			crocOptions.RelayAddress = ""
		}

		if len(crocOptions.SharedSecret) == 0 {
			// generate code phrase
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
	},
}

func init() {
	SendCmd.Flags().StringVar(&code, "code", "", "codephrase used to connect")
}
