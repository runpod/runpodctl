package croc

import (
	"fmt"

	"github.com/schollz/croc/v9/src/croc"
	"github.com/schollz/croc/v9/src/models"
	"github.com/spf13/cobra"
)

var ReceiveCmd = &cobra.Command{
	Use:   "receive [code]",
	Args:  cobra.ExactArgs(1),
	Short: "receive file(s), or folder",
	Long:  "receive file(s), or folder from pod or any computer",
	Run: func(cmd *cobra.Command, args []string) {
		crocOptions := croc.Options{
			Curve:         "p256",
			Debug:         false,
			IsSender:      false,
			NoPrompt:      true,
			Overwrite:     true,
			RelayAddress:  "relay1.runpod.io",
			RelayPassword: "Op7X0378LX7ZB602&qIX#@qHU",
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
			return
		}

		if err = cr.Receive(); err != nil {
			fmt.Println(err)
			return
		}
	},
}
