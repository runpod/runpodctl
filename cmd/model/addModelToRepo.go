package model

import (
	"bytes"
	"context"
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
	internalapi "github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	addModelOwner               string
	addModelName                string
	addModelHuggingFaceModel    string
	addModelCredentialReference string
	addModelCredentialType      string
	addModelStatus              string
	addModelCreateUpload        bool
	addModelFileName            string
	addModelFileSize            string
	addModelPartSize            string
	addModelContentType         string
	addModelDirectoryPath       string
	addModelMetadata            map[string]string
	addModelWaitForHash         bool
	addModelHashTimeout         time.Duration
	addModelVerbose             bool
	addModelDeleteAfterUpload   bool
)

const (
	graphqlTimeoutFlagName   = "graphql-timeout"
	modelGraphQLTimeoutValue = time.Minute
	modelHashPollInterval    = 5 * time.Second
	modelHashWaitTimeout     = 30 * time.Minute
)

// The GraphQL requests for uploading a model can take longer than the default
// 10s, as the server needs to create pre-signed S3 URLs from R2, which can be
// slow. So we bump it up to 1m so that we do not fail erroneously.
//
// Returns an error rather than exiting: cobra.CheckErr would print plaintext to
// stderr and os.Exit(1), bypassing the single JSON error sink in cmd/root.go.
func setModelGraphQLTimeout(cmd *cobra.Command) error {
	timeoutFlag := cmd.InheritedFlags().Lookup(graphqlTimeoutFlagName)
	if timeoutFlag != nil {
		if timeoutFlag.Changed {
			return nil
		}

		if err := timeoutFlag.Value.Set(modelGraphQLTimeoutValue.String()); err != nil {
			return fmt.Errorf("unable to set graphql timeout: %w", err)
		}

		viper.Set(api.GraphQLTimeoutKey, modelGraphQLTimeoutValue)
		return nil
	}

	// In the CLI-restructure flow, the inherited --graphql-timeout flag is removed.
	// Preserve any explicit timeout already set in config/env, otherwise apply the
	// safer model upload default.
	if currentTimeout := viper.GetDuration(api.GraphQLTimeoutKey); currentTimeout > 0 {
		return nil
	}

	viper.Set(api.GraphQLTimeoutKey, modelGraphQLTimeoutValue)
	return nil
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

type uploadedModelFile struct {
	RelativePath string `json:"relativePath"`
	Key          string `json:"key"`
	SessionID    string `json:"sessionId"`
	Status       string `json:"status"`
}

type modelAddOutput struct {
	Model                  *api.Model           `json:"model,omitempty"`
	Upload                 *api.ModelRepoUpload `json:"upload,omitempty"`
	UploadedFiles          []uploadedModelFile  `json:"uploadedFiles,omitempty"`
	ModelSizeBytes         *int64               `json:"modelSizeBytes,omitempty"`
	ModelHash              string               `json:"modelHash,omitempty"`
	ModelURL               string               `json:"modelUrl,omitempty"`
	DeletedModelFiles      int                  `json:"deletedModelFiles,omitempty"`
	DeletedModelFilesBytes int64                `json:"deletedModelFilesBytes,omitempty"`
}

type compactModelAddOutput struct {
	Model compactModel `json:"model"`
}

type compactModel struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Owner string `json:"owner,omitempty"`
}

type modelReadyOutput struct {
	Owner     string
	Name      string
	ModelHash string
	ModelURL  string
}

type modelUploadProgress interface {
	Add64(int64) error
	Finish() error
	Clear() error
}

type progressReader struct {
	reader   io.Reader
	progress modelUploadProgress
}

// TODO: replace the manual completion call with github.com/aws/aws-sdk-go-v2/service/s3's
// CompleteMultipartUpload to rely on the SDK for payload formatting and signing logic.
var (
	addModelToRepo          = api.AddModelToRepo
	createModelRepoUpload   = api.CreateModelRepoUpload
	completeModelRepoUpload = api.CompleteModelRepoUpload
	completeModelUploadFile = completeModelUploadWithProgress
	getModelsForAdd         = api.GetModels
	sleepModelHashPoll      = waitModelHashPoll
	removeModelFile         = os.Remove
)

