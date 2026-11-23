package model

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/runpod/runpodctl/api"
	internalapi "github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"
	"github.com/runpod/runpodctl/internal/redact"

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

	// placeholderModelVersionHashPrefix marks the hash the api assigns when it
	// creates a version, before the real content hash is known.
	placeholderModelVersionHashPrefix = "ph-"

	// dedupedCanonicalVersionMetadataKey is where the api records the canonical
	// version a deduped upload was folded into, as {uuid, hash}.
	dedupedCanonicalVersionMetadataKey = "dedupedToCanonicalVersion"
)

var deployableModelVersionStatuses = map[string]bool{
	api.ModelVersionStatusPodReady: true,
	api.ModelVersionStatusReady:    true,
}

// terminalModelVersionStatuses will never become deployable. DEPRECATED is
// terminal only when it carries no canonical-version pointer, which
// uploadedModelVersionState checks first.
var terminalModelVersionStatuses = map[string]bool{
	api.ModelVersionStatusFailed:     true,
	api.ModelVersionStatusDeprecated: true,
	api.ModelVersionStatusPodRemoved: true,
}

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
	// AbsolutePath is the path as found under --model-path. For a symlink it is
	// the link itself, which is what --delete-my-model-files-after-upload removes.
	AbsolutePath string
	// ResolvedPath is the fully symlink-resolved path that was checked to be
	// inside the containment roots; the upload opens this, never AbsolutePath.
	// Empty means AbsolutePath is used as is.
	ResolvedPath string
	RelativePath string
	Size         int64
	// info is the scan-time lstat of ResolvedPath, identity-pinned at scan time
	// (see pinScanFileIdentity). The upload re-checks the opened file against it
	// so a path swapped after the scan is refused.
	info os.FileInfo
	// linkInfo is the scan-time lstat of the symlink itself, set only when the
	// entry reached its target through a link (isSymlink). The post-upload
	// delete removes the link by name and compares the entry's identity against
	// this before doing so; for a regular entry the delete compares against
	// info instead. Also identity-pinned at scan time.
	linkInfo os.FileInfo
	// deleteRootDir is the scanned (symlink-resolved) --model-path directory and
	// deleteRootInfo its scan-time stat through the pinned containment root
	// (identity pinned at scan time). The post-upload delete re-opens the
	// directory (os.Root) and refuses to run when it is no longer the same
	// directory, so a directory swapped for a symlink after the scan cannot
	// redirect a removal outside it; deleteName is the removed entry's path
	// relative to deleteRootDir (the link itself for a symlink, never its
	// target).
	deleteRootDir  string
	deleteRootInfo os.FileInfo
	deleteName     string
}

// isSymlink reports whether the scanned entry was a symlink to its target.
func (f *modelFile) isSymlink() bool {
	return f.ResolvedPath != "" && f.ResolvedPath != f.AbsolutePath
}

