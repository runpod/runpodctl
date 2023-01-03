package croc

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/schollz/croc/v9/src/croc"
	"github.com/schollz/croc/v9/src/models"
	"github.com/spf13/cobra"
)

func GetRelays() ([]Relay, error) {
	// Make a GET request to the URL
	res, err := http.Get("https://gist.githubusercontent.com/zhl146/bd1d6fac2d64a93db63f04b20b053667/raw/11f6348581a2ee05b49ad0e842f9957a01f2f9da/relays.json")
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

		var success = false

		relays, err := GetRelays()
		if err != nil {
			fmt.Println("There was an issue getting the relay list. Please try again.")
			return
		}

		fmt.Println(relays)

		for _, relay := range relays {
			fmt.Println(relay)
			crocOptions := croc.Options{
				Curve:         "p256",
				Debug:         false,
				IsSender:      false,
				NoPrompt:      true,
				Overwrite:     true,
				RelayAddress:  relay.Address,
				RelayPassword: relay.Password,
				SharedSecret:  args[0],
			}
	
			if crocOptions.RelayAddress != models.DEFAULT_RELAY {
				crocOptions.RelayAddress6 = ""
			} else if crocOptions.RelayAddress6 != models.DEFAULT_RELAY6 {
				crocOptions.RelayAddress = ""
			}
	
			cr, err := croc.New(crocOptions)
			if err != nil {
				fmt.Println(err)
				continue
			}
	
			if err = cr.Receive(); err != nil {
				continue
			} else {
				success = true
				break
			}
	
		}

		if !success {
			fmt.Println("There was an issue receiving the file. Please try to run the send command again.")
		}
		return

	},
}