var addCmd = &cobra.Command{
	Use:   "add",
	Args:  cobra.ExactArgs(0),
	Short: "add a model",
	Long:  "add a model to the runpod model repository",
	Example: `  # --name is the destination; --huggingface-model is the source
  runpodctl model add --name tiny-llm --huggingface-model arnir0/Tiny-LLM

  # upload local files, then delete them once the model version hash is confirmed
  runpodctl model add --name my-model --model-path ./my-model-dir --wait-for-hash --delete-my-model-files-after-upload`,
	RunE: runAddModel,
}

var AddModelToRepoCmd = &cobra.Command{
	Use:    "model",
	Args:   cobra.ExactArgs(0),
	Short:  "deprecated: use 'runpodctl model add'",
	Long:   "",
	Hidden: true,
	RunE:   runAddModel,
}

func init() {
	bindAddModelFlags(addCmd)
	bindAddModelFlags(AddModelToRepoCmd)
	addCmd.MarkFlagRequired("name")            //nolint
	AddModelToRepoCmd.MarkFlagRequired("name") //nolint
}

func bindAddModelFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&addModelOwner, "owner", "", "model owner namespace (user or team owner id)")
	cmd.Flags().StringVar(&addModelName, "name", "", "model name")
	cmd.Flags().StringVar(&addModelHuggingFaceModel, "huggingface-model", "", "hugging face model to mirror (owner/repo)")
	cmd.Flags().StringVar(&addModelCredentialReference, "credential-reference", "", "credential reference (if required)")
	cmd.Flags().StringVar(&addModelCredentialType, "credential-type", "", "credential type (if required)")
	cmd.Flags().StringVar(&addModelStatus, "model-status", "", "initial model status")
	cmd.Flags().BoolVar(&addModelCreateUpload, "create-upload", false, "create an upload session")
	cmd.Flags().StringVar(&addModelFileName, "file-name", "", "file name for upload")
	cmd.Flags().StringVar(&addModelFileSize, "file-size", "", "file size in bytes")
	cmd.Flags().StringVar(&addModelPartSize, "part-size", "", "multipart upload part size in bytes")
	cmd.Flags().StringVar(&addModelContentType, "content-type", "", "upload content type")
	cmd.Flags().StringVar(&addModelDirectoryPath, "model-path", "", "directory containing model files to upload")
	cmd.Flags().StringToStringVar(&addModelMetadata, "metadata", nil, "metadata key=value pairs")
	cmd.Flags().BoolVar(&addModelWaitForHash, "wait-for-hash", false, "wait for completed model-path uploads to be hashed")
	cmd.Flags().DurationVar(&addModelHashTimeout, "hash-timeout", modelHashWaitTimeout, "maximum duration to wait for --wait-for-hash (0 disables timeout)")
	cmd.Flags().BoolVarP(&addModelVerbose, "verbose", "v", false, "include upload details in wait-for-hash output")
	cmd.Flags().BoolVar(&addModelDeleteAfterUpload, "delete-my-model-files-after-upload", false, "delete the uploaded --model-path files once the model version hash is confirmed (requires --wait-for-hash)")
}

func isHuggingFaceMirror() bool {
	return strings.TrimSpace(addModelHuggingFaceModel) != ""
}

// wantsUploadSession reports whether the flags ask for a local upload session.
// Metadata retains its legacy upload behavior except when it belongs to a
// server-side Hugging Face mirror.
func wantsUploadSession() bool {
	return addModelCreateUpload || addModelFileName != "" || addModelFileSize != "" ||
		addModelPartSize != "" || addModelContentType != "" ||
		(len(addModelMetadata) > 0 && !isHuggingFaceMirror())
}

