package croc

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/schollz/croc/v9/src/croc"
	"github.com/schollz/croc/v9/src/models"
	"github.com/spf13/cobra"
)

func getRelays() ([]Relay, error) {
	// Make a GET request to the URL
	client := &http.Client{
		Timeout: 2 * time.Minute,
	}
	res, err := client.Get(relayUrl)
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
		log := log.New(os.Stderr, "runpodctl-receive: ", 0)
		relays, err := getRelays()
		if err != nil {
			log.Fatal("There was an issue getting the relay list. Please try again.")
		}
		sharedSecretCode := args[0]
		split := strings.Split(sharedSecretCode, "-")
		if len(split) < 2 {
			log.Fatalf("Malformed code %q: expected at least 2 parts separated by dashes, but got %v. Please retry 'runpodctl send' to generate a valid code.", sharedSecretCode, len(split))
		}

		relayIndex, err := strconv.Atoi(split[len(split)-1]) // relay index is the final split value
		if err != nil {
			log.Fatalf("Malformed relay, please retry 'runpodctl send' to generate a valid code.")
		}

		if relayIndex < 0 || relayIndex >= len(relays) {
			log.Fatalf("Relay index %d not found; please retry 'runpodctl send' to generate a valid code.", relayIndex)
			return
		}
		relay := relays[relayIndex]

		crocOptions := croc.Options{
			Curve:         "p256",
			Debug:         false,
			IsSender:      false,
			NoPrompt:      true,	
			Overwrite:     true,
			RelayAddress:  relay.Address,
			RelayPassword: relay.Password,
			SharedSecret:  sharedSecretCode,
		}

		if crocOptions.RelayAddress != models.DEFAULT_RELAY {
			crocOptions.RelayAddress6 = ""
		} else if crocOptions.RelayAddress6 != models.DEFAULT_RELAY6 {
			crocOptions.RelayAddress = ""
		}

		cr, err := croc.New(crocOptions)
		if err != nil {
			log.Fatalf("croc: %v", err)
		}

		if err = cr.Receive(); err != nil {
			log.Fatalf("croc: receive: %v", err)
		}
	},
}
