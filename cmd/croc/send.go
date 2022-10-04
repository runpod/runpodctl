package croc

import (
	"fmt"
	"strings"

	"github.com/schollz/croc/v9/src/models"
	"github.com/schollz/croc/v9/src/utils"
	"github.com/spf13/cobra"
)

var code string

var SendCmd = &cobra.Command{
	Use:   "send [filename(s) or folder]",
	Args:  cobra.ExactArgs(1),
	Short: "send file(s), or folder",
	Long:  "send file(s), or folder to pod or any computer",
	Run: func(cmd *cobra.Command, args []string) {
		portsString := ""
		if portsString == "" {
			portsString = "9009,9010,9011,9012,9013"
		}
		crocOptions := Options{
			Curve:         "p256",
			Debug:         false,
			DisableLocal:  true,
			HashAlgorithm: "xxhash",
			IsSender:      true,
			NoPrompt:      true,
			Overwrite:     true,
			RelayAddress:  "relay1.runpod.io",
			RelayPassword: "Op7X0378LX7ZB602&qIX#@qHU",
			RelayPorts:    strings.Split(portsString, ","),
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