func runAddModel(cmd *cobra.Command, args []string) error {
	if err := setModelGraphQLTimeout(cmd); err != nil {
		return err
	}
	if err := validateAddModelFlags(); err != nil {
		return err
	}

	var modelFiles []modelFile

	if addModelDirectoryPath != "" {
		modelPath := filepath.Clean(addModelDirectoryPath)
		info, err := os.Stat(modelPath)
		if err != nil {
			// not_found is reserved for resources the *api* does not have. a
			// mistyped local path is user input, so it takes the cli_error
			// fallback: an agent that saw not_found here would reasonably read it
			// as "the model is missing server-side" and retry with a different
			// name instead of fixing the path.
			if errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("model-path %q does not exist", addModelDirectoryPath)
			}
			return fmt.Errorf("unable to read model directory: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("model-path %q must be a directory", addModelDirectoryPath)
		}

		files, err := collectModelFiles(modelPath)
		if err != nil {
			return err
		}
		if len(files) == 0 {
			return fmt.Errorf("model-path %q does not contain any files to upload", addModelDirectoryPath)
		}

		modelFiles = files
		addModelCreateUpload = true
	}

	isUploadFlow := addModelCreateUpload || addModelFileName != "" || addModelFileSize != "" || addModelPartSize != "" || addModelContentType != ""

	var metadata map[string]interface{}
	if len(addModelMetadata) > 0 {
		metadata = make(map[string]interface{}, len(addModelMetadata))
		for key, value := range addModelMetadata {
			metadata[key] = value
		}
	}

	if addModelWaitForHash && len(modelFiles) == 0 {
		return fmt.Errorf("--wait-for-hash requires --model-path")
	}

	// validate the single-file upload flags BEFORE creating the model. these
	// checks used to run after addModelToRepo, so a missing --file-name left a
	// model created server-side while the cli reported a plain validation error —
	// an agent reading that as "bad input, nothing happened" and retrying would
	// create a duplicate.
	if wantsUploadSession() && len(modelFiles) == 0 {
		if addModelFileName == "" {
			return fmt.Errorf("file-name is required when creating an upload")
		}
		if addModelFileSize == "" {
			return fmt.Errorf("file-size is required when creating an upload")
		}
	}

	input := &api.AddModelToRepoInput{
		Owner:               addModelOwner,
		Name:                addModelName,
		HuggingFaceModel:    addModelHuggingFaceModel,
		CredentialReference: addModelCredentialReference,
		CredentialType:      addModelCredentialType,
		ModelStatus:         addModelStatus,
		Metadata:            metadata,
	}
	if isUploadFlow {
		input.Provider = "LOCAL"
	}

	model, err := addModelToRepo(input)
	if err != nil {

		return err
	}

	if !wantsUploadSession() {
		return printModelAddOutput(cmd, modelAddOutput{Model: model})
	}

	uploadInput := &api.CreateModelRepoUploadInput{
		Owner:               addModelOwner,
		PartSizeBytes:       addModelPartSize,
		ContentType:         addModelContentType,
		CredentialReference: addModelCredentialReference,
		CredentialType:      addModelCredentialType,
		Metadata:            metadata,
	}

	uploadInput.Name = addModelName

	if len(modelFiles) > 0 {
		uploadedFiles, uploadModel, modelVersionUUID, err := uploadModelFiles(modelFiles, uploadInput)
		if err != nil {
			return err
		}
		if uploadModel != nil {
			model = uploadModel
		}
		modelSizeBytes := totalModelFileSize(modelFiles)
		result := modelAddOutput{Model: model, UploadedFiles: uploadedFiles, ModelSizeBytes: &modelSizeBytes}
		if addModelWaitForHash {
			if modelVersionUUID == "" {
				return fmt.Errorf("upload response missing model version uuid required by --wait-for-hash")
			}
			ctx := context.Background()
			var cancel context.CancelFunc
			if addModelHashTimeout > 0 {
				ctx, cancel = context.WithTimeout(ctx, addModelHashTimeout)
				defer cancel()
			}
			ready, err := waitForUploadedModelHash(ctx, addModelOwner, addModelName, model, modelVersionUUID, modelHashPollInterval)
			if err != nil {
				return err
			}
			result.ModelHash = ready.ModelHash
			result.ModelURL = ready.ModelURL
			printModelReadyURL(ready.ModelURL)

			if addModelDeleteAfterUpload {
				deletedCount, deletedBytes, deleteErr := deleteVerifiedModelFiles(modelFiles)
				result.DeletedModelFiles = deletedCount
				result.DeletedModelFilesBytes = deletedBytes
				if deleteErr != nil {
					// the upload and hash are already confirmed above, so this is
					// reported as its own failure rather than folded into a generic
					// upload error: the model is fully usable, only local cleanup
					// did not complete.
					return deleteErr
				}
			}

			if !addModelVerbose {
				return printCompactModelAddOutput(cmd, model)
			}
		}
		return printModelAddOutput(cmd, result)
	}

	if addModelFileName == "" {
		return fmt.Errorf("file-name is required when creating an upload")
	}
	if addModelFileSize == "" {
		return fmt.Errorf("file-size is required when creating an upload")
	}

	uploadInput.FileName = addModelFileName
	uploadInput.FileSizeBytes = addModelFileSize

	// use the package-var seam (as uploadModelFiles does) so this branch is
	// stubbable in tests; calling api.CreateModelRepoUpload directly is why it had
	// no coverage.
	result, err := createModelRepoUpload(uploadInput)
	if err != nil {
		return err
	}

	if result.Upload == nil {
		return fmt.Errorf("upload response missing upload session details")
	}
	if result.Model != nil {
		model = result.Model
	}

	return printModelAddOutput(cmd, modelAddOutput{Model: model, Upload: result.Upload})
}

