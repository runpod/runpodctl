package croc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"strconv"

	"github.com/schollz/croc/v9/src/croc"
	"github.com/schollz/croc/v9/src/models"
	"github.com/spf13/cobra"
)

func GetRelays() ([]Relay, error) {
	// Make a GET request to the URL
	res, err := http.Get("https://gist.githubusercontent.com/zhl146/bd1d6fac2d64a93db63f04b20b053667/raw/3e9e836e7860abf94a4a6972bbba10dbee1ff988/relays.json")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	// Decode the JSON response
	var response Response
	err = json.NewDecoder(res.Body).Decode(&response)
	if err != nil {
		return nil, err
	}

	return response.Relays, nil
}

var ReceiveCmd = &cobra.Command{
	Use:   "receive [code]",
	Args:  cobra.ExactArgs(1),
	Short: "receive file(s), or folder",
	Long:  "receive file(s), or folder from pod or any computer",
	Run: func(cmd *cobra.Command, args []string) {


		relays, err := GetRelays()
		if err != nil {
			fmt.Println("There was an issue getting the relay list. Please try again.")
			return
		}

		fmt.Println(relays) 

		secretPlusIndex := args[0]

		split := strings.Split(secretPlusIndex, "::")

		fmt.Println(split)

		

		SharedSecret := split[0]
		relayIndexString := split[1]

		relayIndex, err := strconv.Atoi(relayIndexString)

		if err != nil {
			fmt.Println("Malformed relay, please try again.")
			return
		}

		fmt.Println("SECRET")
		fmt.Println(SharedSecret)
		fmt.Println("INDEX")
		fmt.Println(relayIndex)


		relay := relays[relayIndex]

		fmt.Println(relay)
		crocOptions := croc.Options{
			Curve:         "p256",
			Debug:         false,
			IsSender:      false,
			NoPrompt:      true,
			Overwrite:     true,
			RelayAddress:  relay.Address,
			RelayPassword: relay.Password,
			SharedSecret:  SharedSecret,
		}

		if crocOptions.RelayAddress != models.DEFAULT_RELAY {
			crocOptions.RelayAddress6 = ""
		} else if crocOptions.RelayAddress6 != models.DEFAULT_RELAY6 {
			crocOptions.RelayAddress = ""
		}

		fmt.Println(crocOptions)

		cr, err := croc.New(crocOptions)

		if err != nil {
			fmt.Println(err)
		}

		if err = cr.Receive(); err != nil {
			fmt.Println(err)
		} 

	},
}