// uploadPath is the path the upload must open.
func (f *modelFile) uploadPath() string {
	if f.ResolvedPath != "" {
		return f.ResolvedPath
	}
	return f.AbsolutePath
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
	// RemovedSymlinks counts symlinks removed by --delete-my-model-files-after-upload.
	// Only the link is removed, never its target through the link; a target that
	// is itself a collected entry (an in-root link) is deleted as that regular
	// entry, so symlinks are not part of DeletedModelFiles/DeletedModelFilesBytes.
	RemovedSymlinks int `json:"removedSymlinks,omitempty"`
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

// TODO: replace the manual completion call with github.com/aws/aws-sdk-go-v2/service/s3's
// CompleteMultipartUpload to rely on the SDK for payload formatting and signing logic.
var (
	addModelToRepo             = api.AddModelToRepo
	createModelRepoUpload      = api.CreateModelRepoUpload
	createModelRepoUploadBatch = api.CreateModelRepoUploadBatch
	completeModelRepoUploadAll = api.CompleteModelRepoUploadBatch
	completeModelUploadFile    = completeModelUploadWithContext
	getModelsForAdd            = api.GetModels
	getModelRepoStorageUsage   = api.GetModelRepoStorageUsage
	sleepModelHashPoll         = waitModelHashPoll
	removeModelFileInRoot      = func(root *os.Root, name string) error { return root.Remove(name) }
)

// modelRepoUploadBatchSize caps the files per createModelRepoUploadBatch request, bounding both
// the payload and the S3 calls the server fans out per invocation. Must stay <= the server's own
// MAX_MODEL_REPO_UPLOAD_BATCH_FILES (runpod/RunPod node/graphql/schema/modelRepo.ts).
const modelRepoUploadBatchSize = 500

// modelRepoUploadConcurrency bounds how many files within a single batch chunk are
// uploaded to S3 at once. A whole chunk's presigned URLs (up to modelRepoUploadBatchSize)
// are minted together in one createModelRepoUploadBatch call; transferring them one file
// at a time left the tail of a large/slow chunk sitting on a presigned URL for close to
// its expiresInSeconds TTL, and serialized S3 round trips were the dominant remaining
// cost for the many-small-files case this batching work targets.
const modelRepoUploadConcurrency = 4

// modelPartUploadConcurrency bounds how many multipart PUTs of a *single* file
// run at once. Kept modest because it multiplies with modelRepoUploadConcurrency
// when several multi-part files upload together (4x4 worst case).
const modelPartUploadConcurrency = 4

// checkModelRepoStorageQuota rejects an upload of requestedBytes that would exceed the
// account's quota, before anything is created server-side. Deliberately fails open: an
// unreachable or unparseable usage query only warns, because a client-side pre-check must
// never itself break an otherwise-valid upload. createModelRepoUpload's server-side gate
// enforces the real limit regardless.
func checkModelRepoStorageQuota(owner string, requestedBytes int64) error {
	usage, err := getModelRepoStorageUsage(owner)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not verify model repo storage quota before upload: %v\n", err)
		return nil
	}
	if usage == nil || !usage.Enforced || usage.AvailableBytes == nil {
		return nil
	}

	available, err := strconv.ParseInt(strings.TrimSpace(*usage.AvailableBytes), 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not parse model repo storage quota response: %v\n", err)
		return nil
	}

	if requestedBytes > available {
		return fmt.Errorf(
			"model upload of %d bytes would exceed the available Model Repo storage quota (%d bytes available); free up space or contact support to request a higher quota",
			requestedBytes, available,
		)
	}

	return nil
}

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
	cmd.Flags().BoolVar(&addModelWaitForHash, "wait-for-hash", false, "wait for completed model-path uploads to become deployable")
	cmd.Flags().DurationVar(&addModelHashTimeout, "hash-timeout", modelHashWaitTimeout, "maximum duration to wait for --wait-for-hash (0 disables timeout)")
	cmd.Flags().BoolVarP(&addModelVerbose, "verbose", "v", false, "include upload details in wait-for-hash output")
	cmd.Flags().BoolVar(&addModelDeleteAfterUpload, "delete-my-model-files-after-upload", false, "delete the uploaded --model-path files once the model version is confirmed deployable (requires --wait-for-hash)")
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

	if wantsUploadSession() {
		if requestedBytes, ok := modelUploadRequestedBytes(modelFiles); ok {
			if err := checkModelRepoStorageQuota(addModelOwner, requestedBytes); err != nil {
				return err
			}
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
				deletedCount, deletedBytes, removedSymlinks, deleteErr := deleteVerifiedModelFiles(modelFiles)
				result.DeletedModelFiles = deletedCount
				result.DeletedModelFilesBytes = deletedBytes
				result.RemovedSymlinks = removedSymlinks
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
			return fmt.Errorf("--delete-my-model-files-after-upload requires --wait-for-hash: files are only deleted once the model version is confirmed deployable server-side")
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

// pinScanFileIdentity forces the scan-time FileInfo to load its file identity
// now. On Windows os.Lstat/os.Stat fill in the volume and file index lazily, on
// the first os.SameFile call (os.loadFileId); without this the first call would
// be the upload-time os.SameFile(scanned, opened), which reads the scanned
// file's identity from whatever sits at the path by then, after any swap, and
// compares the swapped file to itself. os.SameFile(info, info) triggers the
// load at scan time; on Unix the identity comes with the stat result and this is
// a cheap no-op. It is a var so tests can observe that the call happens.
var pinScanFileIdentity = pinFileIdentity

func pinFileIdentity(info os.FileInfo) error {
	if !os.SameFile(info, info) {
		return errors.New("unable to determine the identity of the file")
	}
	return nil
}

// scannedModelInfo is the FileInfo kept for a scanned model file. Besides the
// scan-time lstat it records where the file was found: the containment root it
// lives under (with that root's scan-time identity) and its name relative to
// that root. The upload reopens the root and opens the file through it
// (os.Root), so a directory swapped for a symlink after the scan can never lead
// the open outside the root.
type scannedModelInfo struct {
	os.FileInfo
	rootDir  string
	rootInfo os.FileInfo
	name     string
}

// scanRoots holds the containment roots for the duration of one scan. pin
// opens every root up front; a root that does not exist yet (an HF repo with
// no blobs/ directory) opens lazily at first use instead.
type scanRoots struct {
	dirs    []string
	handles []*os.Root
	infos   []os.FileInfo
}

func newScanRoots(dirs []string) *scanRoots {
	return &scanRoots{dirs: dirs, handles: make([]*os.Root, len(dirs)), infos: make([]os.FileInfo, len(dirs))}
}

// locate returns the index of the root containing path and path relative to it.
func (s *scanRoots) locate(path string) (idx int, rel string, ok bool) {
	for i, dir := range s.dirs {
		if r, within := relWithin(dir, path); within {
			return i, r, true
		}
	}
	return 0, "", false
}

func (s *scanRoots) open(i int) (*os.Root, os.FileInfo, error) {
	if s.handles[i] != nil {
		return s.handles[i], s.infos[i], nil
	}
	handle, err := os.OpenRoot(s.dirs[i])
	if err != nil {
		return nil, nil, fmt.Errorf("open %q as a root: %w", s.dirs[i], err)
	}
	info, err := handle.Stat(".")
	if err == nil {
		err = pinFileIdentity(info)
	}
	if err != nil {
		return nil, nil, errors.Join(fmt.Errorf("stat root %q: %w", s.dirs[i], err), handle.Close())
	}
	s.handles[i], s.infos[i] = handle, info
	return handle, info, nil
}

// pin opens every containment root before the walk starts, so the first open
// of each happens now, in one place, instead of lazily mid-walk (the blobs
// root of a Hugging Face cache would otherwise open when the first link into
// it is found). A root opened earlier follows a directory swapped in between
// the path resolution above and the open; pinning shrinks that window to the
// few instructions between EvalSymlinks and here. Extra roots that do not
// exist (an HF repo with no blobs/ directory and no links into it) open
// lazily at first use instead.
func (s *scanRoots) pin() error {
	for i := range s.dirs {
		if _, _, err := s.open(i); err != nil {
			if i > 0 && errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return err
		}
	}
	return nil
}

func (s *scanRoots) close() error {
	var errs []error
	for _, h := range s.handles {
		if h != nil {
			errs = append(errs, h.Close())
		}
	}
	return errors.Join(errs...)
}

// afterScanEntryHook, when set, runs in the scan after an entry is read and
// before it is statted. Tests use it to swap paths inside that window.
var afterScanEntryHook func(path string)

// errSymlinkOutsideModelPath is the sentinel for a symlink that resolves outside
// the containment roots.
var errSymlinkOutsideModelPath = errors.New("model-path contains a symlink that points outside it")

// symlinkOutsideModelPathHint tells the user how to fix a refused symlink. Links
// into ~/.cache/huggingface (older `huggingface-cli download --local-dir`
// layouts) used to be followed silently, so the hint names them explicitly.
const symlinkOutsideModelPathHint = "To fix: copy the real files into the directory (`cp -rL <src> <dst>` or `rsync -rL <src>/ <dst>/`) and point --model-path at the copy, " +
	"or point --model-path at the Hugging Face snapshot directory itself (models--<org>--<name>/snapshots/<rev>), whose links into the same repo's blobs/ are allowed. " +
	"Directories created by older `huggingface-cli download --local-dir` runs contain links into ~/.cache/huggingface and need the same treatment"

// collectModelFiles lists the regular files under dir. Symlinks are resolved and
// must land inside the containment roots (see modelContainmentRoots): a link
// pointing anywhere else is refused rather than silently uploading a file that
// is not part of --model-path. Symlinked directories, FIFOs, devices and other
// non-regular files are refused too.
func collectModelFiles(dir string) ([]modelFile, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve model directory: %w", err)
	}
	root, err := filepath.EvalSymlinks(absDir)
	if err != nil {
		return nil, fmt.Errorf("resolve model directory: %w", err)
	}
	roots := newScanRoots(modelContainmentRoots(root))
	// Pin the containment roots before walking (see scanRoots.pin): the first
	// open of each root happens now, narrowing the resolve-to-open window to
	// the handful of instructions between EvalSymlinks and pin. A swap of
	// --model-path inside that window is still followed, by design, the same
	// as passing the replacement directory directly. That residual cannot be
	// closed portably: pinning the resolved *directory itself* would need
	// fd-path canonicalization (readlink on /proc/self/fd/N), which is
	// linux-only, and this binary builds for Windows too. It is tracked as a
	// follow-up rather than claimed closed.
	if pinErr := roots.pin(); pinErr != nil {
		// close what pin already opened so a failing scan leaks no root
		// descriptor; the pin error stays the primary one.
		return nil, errors.Join(fmt.Errorf("scan model directory: %w", pinErr), roots.close())
	}

	// the resolved --model-path directory's scan-time identity, taken through
	// the pinned root (its identity is loaded now, not lazily at the first
	// os.SameFile call): the post-upload delete re-checks it before removing
	// anything, so a real-directory replacement of --model-path after the scan
	// cannot pass as the scanned directory.
	_, deleteRootInfo, err := roots.open(0)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("stat model directory: %w", err), roots.close())
	}

	var files []modelFile

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if path == root {
			return nil
		}

		if d.IsDir() {
			return nil
		}

		resolved := path
		var linkInfo os.FileInfo
		if d.Type()&fs.ModeSymlink != 0 {
			target, err := filepath.EvalSymlinks(path)
			if err != nil {
				return fmt.Errorf("resolve symlink %q: %w", path, err)
			}
			resolved = target
			// keep the link's own lstat (identity-pinned like info): the delete
			// removes the link by name and must only remove the very link that
			// was scanned, not a same-named replacement.
			linkInfo, err = d.Info()
			if err != nil {
				return fmt.Errorf("stat symlink %q: %w", path, err)
			}
			if pinErr := pinScanFileIdentity(linkInfo); pinErr != nil {
				return fmt.Errorf("scan symlink %q: %w", path, pinErr)
			}
		}
		rootIdx, rootRel, within := roots.locate(resolved)
		if !within {
			return fmt.Errorf("%w: %q resolves to %q, outside the model directory (--model-path); refusing to upload files that are not under --model-path. %s", errSymlinkOutsideModelPath, path, resolved, symlinkOutsideModelPathHint)
		}

		if afterScanEntryHook != nil {
			afterScanEntryHook(path)
		}

		// stat through the root, not the path: a directory swapped for a symlink
		// after the walk read this entry is refused by the root instead of being
		// followed to a file outside it.
		handle, rootInfo, err := roots.open(rootIdx)
		if err != nil {
			return fmt.Errorf("stat file %q: %w", path, err)
		}
		info, err := handle.Lstat(rootRel)
		if err != nil {
			return fmt.Errorf("stat file %q: %w", path, err)
		}

		if info.IsDir() {
			return fmt.Errorf("encountered directory %q while collecting files", path)
		}

		if !info.Mode().IsRegular() {
			return fmt.Errorf("model-path contains unsupported file type: %s", path)
		}

		if pinErr := pinScanFileIdentity(info); pinErr != nil {
			return fmt.Errorf("scan %q: %w", path, pinErr)
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		files = append(files, modelFile{
			AbsolutePath:   path,
			ResolvedPath:   resolved,
			RelativePath:   filepath.ToSlash(rel),
			Size:           info.Size(),
			info:           &scannedModelInfo{FileInfo: info, rootDir: roots.dirs[rootIdx], rootInfo: rootInfo, name: rootRel},
			linkInfo:       linkInfo,
			deleteRootDir:  root,
			deleteRootInfo: deleteRootInfo,
			deleteName:     rel,
		})

		return nil
	})

	if closeErr := roots.close(); walkErr == nil && closeErr != nil {
		walkErr = fmt.Errorf("close model directory: %w", closeErr)
	}
	if walkErr != nil {
		return nil, fmt.Errorf("scan model directory: %w", walkErr)
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].RelativePath < files[j].RelativePath
	})

	return files, nil
}