func validateAddModelFlags() error {
	if addModelDeleteAfterUpload {
		// checked before the huggingface-model conflict below so a user who
		// combines all three flags gets the more specific, more actionable
		// error first instead of the generic "cannot be combined" message.
		if addModelDirectoryPath == "" {
			return fmt.Errorf("--delete-my-model-files-after-upload requires --model-path")
		}
		if !addModelWaitForHash {
			return fmt.Errorf("--delete-my-model-files-after-upload requires --wait-for-hash: files are only deleted once the model version hash is confirmed server-side")
		}
	}

	if !isHuggingFaceMirror() {
		return nil
	}

	if addModelDirectoryPath != "" || addModelCreateUpload || addModelFileName != "" || addModelFileSize != "" || addModelPartSize != "" || addModelContentType != "" || addModelWaitForHash {
		return fmt.Errorf("--huggingface-model cannot be combined with local upload flags")
	}

	return nil
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

func (r progressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 && r.progress != nil {
		_ = r.progress.Add64(int64(n))
	}
	return n, err
}

func totalModelFileSize(files []modelFile) int64 {
	var total int64
	for _, file := range files {
		total += file.Size
	}
	return total
}

// deleteVerifiedModelFiles removes the local --model-path files backing
// --delete-my-model-files-after-upload. Callers must only invoke this after
// every file in `files` has both (a) completed its multipart upload
// (uploadModelFiles returned without error) and (b) had its model version
// hash confirmed server-side (waitForUploadedModelHash returned without
// error) — this function does not itself re-check either condition. It never
// deletes anything else in --model-path: only the exact absolute paths that
// were part of this run's verified upload, never a directory-wide wipe.
func deleteVerifiedModelFiles(files []modelFile) (deletedCount int, deletedBytes int64, err error) {
	var failures []string

	for _, file := range files {
		if removeErr := removeModelFile(file.AbsolutePath); removeErr != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", file.RelativePath, removeErr))
			continue
		}
		deletedCount++
		deletedBytes += file.Size
	}

	if deletedCount > 0 {
		fmt.Fprintf(os.Stderr, "deleted %d verified model file(s) (%d bytes) from %s\n", deletedCount, deletedBytes, addModelDirectoryPath)
	}

	if len(failures) > 0 {
		return deletedCount, deletedBytes, fmt.Errorf("upload and hash are confirmed, but failed to delete %d of %d model file(s) from %s: %s", len(failures), len(files), addModelDirectoryPath, strings.Join(failures, "; "))
	}

	return deletedCount, deletedBytes, nil
}

