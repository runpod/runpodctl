package model

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/runpod/runpodctl/api"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	addModelName                string
	addModelCredentialReference string
	addModelCredentialType      string
	addModelStatus              string
	addModelVersionStatus       string
	addModelCreateUpload        bool
	addModelFileName            string
	addModelFileSize            string
	addModelPartSize            string
	addModelContentType         string
	addModelDirectoryPath       string
	addModelMetadata            map[string]string
)

const (
	graphqlTimeoutFlagName   = "graphql-timeout"
	modelGraphQLTimeoutValue = time.Minute
)

// The GraphQL requests for uploading a model can take longer than the default
// 10s, as the server needs to create pre-signed S3 URLs from R2, which can be
// slow. So we bump it up to 1m so that we do not fail erroneously.
func setModelGraphQLTimeout(cmd *cobra.Command) {
	timeoutFlag := cmd.InheritedFlags().Lookup(graphqlTimeoutFlagName)
	if timeoutFlag == nil || timeoutFlag.Changed {
		return
	}

	if err := timeoutFlag.Value.Set(modelGraphQLTimeoutValue.String()); err != nil {
		cobra.CheckErr(fmt.Errorf("unable to set graphql timeout: %w", err))
	}

	viper.Set(api.GraphQLTimeoutKey, modelGraphQLTimeoutValue)
	fmt.Printf("--graphql-timeout not set; defaulting to %s for model creation operations\n", modelGraphQLTimeoutValue)
}

type completedPart struct {
	PartNumber int    `xml:"PartNumber"`
	ETag       string `xml:"ETag"`
}

type completeMultipartUpload struct {
	XMLName xml.Name        `xml:"CompleteMultipartUpload"`
	XMLNS   string          `xml:"xmlns,attr"`
	Parts   []completedPart `xml:"Part"`
}

type modelFile struct {
	AbsolutePath string
	RelativePath string
	Size         int64
}

// TODO: replace the manual completion call with github.com/aws/aws-sdk-go-v2/service/s3's
// CompleteMultipartUpload to rely on the SDK for payload formatting and signing logic.

// AddModelToRepoCmd uploads a model to the RunPod model repository.
// Hidden while the model repository feature is in development and not ready for general use.
var AddModelToRepoCmd = &cobra.Command{
	Use:    "model",
	Args:   cobra.ExactArgs(0),
	Short:  "upload a model",
	Long:   "upload a model to the RunPod model repository",
	Hidden: true,
	Run: func(cmd *cobra.Command, args []string) {
		setModelGraphQLTimeout(cmd)

		var modelFiles []modelFile

		if addModelDirectoryPath != "" {
			modelPath := filepath.Clean(addModelDirectoryPath)
			info, err := os.Stat(modelPath)
			if err != nil {
				cobra.CheckErr(fmt.Errorf("unable to read model directory: %w", err))
			}
			if !info.IsDir() {
				cobra.CheckErr(fmt.Errorf("model-path %q must be a directory", addModelDirectoryPath))
			}

			files, err := collectModelFiles(modelPath)
			cobra.CheckErr(err)
			if len(files) == 0 {
				cobra.CheckErr(fmt.Errorf("model-path %q does not contain any files to upload", addModelDirectoryPath))
			}

			modelFiles = files
			addModelCreateUpload = true
		}

		var metadata map[string]interface{}
		if len(addModelMetadata) > 0 {
			metadata = make(map[string]interface{}, len(addModelMetadata))
			for key, value := range addModelMetadata {
				metadata[key] = value
			}
		}

		input := &api.AddModelToRepoInput{
			Name:                addModelName,
			CredentialReference: addModelCredentialReference,
			CredentialType:      addModelCredentialType,
			ModelStatus:         addModelStatus,
			VersionStatus:       addModelVersionStatus,
			Metadata:            metadata,
		}

		model, err := api.AddModelToRepo(input)
		if err != nil {
			if errors.Is(err, api.ErrModelRepoNotImplemented) {
				fmt.Println(api.ErrModelRepoNotImplemented.Error())
				return
			}

			cobra.CheckErr(err)
			return
		}

		if model != nil {
			fmt.Printf("model %q registered with Model Repo (id: %s)\n", model.Name, model.ID)
		}

		shouldCreateUpload := addModelCreateUpload || addModelFileName != "" || addModelFileSize != "" || addModelPartSize != "" || addModelContentType != "" || len(addModelMetadata) > 0
		if !shouldCreateUpload {
			return
		}

		uploadInput := &api.CreateModelRepoUploadInput{
			PartSizeBytes:       addModelPartSize,
			ContentType:         addModelContentType,
			CredentialReference: addModelCredentialReference,
			CredentialType:      addModelCredentialType,
			Metadata:            metadata,
		}

		uploadInput.Name = addModelName

		if len(modelFiles) > 0 {
			err := uploadModelFiles(modelFiles, uploadInput)
			cobra.CheckErr(err)
			return
		}

		if addModelFileName == "" {
			cobra.CheckErr(fmt.Errorf("file-name is required when creating an upload"))
		}
		if addModelFileSize == "" {
			cobra.CheckErr(fmt.Errorf("file-size is required when creating an upload"))
		}

		uploadInput.FileName = addModelFileName
		uploadInput.FileSizeBytes = addModelFileSize

		result, err := api.CreateModelRepoUpload(uploadInput)
		cobra.CheckErr(err)

		if result.Upload == nil {
			cobra.CheckErr(fmt.Errorf("upload response missing upload session details"))
		}

		uploadJSON, err := json.MarshalIndent(result.Upload, "", "  ")
		cobra.CheckErr(err)
		fmt.Printf("multipart upload session created:\n%s\n", string(uploadJSON))
	},
}