// modelContainmentRoots returns the directories a resolved file may live in:
// the (already resolved) model directory itself and, when it sits at or below
// a Hugging Face cache snapshot's revision directory
// (.../models--owner--name/snapshots/<rev>[/sub/dir]), that repo's blobs/
// directory and nothing else of the repo. HF caches store every snapshot file
// as a relative symlink into the repo's blobs/ directory
// (snapshots/<rev>/file -> ../../blobs/<sha>), so a single root would refuse
// the whole layout; a subfolder of a snapshot (say snapshots/<rev>/unet, how
// a diffusers component would be uploaded) stores its files as links into the
// same blobs/, so the nearest snapshots/<rev> ancestor wins the allowance,
// not only the revision directory itself. refs/ and the other revisions'
// snapshots/ stay outside the allowance: a link into them would upload
// content that is neither in the selected snapshot nor a blob.
func modelContainmentRoots(resolvedRoot string) []string {
	roots := []string{resolvedRoot}
	for dir := resolvedRoot; ; dir = filepath.Dir(dir) {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		if filepath.Base(parent) != "snapshots" {
			continue
		}
		repo := filepath.Dir(parent)
		if strings.HasPrefix(filepath.Base(repo), "models--") {
			roots = append(roots, filepath.Join(repo, "blobs"))
		}
		break
	}
	return roots
}

