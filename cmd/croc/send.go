package croc

import (
	"fmt"
	"strings"
	"encoding/json"
	"math/rand"
	"net/http"
	"time"
	"strconv"

	"github.com/schollz/croc/v9/src/models"
	"github.com/schollz/croc/v9/src/utils"
	"github.com/spf13/cobra"
)

type Relay struct {
	Address    string   `json:"address"`
	Password	string `json:"password"`
	Ports  string    `json:"ports"`
}

type Response struct {
	Relays []Relay `json:"relays"`
}

var code string

var SendCmd = &cobra.Command{
	Use:   "send [filename(s) or folder]",
	Args:  cobra.ExactArgs(1),
	Short: "send file(s), or folder",
	Long:  "send file(s), or folder to pod or any computer",
	Run: func(cmd *cobra.Command, args []string) {

		rand.Seed(time.Now().UnixNano())

		// Make a GET request to the URL
		res, err := http.Get("https://raw.githubusercontent.com/runpod/runpodctl/main/relay_list.json")
		if err != nil {
			fmt.Println(err)
			return
		}
		defer res.Body.Close()
	
		// Decode the JSON response
		var response Response
		err = json.NewDecoder(res.Body).Decode(&response)
		if err != nil {
			fmt.Println(err)
			fmt.Println("Could not get list of relays. Please contact support for help!")
			return
		}
	
		// Choose a random relay from the array
		randomIndex := rand.Intn(len(response.Relays))
		relay := response.Relays[randomIndex]

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

		fnames := args
		if len(fnames) == 0 {
			fmt.Println("must specify file: croc send [filename(s) or folder]")
			return
		}

		if len(crocOptions.SharedSecret) == 0 {
			// generate code phrase
			crocOptions.SharedSecret = utils.GetRandomName()
		}

		crocOptions.SharedSecret = crocOptions.SharedSecret + "-" + strconv.Itoa(randomIndex)

		minimalFileInfos, emptyFoldersToTransfer, totalNumberFolders, err := GetFilesInfo(fnames, crocOptions.ZipFolder)
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