func stderrIsTerminal() bool {
	info, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func newModelUploadProgress(totalBytes int64) modelUploadProgress {
	if totalBytes <= 0 || !stderrIsTerminal() {
		return nil
	}

	return progressbar.NewOptions64(totalBytes,
		progressbar.OptionOnCompletion(func() {
			fmt.Fprintln(os.Stderr)
		}),
		progressbar.OptionSetDescription("uploading model"),
		progressbar.OptionSetWidth(20),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionShowBytes(true),
		progressbar.OptionShowCount(),
		progressbar.OptionSetPredictTime(true),
		progressbar.OptionThrottle(100*time.Millisecond),
		progressbar.OptionSetWriter(os.Stderr),
	)
}

func printCompletedModelUploadSize(totalBytes int64) {
	fmt.Fprintf(os.Stderr, "model size: %d bytes\n", totalBytes)
}

func waitForUploadedModelHash(ctx context.Context, owner, name string, uploadedModel *api.Model, modelVersionUUID string, pollInterval time.Duration) (*modelReadyOutput, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	name = strings.TrimSpace(name)
	if name == "" && uploadedModel != nil {
		name = strings.TrimSpace(uploadedModel.Name)
	}
	if name == "" {
		return nil, fmt.Errorf("model name is required to wait for hashing")
	}
	modelVersionUUID = strings.TrimSpace(modelVersionUUID)
	if modelVersionUUID == "" {
		return nil, fmt.Errorf("model version uuid is required to wait for hashing")
	}

	fmt.Fprint(os.Stderr, "waiting for model to be hashed")
	defer fmt.Fprintln(os.Stderr)

	for {
		if err := ctx.Err(); err != nil {
			return nil, internalapi.NewTimeoutError("upload completed but timed out waiting for the model hash; the model exists, do not re-upload: %v", err)
		}

		models, err := getModelsForAdd(&api.GetModelsInput{Name: name})
		if err != nil {
			return nil, fmt.Errorf("get model hash: %w", err)
		}

		model := findUploadedModel(models, owner, name, uploadedModel)
		hash := uploadedModelVersionHash(model, modelVersionUUID)
		if hash != "" {
			ownerID := modelOwnerForURL(owner, uploadedModel, model)
			if ownerID == "" {
				return nil, fmt.Errorf("model owner is required to build model url")
			}
			modelName := modelNameForURL(name, uploadedModel, model)
			if modelName == "" {
				return nil, fmt.Errorf("model name is required to build model url")
			}

			modelURL := formatModelURL(ownerID, modelName, hash)
			return &modelReadyOutput{
				Owner:     ownerID,
				Name:      modelName,
				ModelHash: hash,
				ModelURL:  modelURL,
			}, nil
		}

		fmt.Fprint(os.Stderr, ".")
		if err := sleepModelHashPoll(ctx, pollInterval); err != nil {
			return nil, internalapi.NewTimeoutError("upload completed but timed out waiting for the model hash; the model exists, do not re-upload: %v", err)
		}
	}
}

func uploadedModelVersionHash(model *api.Model, modelVersionUUID string) string {
	if model == nil {
		return ""
	}
	modelVersionUUID = strings.TrimSpace(modelVersionUUID)
	if modelVersionUUID == "" {
		return ""
	}
	for _, version := range model.Versions {
		if version == nil || strings.TrimSpace(version.UUID) != modelVersionUUID {
			continue
		}
		if hash := strings.TrimSpace(version.Hash); hash != "" {
			return hash
		}
		return ""
	}
	return ""
}

func waitModelHashPoll(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func findUploadedModel(models []*api.Model, owner, name string, uploadedModel *api.Model) *api.Model {
	owner = strings.TrimSpace(owner)
	name = strings.TrimSpace(name)
	uploadedID := ""
	uploadedOwner := ""
	uploadedName := ""
	if uploadedModel != nil {
		uploadedID = strings.TrimSpace(uploadedModel.ID)
		uploadedOwner = strings.TrimSpace(uploadedModel.Owner)
		uploadedName = strings.TrimSpace(uploadedModel.Name)
	}
	if owner == "" {
		owner = uploadedOwner
	}
	if name == "" {
		name = uploadedName
	}

	for _, model := range models {
		if model == nil {
			continue
		}
		if uploadedID != "" && strings.TrimSpace(model.ID) == uploadedID {
			return model
		}
		if name != "" && strings.TrimSpace(model.Name) != name {
			continue
		}
		if owner != "" && strings.TrimSpace(model.Owner) != owner {
			continue
		}
		return model
	}

	return nil
}

func modelOwnerForURL(owner string, uploadedModel, polledModel *api.Model) string {
	if owner = strings.TrimSpace(owner); owner != "" {
		return owner
	}
	if polledModel != nil {
		if owner = strings.TrimSpace(polledModel.Owner); owner != "" {
			return owner
		}
	}
	if uploadedModel != nil {
		return strings.TrimSpace(uploadedModel.Owner)
	}
	return ""
}

func modelNameForURL(name string, uploadedModel, polledModel *api.Model) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	if polledModel != nil {
		if name = strings.TrimSpace(polledModel.Name); name != "" {
			return name
		}
	}
	if uploadedModel != nil {
		return strings.TrimSpace(uploadedModel.Name)
	}
	return ""
}

func formatModelURL(owner, name, hash string) string {
	return fmt.Sprintf("https://local/%s/%s:%s", owner, name, hash)
}

func printModelReadyURL(modelURL string) {
	fmt.Fprintf(os.Stderr, "model is ready to deploy, your model url is: \"%s\"\n", shellDoubleQuoteValue(modelURL))
}

func shellDoubleQuoteValue(value string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		`$`, `\$`,
		"`", "\\`",
	)
	return replacer.Replace(value)
}