// relWithin returns path relative to root, and whether path is at or below it.
func relWithin(root, path string) (string, bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// betweenLstatAndOpenHook, when set, runs in openScannedModelFile after the
// lstat check and before the open. Tests use it to swap the file in exactly that
// window; it is nil in production.
var betweenLstatAndOpenHook func(path string)

// openScannedModelFile opens path for upload and verifies it is still the
// regular file seen at scan time. A path that was swapped for a symlink (or
// another file) after the scan is refused instead of followed. The lstat is
// only an early, readable refusal: it cannot make the open safe, because the
// file can be replaced between the two calls. The open itself therefore uses
// O_NONBLOCK (a FIFO swapped in cannot park it waiting for a writer, before any
// stall guard exists), and the fstat of the opened descriptor is authoritative:
// it describes the file actually opened and must be a regular file with the scan
// identity. scanned may be nil (callers outside the directory scan), in which
// case only the regular-file checks apply. When scanned carries the scan-time
// root (see scannedModelInfo) the file is opened beneath that root instead of by
// path.
func openScannedModelFile(path string, scanned os.FileInfo) (*os.File, os.FileInfo, error) {
	var (
		root     *os.Root
		openName = path
		identity = scanned
	)
	if rooted, ok := scanned.(*scannedModelInfo); ok {
		// open beneath the scan-time root: the root itself must still be the same
		// directory, and every component below it is resolved by the root, which
		// refuses a symlink that leads out of it.
		var err error
		root, err = os.OpenRoot(rooted.rootDir)
		if err != nil {
			return nil, nil, fmt.Errorf("open artifact: %w", err)
		}
		defer root.Close() //nolint:errcheck // read-only directory handle; the opened file stays valid and a failed close leaves nothing to recover
		rootInfo, err := root.Stat(".")
		if err != nil {
			return nil, nil, fmt.Errorf("open artifact: %w", err)
		}
		if !os.SameFile(rooted.rootInfo, rootInfo) {
			return nil, nil, fmt.Errorf("refusing to upload %q: its directory changed since the model directory was scanned", path)
		}
		openName = rooted.name
		identity = rooted.FileInfo
	}
	lstat := os.Lstat
	// a path open also refuses a symlink at the final component; a root open
	// is confined by the root and keeps following links that stay inside it.
	open := func(name string) (*os.File, error) {
		return os.OpenFile(name, os.O_RDONLY|openNonblockFlag|openNoFollowFlag, 0) //nolint:gosec // G304: the path is the scanned model file, checked by lstat before and by fstat after
	}
	if root != nil {
		lstat = root.Lstat
		open = func(name string) (*os.File, error) {
			return root.OpenFile(name, os.O_RDONLY|openNonblockFlag, 0)
		}
	}

	pre, err := lstat(openName)
	if err != nil {
		return nil, nil, fmt.Errorf("open artifact: %w", err)
	}
	if !pre.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("refusing to upload %q: it is no longer a regular file (changed since the model directory was scanned)", path)
	}

	if betweenLstatAndOpenHook != nil {
		betweenLstatAndOpenHook(path)
	}
	file, err := open(openName)
	if err != nil {
		return nil, nil, fmt.Errorf("open artifact: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		return nil, nil, errors.Join(fmt.Errorf("stat artifact: %w", err), file.Close())
	}
	if !info.Mode().IsRegular() {
		return nil, nil, errors.Join(fmt.Errorf("refusing to upload %q: it is not a regular file", path), file.Close())
	}
	if identity != nil && !os.SameFile(identity, info) {
		return nil, nil, errors.Join(fmt.Errorf("refusing to upload %q: the file changed since the model directory was scanned", path), file.Close())
	}
	return file, info, nil
}

func totalModelFileSize(files []modelFile) int64 {
	var total int64
	for i := range files {
		total += files[i].Size
	}
	return total
}

// modelUploadRequestedBytes reports the total bytes an upload is about to send. ok is false
// when that can't be determined locally (e.g. an unparseable --file-size), in which case the
// caller must skip the quota pre-check rather than guess.
func modelUploadRequestedBytes(modelFiles []modelFile) (bytes int64, ok bool) {
	if len(modelFiles) > 0 {
		return totalModelFileSize(modelFiles), true
	}

	size, err := strconv.ParseInt(strings.TrimSpace(addModelFileSize), 10, 64)
	if err != nil {
		return 0, false
	}
	return size, true
}

// deleteVerifiedModelFiles removes the local --model-path files backing
// --delete-my-model-files-after-upload. Callers must only invoke this after
// every file in `files` has both (a) completed its multipart upload
// (uploadModelFiles returned without error) and (b) been confirmed deployable
// server-side (waitForUploadedModelHash returned without error). This
// function does not itself re-check either condition. It never
// deletes anything else in --model-path: only the exact entries that were
// part of this run's verified upload, never a directory-wide wipe.
//
// Every removal goes through an *os.Root (Root.Remove) opened on the scanned
// --model-path directory, after re-checking that directory's identity against
// the scan: the upload and the server-side hash take as long as the network
// and the server do, and a directory -- or --model-path itself -- swapped for
// a symlink during that window must not redirect a removal outside
// --model-path.
//
// Before each entry is removed its identity is compared against the scan (the
// link's own lstat for a symlink, the target's for a regular file). An entry
// that was replaced since the scan -- renamed over, or reached through a
// same-named replaced directory -- fails the check and is reported as a
// failure instead of being deleted: only the bytes that were uploaded may be
// removed, so a same-named file that was never part of this run survives. The
// name can still change between that check and the removal itself; the window
// is the few instructions between them rather than the whole upload.
func deleteVerifiedModelFiles(files []modelFile) (deletedCount int, deletedBytes int64, removedSymlinks int, err error) {
	if len(files) == 0 {
		return 0, 0, 0, nil
	}

	root, err := os.OpenRoot(files[0].deleteRootDir)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("upload and hash are confirmed, but the local model files were not deleted: open model directory: %w", err)
	}
	defer root.Close() //nolint:errcheck // directory handle; the removals above it already happened or failed, and a failed close leaves nothing to recover
	rootInfo, err := root.Stat(".")
	if err != nil {
		return 0, 0, 0, fmt.Errorf("upload and hash are confirmed, but the local model files were not deleted: stat model directory: %w", err)
	}
	if !os.SameFile(files[0].deleteRootInfo, rootInfo) {
		return 0, 0, 0, fmt.Errorf("upload and hash are confirmed, but refusing to delete the local model files from %s: the directory changed since it was scanned", addModelDirectoryPath)
	}

	var failures []string
	// regular entries deleted in this run, and the distinct resolved targets of
	// removed links: an in-root link's target is itself a collected entry and
	// is deleted too, so "targets kept" only holds for distinct link targets
	// that survived -- two aliases to one surviving blob are one kept target.
	deleted := make(map[string]bool)
	keptTargets := make(map[string]bool)

	for i := range files {
		file := &files[i]
		if refuseErr := refuseDeleteOfReplacedEntry(root, file); refuseErr != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", file.RelativePath, refuseErr))
			continue
		}
		if removeErr := removeModelFileInRoot(root, file.deleteName); removeErr != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", file.RelativePath, removeErr))
			continue
		}
		// os.Remove on a symlink removes the link, not the target, so a link frees
		// no bytes: count it separately instead of adding the target's size.
		if file.isSymlink() {
			removedSymlinks++
			if !deleted[file.ResolvedPath] {
				keptTargets[file.ResolvedPath] = true
			}
			continue
		}
		deleted[file.AbsolutePath] = true
		delete(keptTargets, file.AbsolutePath)
		deletedCount++
		deletedBytes += file.Size
	}

	if deletedCount > 0 || removedSymlinks > 0 {
		msg := fmt.Sprintf("deleted %d verified model file(s) (%d bytes) from %s", deletedCount, deletedBytes, addModelDirectoryPath)
		if removedSymlinks > 0 {
			msg += fmt.Sprintf(" and removed %d symlink(s)", removedSymlinks)
			if kept := len(keptTargets); kept == removedSymlinks {
				msg += " (targets kept)"
			} else if kept > 0 {
				msg += fmt.Sprintf(" (%d target(s) kept)", kept)
			}
		}
		fmt.Fprintln(os.Stderr, msg)
	}

	if len(failures) > 0 {
		return deletedCount, deletedBytes, removedSymlinks, fmt.Errorf("upload and hash are confirmed, but failed to delete %d of %d model file(s) from %s: %s", len(failures), len(files), addModelDirectoryPath, strings.Join(failures, "; "))
	}

	return deletedCount, deletedBytes, removedSymlinks, nil
}

// refuseDeleteOfReplacedEntry reports why file must not be deleted: the entry
// at its scanned name is no longer the entry that was scanned, uploaded and
// hashed. Returns nil when they are the same. Comparisons go through the
// pinned --model-path root, so the lookup itself cannot escape it. file is a
// pointer into the caller's slice: per-entry copies of a modelFile are wasted.
//
// For a regular file the inode alone is not enough: a truncate-and-rewrite in
// place keeps it, so the size and mtime must match too (see inside). A symlink
// entry compares only the link's own identity; its target's bytes are never
// deleted through the link.
func refuseDeleteOfReplacedEntry(root *os.Root, file *modelFile) error {
	// a symlink entry compares against the link's own scan-time lstat (the
	// delete removes the link), a regular entry against the scanned target.
	scanned := file.info
	scannedIsLink := false
	if file.isSymlink() {
		scanned = file.linkInfo
		scannedIsLink = true
	}
	if scanned == nil {
		// files constructed without a scan (tests, non-model-path flows) have no
		// identity to compare; collectModelFiles always records one.
		return nil
	}
	// os.SameFile needs the raw stat: scannedModelInfo wraps it with the
	// scan-time location, and comparing the wrapper itself is never equal.
	if wrapped, ok := scanned.(*scannedModelInfo); ok {
		scanned = wrapped.FileInfo
	}
	current, err := root.Lstat(file.deleteName)
	if err != nil {
		return fmt.Errorf("it is no longer present as scanned: %w", err)
	}
	if isLink := current.Mode()&os.ModeSymlink != 0; isLink != scannedIsLink {
		return errors.New("it changed since the model directory was scanned")
	}
	if !os.SameFile(scanned, current) {
		return errors.New("it changed since the model directory was scanned")
	}
	if !scannedIsLink {
		// SameFile compares device+inode: a truncate-and-rewrite in place (how
		// torch.save and safetensors save_file write a checkpoint) keeps the
		// inode, so a same-path rewrite after the scan would pass that check
		// and delete bytes that were never uploaded. The size and mtime must
		// also match. Residual: a writer that rewrites the file AND restores
		// its size and mtime is indistinguishable and passes.
		if current.Size() != scanned.Size() || !current.ModTime().Equal(scanned.ModTime()) {
			return errors.New("its contents changed since the model directory was scanned")
		}
	}
	return nil
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

	fmt.Fprint(os.Stderr, "waiting for model to be deployable")
	defer fmt.Fprintln(os.Stderr)

	lastStatus := ""
	for {
		if err := ctx.Err(); err != nil {
			return nil, modelVersionWaitTimeoutError(name, lastStatus, err)
		}

		models, err := getModelsForAdd(&api.GetModelsInput{Name: name})
		if err != nil {
			return nil, fmt.Errorf("get model hash: %w", err)
		}

		model := findUploadedModel(models, owner, name, uploadedModel)
		state := uploadedModelVersionState(model, modelVersionUUID)
		if state.status != "" {
			lastStatus = state.status
		}

		if state.terminal {
			return nil, modelVersionTerminalError(name, state.status)
		}

		if state.hash != "" {
			ownerID := modelOwnerForURL(owner, uploadedModel, model)
			if ownerID == "" {
				return nil, fmt.Errorf("model owner is required to build model url")
			}
			modelName := modelNameForURL(name, uploadedModel, model)
			if modelName == "" {
				return nil, fmt.Errorf("model name is required to build model url")
			}

			modelURL := formatModelURL(ownerID, modelName, state.hash)
			return &modelReadyOutput{
				Owner:     ownerID,
				Name:      modelName,
				ModelHash: state.hash,
				ModelURL:  modelURL,
			}, nil
		}

		fmt.Fprint(os.Stderr, ".")
		if err := sleepModelHashPoll(ctx, pollInterval); err != nil {
			return nil, modelVersionWaitTimeoutError(name, lastStatus, err)
		}
	}
}