func init() {
	AddModelToRepoCmd.Flags().StringVar(&addModelName, "name", "", "model name within your namespace")
	AddModelToRepoCmd.Flags().StringVar(&addModelCredentialReference, "credential-reference", "", "reference that allows RunPod to access the model artifact")
	AddModelToRepoCmd.Flags().StringVar(&addModelCredentialType, "credential-type", "", "type of credential used to access the model artifact (API_KEY, OAUTH_TOKEN, OTHER, USERNAME_PASSWORD)")
	AddModelToRepoCmd.Flags().StringVar(&addModelVersionStatus, "version-status", "", "status to assign to the uploaded model version")
	AddModelToRepoCmd.Flags().StringVar(&addModelStatus, "model-status", "", "status to assign to the model record")
	AddModelToRepoCmd.Flags().BoolVar(&addModelCreateUpload, "create-upload", false, "initialize a multipart upload session for the model artifact")
	AddModelToRepoCmd.Flags().StringVar(&addModelFileName, "file-name", "", "file name to use for the model artifact upload")
	AddModelToRepoCmd.Flags().StringVar(&addModelFileSize, "file-size", "", "size of the model artifact in bytes")
	AddModelToRepoCmd.Flags().StringVar(&addModelPartSize, "part-size", "", "preferred multipart upload part size in bytes")
	AddModelToRepoCmd.Flags().StringVar(&addModelContentType, "content-type", "", "content type for the model artifact upload")
	AddModelToRepoCmd.Flags().StringVar(&addModelDirectoryPath, "model-path", "", "path to a directory containing the model files to upload")
	AddModelToRepoCmd.Flags().StringToStringVar(&addModelMetadata, "metadata", nil, "key=value metadata to associate with the model and upload")

	AddModelToRepoCmd.MarkFlagRequired("name") //nolint
}

func collectModelFiles(dir string) ([]modelFile, error) {
	var files []modelFile

	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if path == dir {
			return nil
		}

		if d.IsDir() {
			return nil
		}

		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat file %q: %w", path, err)
		}

		if info.IsDir() {
			return fmt.Errorf("encountered directory %q while collecting files", path)
		}

		if !info.Mode().IsRegular() {
			return fmt.Errorf("model-path contains unsupported file type: %s", path)
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		files = append(files, modelFile{
			AbsolutePath: path,
			RelativePath: filepath.ToSlash(rel),
			Size:         info.Size(),
		})

		return nil
	})

	if walkErr != nil {
		return nil, fmt.Errorf("scan model directory: %w", walkErr)
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].RelativePath < files[j].RelativePath
	})

	return files, nil
}