func uploadModelFiles(files []modelFile, baseInput *api.CreateModelRepoUploadInput) ([]uploadedModelFile, *api.Model, string, error) {
	var modelVersionUUID string
	var uploadModel *api.Model
	uploadedFiles := make([]uploadedModelFile, 0, len(files))
	totalSize := totalModelFileSize(files)
	progress := newModelUploadProgress(totalSize)

	for i, file := range files {
		input := *baseInput
		input.FileName = file.RelativePath
		input.FileSizeBytes = strconv.FormatInt(file.Size, 10)
		input.ModelVersionUUID = modelVersionUUID

		result, err := createModelRepoUpload(&input)
		if err != nil {
			if progress != nil {
				_ = progress.Clear()
			}
			return nil, nil, "", fmt.Errorf("create upload for %s: %w", file.RelativePath, err)
		}
		if result.Upload == nil {
			if progress != nil {
				_ = progress.Clear()
			}
			return nil, nil, "", fmt.Errorf("upload response missing upload session details for %s", file.RelativePath)
		}
		if result.Model != nil {
			uploadModel = result.Model
		}
		if modelVersionUUID == "" {
			if result.Version != nil {
				modelVersionUUID = strings.TrimSpace(result.Version.UUID)
			}
			if modelVersionUUID == "" && i < len(files)-1 {
				if progress != nil {
					_ = progress.Clear()
				}
				return nil, nil, "", fmt.Errorf("upload response missing model version uuid for %s", file.RelativePath)
			}
		}

		if err = completeModelUploadFile(result.Upload, file.AbsolutePath, progress); err != nil {
			if progress != nil {
				_ = progress.Clear()
			}
			return nil, nil, "", fmt.Errorf("upload %s: %w", file.RelativePath, err)
		}

		if result.Upload.SessionID == "" {
			if progress != nil {
				_ = progress.Clear()
			}
			return nil, nil, "", fmt.Errorf("upload %s: missing session identifier for completion", file.RelativePath)
		}

		uploadedFiles = append(uploadedFiles, uploadedModelFile{
			RelativePath: file.RelativePath,
			Key:          result.Upload.Key,
			SessionID:    result.Upload.SessionID,
		})
	}

	for i := range uploadedFiles {
		completion, err := completeModelRepoUpload(uploadedFiles[i].SessionID)
		if err != nil {
			if progress != nil {
				_ = progress.Clear()
			}
			return nil, nil, "", fmt.Errorf("complete upload session for %s: %w", uploadedFiles[i].RelativePath, err)
		}

		uploadedFiles[i].SessionID = completion.SessionID
		uploadedFiles[i].Status = completion.Status
	}

	if progress != nil {
		_ = progress.Finish()
	}
	printCompletedModelUploadSize(totalSize)

	return uploadedFiles, uploadModel, modelVersionUUID, nil
}

// printModelAddOutput and printCompactModelAddOutput return the encode error to
// the handler instead of calling cobra.CheckErr, which would print plaintext and
// exit before the JSON sink in cmd/root.go ever ran.
func printModelAddOutput(cmd *cobra.Command, result modelAddOutput) error {
	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(result, &output.Config{Format: format})
}

func printCompactModelAddOutput(cmd *cobra.Command, model *api.Model) error {
	format := output.ParseFormat(cmd.Flag("output").Value.String())
	compact := compactModel{}
	if model != nil {
		compact.ID = strings.TrimSpace(model.ID)
		compact.Name = strings.TrimSpace(model.Name)
		compact.Owner = strings.TrimSpace(model.Owner)
	}
	return output.Print(compactModelAddOutput{
		Model: compact,
	}, &output.Config{Format: format})
}

func completeModelUpload(upload *api.ModelRepoUpload, artifactPath string) error {
	return completeModelUploadWithProgress(upload, artifactPath, nil)
}

func completeModelUploadWithProgress(upload *api.ModelRepoUpload, artifactPath string, progress modelUploadProgress) error {
	if upload == nil {
		return fmt.Errorf("upload details are required")
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

	totalSize := fileInfo.Size()
	if totalSize == 0 {
		if len(upload.Parts) > 0 {
			return fmt.Errorf("zero-byte upload should not contain any parts")
		}
		return nil
	}

	if len(upload.Parts) == 0 {
		return fmt.Errorf("upload does not contain any parts")
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

		var body io.Reader = io.NewSectionReader(file, offset, chunkSize)
		if progress != nil {
			body = progressReader{reader: body, progress: progress}
		}

		req, err := http.NewRequest(http.MethodPut, part.URL, body)
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
			completed = append(completed, completedPart{PartNumber: part.PartNumber, ETag: fmt.Sprintf("%q", etag)})
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