// modelVersionWaitTimeoutError reports a --wait-for-hash budget that ran out
// with the version still being processed server-side. It names the last status
// seen and the command to poll with, because the upload itself succeeded and a
// re-upload would create a second version of the same bytes.
func modelVersionWaitTimeoutError(name, lastStatus string, cause error) error {
	if lastStatus == "" {
		lastStatus = "unknown"
	}
	return internalapi.NewTimeoutError(
		"upload completed but timed out waiting for the model version to become deployable (last version status: %s); the model exists, do not re-upload, poll it with: runpodctl model list --name %s: %v",
		strings.ToLower(lastStatus), name, cause,
	)
}

// modelVersionTerminalError reports a version that will never become
// deployable, so the wait ends immediately instead of burning the whole
// --hash-timeout budget and reporting `timeout` (which tells an agent the work
// is still running). Coded `conflict`, matching the pod wait's treatment of a
// terminal status: nothing the caller typed was wrong, the version is in a
// state no amount of waiting fixes.
func modelVersionTerminalError(name, status string) error {
	return &internalapi.APIError{
		Message: fmt.Sprintf(
			"model version for %s is %s, so it will never become deployable; the uploaded bytes are not usable, re-run the upload",
			name, strings.ToLower(status),
		),
		Code: "conflict",
	}
}

// modelVersionState is what a single poll of the uploaded version could tell
// us. A zero value means "keep waiting": not found yet, or found and still
// being processed.
type modelVersionState struct {
	// status is the version status the poll saw, "" when the version is not
	// listed yet. Reported in the timeout message so a caller can tell "still
	// hashing" from "stuck".
	status string
	// hash is the deployable version hash, set only once the version is
	// deployable. Never a placeholder.
	hash string
	// terminal marks a version that will never become deployable.
	terminal bool
}

// uploadedModelVersionState reads the readiness of the version this upload
// created. A nonempty hash alone does not mean ready: the api assigns a
// `ph-`-prefixed placeholder hash at creation, while the version is still
// NEEDS_HASH and would be rejected at deploy time.
//
// The version's own status is not the whole answer either. When the hashed
// bytes collide with an existing version, the api deprecates this uuid and
// points it at the canonical version that already holds the content, so a
// successful upload's uuid stays DEPRECATED with its placeholder hash forever.
// That pointer is the deployable result, not a failure.
func uploadedModelVersionState(model *api.Model, modelVersionUUID string) modelVersionState {
	if model == nil {
		return modelVersionState{}
	}
	modelVersionUUID = strings.TrimSpace(modelVersionUUID)
	if modelVersionUUID == "" {
		return modelVersionState{}
	}

	for _, version := range model.Versions {
		if version == nil || strings.TrimSpace(version.UUID) != modelVersionUUID {
			continue
		}

		status := strings.ToUpper(strings.TrimSpace(version.Status))
		if canonicalHash := dedupedCanonicalVersionHash(version); canonicalHash != "" {
			return modelVersionState{status: status, hash: canonicalHash}
		}
		if terminalModelVersionStatuses[status] {
			return modelVersionState{status: status, terminal: true}
		}
		if !deployableModelVersionStatuses[status] {
			return modelVersionState{status: status}
		}

		hash := strings.TrimSpace(version.Hash)
		if hash == "" || isPlaceholderModelVersionHash(hash) {
			return modelVersionState{status: status}
		}
		return modelVersionState{status: status, hash: hash}
	}

	return modelVersionState{}
}

func isPlaceholderModelVersionHash(hash string) bool {
	return strings.HasPrefix(strings.TrimSpace(hash), placeholderModelVersionHashPrefix)
}

