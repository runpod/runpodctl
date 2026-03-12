package cmd

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

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
	var result GithubApiResponse
	err = json.Unmarshal(body, &result)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// assetName returns the expected release asset name for the current platform.
// darwin uses a universal binary ("all"), others use the specific arch.
// release assets are archives: .tar.gz for unix, .zip for windows.
func assetName() string {
	arch := runtime.GOARCH
	if runtime.GOOS == "darwin" {
		arch = "all"
	}
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("runpodctl-%s-%s%s", runtime.GOOS, arch, ext)
}

// extractBinaryFromTarGz extracts the "runpodctl" binary from a .tar.gz archive.
func extractBinaryFromTarGz(archivePath, destPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if header.Typeflag == tar.TypeReg && filepath.Base(header.Name) == "runpodctl" {
			out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
			if err != nil {
				return err
			}
			defer out.Close()
			if _, err := io.Copy(out, tr); err != nil {
				return err
			}
			return nil
		}
	}
	return fmt.Errorf("runpodctl binary not found in archive")
}

// extractBinaryFromZip extracts the "runpodctl.exe" binary from a .zip archive.
func extractBinaryFromZip(archivePath, destPath string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		if strings.EqualFold(filepath.Base(f.Name), "runpodctl.exe") {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			defer rc.Close()
			out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
			if err != nil {
				return err
			}
			defer out.Close()
			if _, err := io.Copy(out, rc); err != nil {
				return err
			}
			return nil
		}
	}
	return fmt.Errorf("runpodctl.exe not found in archive")
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "update runpodctl cli",
	Long:  "update runpodctl cli to the latest version",
	Run: func(c *cobra.Command, args []string) {
		// fetch newest github release
		githubApiUrl := "https://api.github.com/repos/runpod/runpodctl/releases/latest"
		apiResp, err := GetJson(githubApiUrl)
		if err != nil {
			fmt.Println("error fetching latest version info for runpodctl", err)
			return
		}
		latestVersion := apiResp.Version
		if semver.Compare("v"+version, latestVersion) >= 0 {
			fmt.Printf("runpodctl %s is already up to date\n", version)
			return
		}

		// find download link for current platform
		expectedAsset := assetName()
		var downloadLink string
		for _, asset := range apiResp.Assets {
			if asset.Name == expectedAsset {
				downloadLink = asset.Url
				break
			}
		}
		if downloadLink == "" {
			fmt.Printf("platform %s-%s not supported in latest version\n", runtime.GOOS, runtime.GOARCH)
			return
		}

		ex, err := os.Executable()
		if err != nil {
			fmt.Println("error finding current executable:", err)
			return
		}
		exPath := filepath.Dir(ex)

		destFilename := "runpodctl"
		if runtime.GOOS == "windows" {
			destFilename = "runpodctl.exe"
		}
		destPath := filepath.Join(exPath, destFilename)

		// download archive to a temp file
		tmpFile, err := os.CreateTemp("", "runpodctl-update-*")
		if err != nil {
			fmt.Println("error creating temp file:", err)
			return
		}
		archivePath := tmpFile.Name()
		tmpFile.Close()
		defer os.Remove(archivePath)

		fmt.Printf("downloading runpodctl %s\n", latestVersion)
		file, err := DownloadFile(downloadLink, archivePath)
		if err != nil {
			fmt.Println("error fetching the latest version of runpodctl:", err)
			return
		}
		file.Close()

		// extract binary from archive to a temp location next to the destination
		extractedPath := destPath + ".new"
		defer os.Remove(extractedPath)

		if runtime.GOOS == "windows" {
			if err := extractBinaryFromZip(archivePath, extractedPath); err != nil {
				fmt.Println("error extracting update:", err)
				return
			}
			fmt.Println("to complete the update, run this command:")
			fmt.Printf("move /Y \"%s\" \"%s\"\n", extractedPath, destPath)
		} else {
			if err := extractBinaryFromTarGz(archivePath, extractedPath); err != nil {
				fmt.Println("error extracting update:", err)
				return
			}
			fmt.Printf("installing runpodctl %s to %s\n", latestVersion, destPath)
			// need to run externally to current process because we're updating the running executable
			exec.Command("mv", extractedPath, destPath).Run()
		}
	},
}