func uploadModelFiles(files []modelFile, baseInput *api.CreateModelRepoUploadInput) error {
	for _, file := range files {
		input := *baseInput
		input.FileName = file.RelativePath
		input.FileSizeBytes = strconv.FormatInt(file.Size, 10)

		result, err := api.CreateModelRepoUpload(&input)
		if err != nil {
			return fmt.Errorf("create upload for %s: %w", file.RelativePath, err)
		}
		if result.Upload == nil {
			return fmt.Errorf("upload response missing upload session details for %s", file.RelativePath)
		}

		if err = completeModelUpload(result.Upload, file.AbsolutePath); err != nil {
			return fmt.Errorf("upload %s: %w", file.RelativePath, err)
		}

		if result.Upload.SessionID == "" {
			return fmt.Errorf("upload %s: missing session identifier for completion", file.RelativePath)
		}

		completion, err := api.CompleteModelRepoUpload(result.Upload.SessionID)
		if err != nil {
			return fmt.Errorf("complete upload session for %s: %w", file.RelativePath, err)
		}

		fmt.Printf("model artifact %q uploaded to %s (session %s status: %s)\n", file.RelativePath, result.Upload.Key, completion.SessionID, completion.Status)
	}

	return nil
}

func completeModelUpload(upload *api.ModelRepoUpload, artifactPath string) error {
	if upload == nil {
		return fmt.Errorf("upload details are required")
	}
	if len(upload.Parts) == 0 {
		return fmt.Errorf("upload does not contain any parts")
	}

	file, err := os.Open(artifactPath)
	if err != nil {
		return fmt.Errorf("open artifact: %w", err)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat artifact: %w", err)
	}

	parts := make([]*api.ModelRepoUploadPart, len(upload.Parts))
	copy(parts, upload.Parts)
	sort.Slice(parts, func(i, j int) bool {
		return parts[i].PartNumber < parts[j].PartNumber
	})

	partSize := upload.PartSizeBytes
	if partSize <= 0 {
		return fmt.Errorf("invalid part size %d", partSize)
	}

	totalSize := fileInfo.Size()
	var offset int64
	completed := make([]completedPart, 0, len(parts))

	for _, part := range parts {
		remaining := totalSize - offset
		if remaining <= 0 {
			return fmt.Errorf("no data remaining for part %d", part.PartNumber)
		}

		chunkSize := partSize
		if remaining < chunkSize {
			chunkSize = remaining
		}

		section := io.NewSectionReader(file, offset, chunkSize)
		req, err := http.NewRequest(http.MethodPut, part.URL, section)
		if err != nil {
			return fmt.Errorf("create request for part %d: %w", part.PartNumber, err)
		}
		req.ContentLength = chunkSize

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("upload part %d: %w", part.PartNumber, err)
		}
		func() {
			defer resp.Body.Close()
			if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
				body, _ := io.ReadAll(resp.Body)
				err = fmt.Errorf("upload part %d failed: status %d: %s", part.PartNumber, resp.StatusCode, strings.TrimSpace(string(body)))
				return
			}
			etag := strings.Trim(resp.Header.Get("ETag"), "\"")
			if etag == "" {
				err = fmt.Errorf("upload part %d missing ETag", part.PartNumber)
				return
			}
			completed = append(completed, completedPart{PartNumber: part.PartNumber, ETag: fmt.Sprintf("\"%s\"", etag)})
		}()
		if err != nil {
			return err
		}

		offset += chunkSize
	}

	if offset != totalSize {
		return fmt.Errorf("uploaded %d bytes but artifact size is %d bytes", offset, totalSize)
	}

	completePayload := completeMultipartUpload{
		XMLNS: "http://s3.amazonaws.com/doc/2006-03-01/",
		Parts: completed,
	}

	body, err := xml.Marshal(completePayload)
	if err != nil {
		return fmt.Errorf("marshal completion payload: %w", err)
	}

	payload := append([]byte(xml.Header), body...)

	completeReq, err := http.NewRequest(http.MethodPost, upload.CompleteURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create completion request: %w", err)
	}
	completeReq.Header.Set("Content-Type", "application/xml")

	resp, err := http.DefaultClient.Do(completeReq)
	if err != nil {
		return fmt.Errorf("complete upload: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("completion request failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil
}