// dedupedCanonicalVersionHash returns the hash of the canonical version this
// one was deduped into, or "" when it was not deduped.
func dedupedCanonicalVersionHash(version *api.ModelVersion) string {
	if version == nil {
		return ""
	}
	deduped, ok := version.Metadata[dedupedCanonicalVersionMetadataKey].(map[string]interface{})
	if !ok {
		return ""
	}
	hash, ok := deduped["hash"].(string)
	if !ok {
		return ""
	}
	if hash = strings.TrimSpace(hash); hash == "" || isPlaceholderModelVersionHash(hash) {
		return ""
	}
	return hash
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

// uploadModelFiles creates upload sessions for every file in files, modelRepoUploadBatchSize at
// a time, then uploads their bytes. The first batch's response establishes modelVersionUUID and
// every later batch is pinned to it, so a chunked manifest still lands on one model version.
func uploadModelFiles(files []modelFile, baseInput *api.CreateModelRepoUploadInput) ([]uploadedModelFile, *api.Model, string, error) {
	var modelVersionUUID string
	var uploadModel *api.Model
	uploadedFiles := make([]uploadedModelFile, 0, len(files))
	totalSize := totalModelFileSize(files)
	progress := newModelUploadProgress(totalSize)

	fail := func(err error) ([]uploadedModelFile, *api.Model, string, error) {
		if progress != nil {
			_ = progress.Clear()
		}
		return nil, nil, "", err
	}

	for start := 0; start < len(files); start += modelRepoUploadBatchSize {
		end := start + modelRepoUploadBatchSize
		if end > len(files) {
			end = len(files)
		}
		chunk := files[start:end]

		batchInput := api.CreateModelRepoUploadBatchInput{
			Owner:               baseInput.Owner,
			Name:                baseInput.Name,
			ModelVersionUUID:    modelVersionUUID,
			Metadata:            baseInput.Metadata,
			CredentialType:      baseInput.CredentialType,
			CredentialReference: baseInput.CredentialReference,
			Files:               make([]api.ModelRepoUploadBatchFileInput, len(chunk)),
		}
		for i := range chunk {
			file := &chunk[i]
			batchInput.Files[i] = api.ModelRepoUploadBatchFileInput{
				FileName:      file.RelativePath,
				FileSizeBytes: strconv.FormatInt(file.Size, 10),
				PartSizeBytes: baseInput.PartSizeBytes,
				ContentType:   baseInput.ContentType,
			}
		}

		result, err := createModelRepoUploadBatch(&batchInput)
		if err != nil {
			return fail(fmt.Errorf("create upload batch for files %d-%d: %w", start, end-1, err))
		}
		if result.Model != nil {
			uploadModel = result.Model
		}
		if modelVersionUUID == "" {
			if result.Version != nil {
				modelVersionUUID = strings.TrimSpace(result.Version.UUID)
			}
			// Only the final chunk may legitimately omit a version uuid (e.g. a
			// degenerate one-batch response shape); any earlier chunk without one
			// leaves later batches with nothing to pin to, so fail fast instead of
			// silently splitting the manifest across versions.
			if modelVersionUUID == "" && end < len(files) {
				return fail(fmt.Errorf("upload batch response missing model version uuid"))
			}
		}

		chunkUploaded, err := uploadModelFileChunk(chunk, result.Uploads, progress)
		if err != nil {
			return fail(err)
		}
		uploadedFiles = append(uploadedFiles, chunkUploaded...)
	}

	// Finalize every uploaded session in one request: build the session id list as it
	// exists now (every chunk has already finished uploading its bytes) and send it as
	// a single completeModelRepoUploadBatch call, rather than one completeModelRepoUpload
	// round trip per file.
	if len(uploadedFiles) > 0 {
		sessionIDs := make([]string, len(uploadedFiles))
		for i := range uploadedFiles {
			sessionIDs[i] = uploadedFiles[i].SessionID
		}
		completions, err := completeModelRepoUploadAll(sessionIDs)
		if err != nil {
			return fail(fmt.Errorf("complete upload sessions: %w", err))
		}
		if len(completions) != len(uploadedFiles) {
			return fail(fmt.Errorf("expected %d completed sessions, got %d", len(uploadedFiles), len(completions)))
		}
		for i, completion := range completions {
			uploadedFiles[i].SessionID = completion.SessionID
			uploadedFiles[i].Status = completion.Status
		}
	}

	if progress != nil {
		_ = progress.Finish()
	}
	printCompletedModelUploadSize(totalSize)

	return uploadedFiles, uploadModel, modelVersionUUID, nil
}

// runBounded runs fn(ctx, i) for i in [0, n) with at most concurrency goroutines in
// flight at once. The first failure cancels ctx, so no further fn is started and
// in-flight calls that honor ctx stop early; one bad part or file must not let the
// rest of a multi-GB upload go out first. It always waits for every launched call
// to return, then reports the error that first triggered cancellation.
func runBounded(parent context.Context, n, concurrency int, fn func(ctx context.Context, i int) error) error {
	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i := range n {
		sem <- struct{}{}
		if ctx.Err() != nil {
			<-sem
			break
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := fn(ctx, i); err != nil {
				cancel(err)
			}
		}(i)
	}
	wg.Wait()

	return context.Cause(ctx)
}

// uploadModelFileChunk uploads every file in chunk to S3, with up to
// modelRepoUploadConcurrency transfers in flight at once (see runBounded). uploads[i]
// is paired with chunk[i] by slice position: the server builds sessions from the same
// ordered file list it was sent (see createModelRepoUploadBatch in
// RunPod/node/graphql/schema/modelRepo.ts, which maps fileWork/sessions/uploads via a
// single shared index throughout), and ModelRepoUpload carries no fileName to
// re-correlate by. The count check in api.CreateModelRepoUploadBatch only guards
// length, not ordering.
//
// Session validity (upload != nil, upload.SessionID != "") is checked synchronously
// for the whole chunk before any goroutine is launched, so a malformed response fails
// before wasting a single byte transfer.
func uploadModelFileChunk(chunk []modelFile, uploads []*api.ModelRepoUpload, progress modelUploadProgress) ([]uploadedModelFile, error) {
	for i := range chunk {
		file := &chunk[i]
		upload := uploads[i]
		if upload == nil {
			return nil, fmt.Errorf("upload batch response missing upload session details for %s", file.RelativePath)
		}
		if upload.SessionID == "" {
			return nil, fmt.Errorf("upload %s: missing session identifier for completion", file.RelativePath)
		}
	}

	results := make([]uploadedModelFile, len(chunk))
	err := runBounded(context.Background(), len(chunk), modelRepoUploadConcurrency, func(ctx context.Context, i int) error {
		file := chunk[i]
		upload := uploads[i]
		if err := completeModelUploadFile(ctx, upload, file.uploadPath(), file.info, progress); err != nil {
			return fmt.Errorf("upload %s: %w", file.RelativePath, err)
		}
		results[i] = uploadedModelFile{
			RelativePath: file.RelativePath,
			Key:          upload.Key,
			SessionID:    upload.SessionID,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
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

const modelUploadMaxAttempts = 3

// modelUploadRetryBackoff is the base delay for the exponential backoff between
// retries of a transient S3 request. It is a var so tests can shorten it.
var modelUploadRetryBackoff = 500 * time.Millisecond

// modelUploadStallTimeout bounds how long a request body write or response
// body read may go without moving a byte before the attempt is aborted. It is
// a var so tests can shorten it.
var modelUploadStallTimeout = 60 * time.Second

// modelUploadMaxRetryAfter caps a server-requested Retry-After wait so a
// misbehaving or hostile endpoint cannot park the upload for hours.
const modelUploadMaxRetryAfter = 30 * time.Second

// modelUploadErrorBodyLimit caps how much of an error response body is read
// into the returned error message.
const modelUploadErrorBodyLimit = 4096

// modelUploadHTTPClient talks straight to object storage for the part/complete
// requests (not the control plane), so it gets its own transport. The
// ResponseHeaderTimeout guards a stalled server once the body has been sent
// without capping a legitimately long large-part upload; http.DefaultClient
// (used before) had no timeouts at all, so a hung connection blocked forever.
var modelUploadHTTPClient = &http.Client{
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   modelRepoUploadConcurrency * modelPartUploadConcurrency,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 5 * time.Minute,
	},
}

// isRetryableUploadStatus reports whether an HTTP status is a transient
// server-side condition worth retrying, as opposed to a 4xx the caller must fix.
func isRetryableUploadStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= http.StatusInternalServerError
}

// stallGuard cancels an upload attempt when its request or response body goes
// modelUploadStallTimeout without moving a byte. ResponseHeaderTimeout covers
// the wait for headers; this covers the two phases it does not: a server that
// stops consuming the request body, and one that sends headers then stalls the
// response body. Slow transfers that keep making progress are never cut off.
//
// The transport reads the request body from its own goroutine and can make
// the final EOF read after the response headers arrived, so once the response
// phase starts, request body events are ignored; otherwise that late EOF
// would stop the timer guarding the response body.
type stallGuard struct {
	mu         sync.Mutex
	timer      *time.Timer
	cancel     context.CancelFunc
	responding bool
}

func newStallGuard(cancel context.CancelFunc) *stallGuard {
	return &stallGuard{timer: time.AfterFunc(modelUploadStallTimeout, cancel), cancel: cancel}
}

// startResponse switches the guard to the response body phase and arms it.
func (g *stallGuard) startResponse() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.responding = true
	g.timer.Reset(modelUploadStallTimeout)
}

// progress re-arms the guard after a body read that moved bytes.
func (g *stallGuard) progress(fromResponse bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if fromResponse == g.responding {
		g.timer.Reset(modelUploadStallTimeout)
	}
}

// idle stops the guard once a body is fully read.
func (g *stallGuard) idle(fromResponse bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if fromResponse == g.responding {
		g.timer.Stop()
	}
}

func (g *stallGuard) disarm() { g.timer.Stop() }

// stallReader re-arms the guard on every read that moves bytes. On EOF it
// disarms, so the guard is idle while waiting for response headers.
// response is set only for response bodies: it marks their phase and cancels
// the attempt on Close. The transport closes the request body itself once
// written, which must not end the attempt.
type stallReader struct {
	r        io.ReadCloser
	guard    *stallGuard
	response bool
}

func (s *stallReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 {
		s.guard.progress(s.response)
	}
	if err == io.EOF {
		s.guard.idle(s.response)
	}
	return n, err
}

func (s *stallReader) Close() error {
	err := s.r.Close()
	if s.response {
		s.guard.disarm()
		s.guard.cancel()
	}
	return err
}

// doModelUploadRequest issues the request built by newReq and retries transient
// failures (network errors, stalls, 429, 5xx) with exponential backoff. newReq
// is called once per attempt so the request body can be re-read on retry. On
// success or a non-retryable status it returns the response for the caller to
// validate and close; its body stays stall-guarded. If every attempt fails
// transiently it returns the last error. Once ctx is done (another part failed)
// it starts no further attempt and stops waiting out a backoff. newReq must
// build its request with ctx so an in-flight attempt is aborted too.
func doModelUploadRequest(ctx context.Context, what string, newReq func() (*http.Request, error)) (*http.Response, error) {
	var lastErr error
	for attempt := 1; attempt <= modelUploadMaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("%s: %w", what, errors.Join(err, lastErr))
		}
		resp, err := doGuardedModelUploadAttempt(ctx, newReq)
		retryAfter := ""
		switch {
		case err != nil:
			lastErr = fmt.Errorf("%s: %w", what, err)
		case isRetryableUploadStatus(resp.StatusCode):
			retryAfter = resp.Header.Get("Retry-After")
			lastErr = fmt.Errorf("%s failed: status %d: %s", what, resp.StatusCode, modelUploadErrorSnippet(resp.Body))
			if closeErr := resp.Body.Close(); closeErr != nil {
				lastErr = fmt.Errorf("%w (close response body: %w)", lastErr, closeErr)
			}
		default:
			return resp, nil
		}

		if attempt < modelUploadMaxAttempts {
			sleepCtx(ctx, modelUploadRetryDelay(attempt, retryAfter, time.Now()))
		}
	}
	return nil, lastErr
}

