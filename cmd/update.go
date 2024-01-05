package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"
)

type Asset struct {
	Url  string `json:"browser_download_url"`
	Name string
}
type GithubApiResponse struct {
	Version string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

func DownloadFile(url string, savePath string) (file *os.File, err error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status: %s", resp.Status)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	file, err = os.Create(savePath)
	if err != nil {
		return nil, err
	}

	_, err = file.Write(content)
	if err != nil {
		return nil, err
	}

	return file, nil
}
func GetJson(url string) (*GithubApiResponse, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	// fmt.Println(string(body))
	var result GithubApiResponse
	err = json.Unmarshal(body, &result)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "runpodctl update",
	Long:  "runpodctl update",
	Run: func(c *cobra.Command, args []string) {
		//fetch newest github release
		githubApiUrl := "https://api.github.com/repos/runpod/runpodctl/releases/latest"
		apiResp, err := GetJson(githubApiUrl)
		if err != nil {
			fmt.Println("error fetching latest version info for runpodctl", err)
			return
		}
		//find download link for current platform
		latestVersion := apiResp.Version
		if semver.Compare("v"+version, latestVersion) == -1 {
			//version < latest
			// newBinaryName := fmt.Sprintf("runpodctl-%s-%s", runtime.GOOS, runtime.GOARCH)
			newBinaryName := "runpodctl-darwin-arm"
			foundNewBinary := false
			var downloadLink string
			for _, asset := range apiResp.Assets {
				if asset.Name == newBinaryName {
					foundNewBinary = true
					downloadLink = asset.Url
				}
			}
			if !foundNewBinary {
				fmt.Printf("platform %s-%s not supported in latest version\n", runtime.GOOS, runtime.GOARCH)
				return
			}
			ex, err := os.Executable()
			if err != nil {
				panic(err)
			}
			exPath := filepath.Dir(ex)
			downloadPath := newBinaryName
			destFilename := "runpodctl"
			if runtime.GOOS == "windows" {
				destFilename = "runpodctl.exe"
			}
			destPath := filepath.Join(exPath, destFilename)
			fmt.Printf("downloading runpodctl %s to %s\n", latestVersion, downloadPath)
			file, err := DownloadFile(downloadLink, downloadPath)
			defer file.Close()
			if err != nil {
				fmt.Println("error fetching the latest version of runpodctl", err)
				return
			}
			//chmod +x
			err = file.Chmod(0755)
			if err != nil {
				fmt.Println("error setting permissions on new binary", err)
			}
			if runtime.GOOS != "windows" {
				fmt.Printf("moving %s to %s\n", downloadPath, destPath)
				exec.Command("mv", downloadPath, destPath).Run() //need to run externally to current process because we're updating the running executable
			}
		}

	},
}