// sleepCtx waits for d or until ctx is done, whichever comes first.
func sleepCtx(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

// modelUploadErrorSnippet reads at most modelUploadErrorBodyLimit bytes of an
// error response body and returns a short, redacted summary for the returned
// error message. The body is stall guarded, so a server that stops sending
// mid-body cannot block this. A read failure is reported inline since the
// status code is the actionable part.
//
// Storage error bodies echo the request (S3 includes the signature, access key
// id and string-to-sign), so the raw body is never returned: an S3-style error
// is reduced to its <Code> and <Message>, anything else to a bounded excerpt,
// and both go through redact.Text.
//
// The read limit cuts the body before any redaction, so a configured key can
// reach redact.Text half-written at the end of the text; redact.Text masks such
// a trailing prefix of the key, which keeps it from surviving the later excerpt
// bound when a shortened URL pulls it into range.
func modelUploadErrorSnippet(body io.Reader) string {
	snippet, err := io.ReadAll(io.LimitReader(body, modelUploadErrorBodyLimit))
	text := summarizeStorageError(string(snippet))
	if err != nil {
		return fmt.Sprintf("%s (reading error body: %v)", text, redact.Error(err))
	}
	return text
}

// modelUploadErrorExcerptRunes bounds each field of the error summary.
const modelUploadErrorExcerptRunes = 256

// unrecognizedXMLErrorText stands in for an XML body that has no <Code> or
// <Message>: such a body is never echoed, because storage error documents carry
// the signature, access key id and string-to-sign in arbitrary other elements.
const unrecognizedXMLErrorText = "unrecognized XML error response"

// storageXMLMarkerPattern spots an S3-style error document buried in a body
// that does not itself lead with markup (a proxy prepending plain text, or an
// error document embedded in JSON), so a wrapper cannot hide one from the
// structured path (which echoes only the allowlisted <Code> and <Message>
// fields, never arbitrary other elements).
var storageXMLMarkerPattern = regexp.MustCompile(`<\?xml[\s?]|<Error[\s>]`)

// markupTagPattern matches one markup tag, for reducing a proxy's HTML error
// page to its readable text.
var markupTagPattern = regexp.MustCompile(`<[^>]*>`)

// looksLikeHTML reports whether body reads as an HTML document (a proxy's
// 502/504 page), the only markup shape whose text is safe to echo. Case does
// not matter.
func looksLikeHTML(body string) bool {
	lower := strings.ToLower(body)
	for _, prefix := range []string{"<html", "<!doctype", "<head", "<body"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// redactedCodeMessage renders the allowlisted fields of a storage error
// document, each bounded and redacted.
func redactedCodeMessage(code, message string) string {
	switch {
	case code != "" && message != "":
		return boundedRedacted(code) + ": " + boundedRedacted(message)
	case code != "":
		return boundedRedacted(code)
	default:
		return boundedRedacted(message)
	}
}

func summarizeStorageError(body string) string {
	body = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(body), "\uFEFF"))
	if body == "" {
		return ""
	}
	if strings.HasPrefix(body, "<") {
		// any markup-leading body is a storage-style error document or nothing:
		// structurally parsed, only the allowlisted <Code>/<Message> text comes
		// out. Unrecognized markup is never echoed at all — unknown elements
		// can carry the access key id, signature and string-to-sign, and
		// redact.Text does not know them. A recognized HTML page (a proxy's
		// error document) alone keeps a text excerpt for diagnosis.
		if code, message := parseStorageErrorXML(body); code != "" || message != "" {
			return redactedCodeMessage(code, message)
		}
		if looksLikeHTML(body) {
			return boundedRedacted(stripMarkupTags(body))
		}
		return unrecognizedXMLErrorText
	}
	if storageXMLMarkerPattern.MatchString(body) {
		if code, message := parseStorageErrorXML(body); code != "" || message != "" {
			return redactedCodeMessage(code, message)
		}
		return unrecognizedXMLErrorText
	}
	// plain text or JSON (e.g. {"message":"expected <Part> elements"}): a
	// bounded redacted excerpt, verbatim so error text survives intact.
	return boundedRedacted(body)
}

// stripMarkupTags reduces an HTML error page to its text: tags are dropped
// and the remaining runs of whitespace collapse into single spaces.
func stripMarkupTags(s string) string {
	return strings.Join(strings.Fields(markupTagPattern.ReplaceAllString(s, " ")), " ")
}

// parseStorageErrorXML decodes body structurally and returns only the text of
// the first <Code> and <Message> elements, whatever their namespace or nesting;
// every other element and attribute is ignored. A body that stops being
// well-formed keeps whatever was decoded before the failure.
func parseStorageErrorXML(body string) (code, message string) {
	dec := xml.NewDecoder(strings.NewReader(body))
	for {
		tok, err := dec.Token()
		if err != nil {
			return code, message
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		var target *string
		switch start.Name.Local {
		case "Code":
			target = &code
		case "Message":
			target = &message
		default:
			continue
		}
		if *target != "" {
			continue
		}
		var text string
		if err := dec.DecodeElement(&text, &start); err != nil {
			return code, message
		}
		*target = strings.TrimSpace(text)
	}
}

// boundedRedacted redacts before truncating so a signature cut in half by the
// bound cannot slip past the patterns.
func boundedRedacted(s string) string {
	s = redact.Text(s)
	runes := []rune(s)
	if len(runes) > modelUploadErrorExcerptRunes {
		return string(runes[:modelUploadErrorExcerptRunes]) + "..."
	}
	return s
}

// modelUploadRetryDelay returns how long to wait before the attempt after
// `attempt`. A valid Retry-After (delta-seconds or HTTP-date, RFC 9110 10.2.3)
// wins over exponential backoff, capped at modelUploadMaxRetryAfter; an empty
// or unparseable value falls back to backoff.
func modelUploadRetryDelay(attempt int, retryAfter string, now time.Time) time.Duration {
	backoff := modelUploadRetryBackoff * time.Duration(1<<(attempt-1))
	retryAfter = strings.TrimSpace(retryAfter)
	if retryAfter == "" {
		return backoff
	}

	var wait time.Duration
	if secs, err := strconv.ParseInt(retryAfter, 10, 64); err == nil {
		if secs < 0 {
			return backoff
		}
		// compare in seconds first so a huge value cannot overflow Duration.
		if secs > int64(modelUploadMaxRetryAfter/time.Second) {
			return modelUploadMaxRetryAfter
		}
		wait = time.Duration(secs) * time.Second
	} else if at, err := http.ParseTime(retryAfter); err == nil {
		wait = max(at.Sub(now), 0)
	} else {
		return backoff
	}
	return min(wait, modelUploadMaxRetryAfter)
}

// doGuardedModelUploadAttempt runs one attempt under a stallGuard, as a child of
// parent. The returned response body re-arms the guard on read and cancels the
// attempt on Close.
func doGuardedModelUploadAttempt(parent context.Context, newReq func() (*http.Request, error)) (*http.Response, error) {
	req, err := newReq()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	guard := newStallGuard(cancel)
	req = req.WithContext(ctx)
	if req.Body != nil {
		req.Body = &stallReader{r: req.Body, guard: guard}
		// GetBody would hand the transport an unguarded copy on a redirect
		// or HTTP/2 replay; retries go through newReq instead.
		req.GetBody = nil
	}

	resp, err := modelUploadHTTPClient.Do(req)
	if err != nil {
		// read before cancel: afterwards ctx.Err() is always set. A done parent
		// means the whole upload was canceled, not that this attempt stalled.
		stalled := ctx.Err() != nil && parent.Err() == nil
		guard.disarm()
		cancel()
		if stalled {
			return nil, fmt.Errorf("no progress for %s: %w", modelUploadStallTimeout, redact.Error(err))
		}
		return nil, redact.Error(err)
	}
	guard.startResponse()
	resp.Body = &stallReader{r: resp.Body, guard: guard, response: true}
	return resp, nil
}

func completeModelUploadWithProgress(upload *api.ModelRepoUpload, artifactPath string, progress modelUploadProgress) error {
	return completeModelUploadWithContext(context.Background(), upload, artifactPath, nil, progress)
}

// completeModelUploadWithContext uploads artifactPath. scanned is the scan-time
// FileInfo of the artifact (nil when unknown); see openScannedModelFile. Every
// error it returns is redacted: the part and completion URLs are presigned, and
// their signatures must not reach error output.
func completeModelUploadWithContext(ctx context.Context, upload *api.ModelRepoUpload, artifactPath string, scanned os.FileInfo, progress modelUploadProgress) (retErr error) {
	defer func() { retErr = redact.Error(retErr) }()
	if upload == nil {
		return fmt.Errorf("upload details are required")
	}
	file, fileInfo, err := openScannedModelFile(artifactPath, scanned)
	if err != nil {
		return err
	}
	defer file.Close()

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

	// parts are contiguous and uniform (partSize each) except the last, so a
	// part's offset is a pure function of its position — no accumulation. This
	// lets the PUTs run in parallel: a model repo is commonly one large file, so
	// uploading its parts sequentially left the pipe mostly idle.
	completed := make([]completedPart, len(parts))
	uploadErr := runBounded(ctx, len(parts), modelPartUploadConcurrency, func(ctx context.Context, i int) error {
		part := parts[i]
		offset := int64(i) * partSize
		remaining := totalSize - offset
		if remaining <= 0 {
			return fmt.Errorf("no data remaining for part %d", part.PartNumber)
		}
		chunkSize := min(partSize, remaining)

		resp, err := doModelUploadRequest(ctx, fmt.Sprintf("upload part %d", part.PartNumber), func() (*http.Request, error) {
			// fresh SectionReader per attempt so a retry re-reads the chunk.
			// os.File.ReadAt (used by SectionReader) is safe for concurrent use.
			req, reqErr := http.NewRequestWithContext(ctx, http.MethodPut, part.URL, io.NewSectionReader(file, offset, chunkSize))
			if reqErr != nil {
				return nil, fmt.Errorf("create request for part %d: %w", part.PartNumber, reqErr)
			}
			req.ContentLength = chunkSize
			return req, nil
		})
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		// a non-retryable status reaches here (2xx ok, 4xx errors out).
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return fmt.Errorf("upload part %d failed: status %d: %s", part.PartNumber, resp.StatusCode, modelUploadErrorSnippet(resp.Body))
		}
		etag := strings.Trim(resp.Header.Get("ETag"), "\"")
		if etag == "" {
			return fmt.Errorf("upload part %d missing ETag", part.PartNumber)
		}
		completed[i] = completedPart{PartNumber: part.PartNumber, ETag: fmt.Sprintf("%q", etag)}

		// account for the part once it succeeds, so a retried part is not
		// double-counted (coarser than per-byte, but correct under retry).
		if progress != nil {
			_ = progress.Add64(chunkSize) //nolint:errcheck // progress is cosmetic; a failed redraw must not fail the upload
		}
		return nil
	})
	if uploadErr != nil {
		return uploadErr
	}

	// verify the parts fully covered the file. The last part carries the
	// remainder; if the plan stopped short the artifact was under-uploaded.
	lastOffset := int64(len(parts)-1) * partSize
	if covered := lastOffset + min(partSize, totalSize-lastOffset); covered != totalSize {
		return fmt.Errorf("uploaded %d bytes but artifact size is %d bytes", covered, totalSize)
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

	resp, err := doModelUploadRequest(ctx, "complete upload", func() (*http.Request, error) {
		completeReq, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, upload.CompleteURL, bytes.NewReader(payload))
		if reqErr != nil {
			return nil, fmt.Errorf("create completion request: %w", reqErr)
		}
		completeReq.Header.Set("Content-Type", "application/xml")
		return completeReq, nil
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("completion request failed: status %d: %s", resp.StatusCode, modelUploadErrorSnippet(resp.Body))
	}

	return nil
}
