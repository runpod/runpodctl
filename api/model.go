package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	internalapi "github.com/runpod/runpodctl/internal/api"
)

// ErrModelRepoNotImplemented is retained for backwards compatibility with callers that
// handled the previous unimplemented model repository helpers.
var ErrModelRepoNotImplemented = errors.New("model repository functionality not yet implemented")

// modelRepoHTTPError wraps a non-200 graphql http response (e.g. Model Repo
// down behind the api gateway) as a typed GraphQLError, giving it a stable
// "graphql_error" code and preserving the http status.
func modelRepoHTTPError(status int, body []byte) error {
	return &internalapi.GraphQLError{
		Message: fmt.Sprintf("statuscode %d: %s", status, string(body)),
		Status:  status,
	}
}

// modelRepoGraphQLError wraps a graphql top-level error (how Model Repo
// access failures surface: the resolver throws before returning data) as a
// typed GraphQLError, giving it a stable "graphql_error" code instead of the
// generic cli_error fallback.
func modelRepoGraphQLError(gqlErr *GraphQLError) error {
	return &internalapi.GraphQLError{Message: gqlErr.Message}
}

// Model represents a model stored in the RunPod model repository.
type Model struct {
	ID        string          `json:"id"`
	Provider  string          `json:"provider"`
	Name      string          `json:"name"`
	Owner     string          `json:"owner,omitempty"`
	Status    string          `json:"status,omitempty"`
	CreatedAt string          `json:"createdAt,omitempty"`
	UpdatedAt string          `json:"updatedAt,omitempty"`
	Versions  []*ModelVersion `json:"versions,omitempty"`
	Users     []*ModelUser    `json:"users,omitempty"`
}

// ModelUser represents the relationship between a RunPod user and a model entry.
type ModelUser struct {
	UserID              string `json:"userId,omitempty"`
	CredentialType      string `json:"credentialType,omitempty"`
	CredentialReference string `json:"credentialReference,omitempty"`
	Status              string `json:"status,omitempty"`
	UpdatedAt           string `json:"updatedAt,omitempty"`
}

// ModelVersion represents a specific version of a model stored in the repository.
type ModelVersion struct {
	UUID      string                 `json:"uuid,omitempty"`
	Hash      string                 `json:"hash,omitempty"`
	Status    string                 `json:"status,omitempty"`
	CreatedAt string                 `json:"createdAt,omitempty"`
	UpdatedAt string                 `json:"updatedAt,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// ModelVersionStatus constants: the statuses the cli writes when updating a
// model version, plus the ones it has to branch on while waiting for an upload.
// POD_READY and READY are the only two the api accepts for endpoint deployment
// (`READY_MODEL_REPO_STATUSES` in its aiApi schema): a version in any other
// status is rejected at deploy time.
const (
	ModelVersionStatusReady      = "READY"
	ModelVersionStatusPodReady   = "POD_READY"
	ModelVersionStatusNeedsHash  = "NEEDS_HASH"
	ModelVersionStatusFailed     = "FAILED"
	ModelVersionStatusDeprecated = "DEPRECATED"
	ModelVersionStatusPodRemoved = "POD_REMOVED"
)

// ModelRepoUpload describes the multipart upload session returned by createModelRepoUpload.
type ModelRepoUpload struct {
	UploadID         string                 `json:"uploadId"`
	Bucket           string                 `json:"bucket"`
	Key              string                 `json:"key"`
	KeyPrefix        string                 `json:"keyPrefix"`
	PartSizeBytes    int64                  `json:"partSizeBytes"`
	PartCount        int                    `json:"partCount"`
	ExpiresInSeconds int64                  `json:"expiresInSeconds"`
	Parts            []*ModelRepoUploadPart `json:"parts"`
	CompleteURL      string                 `json:"completeUrl"`
	AbortURL         string                 `json:"abortUrl"`
	SessionID        string                 `json:"sessionId"`
	Status           string                 `json:"status"`
}

// UnmarshalJSON implements custom decoding to accept both numeric and string values for
// fields that should be represented as integers in the client API.
func (m *ModelRepoUpload) UnmarshalJSON(data []byte) error {
	type alias struct {
		UploadID         string                 `json:"uploadId"`
		Bucket           string                 `json:"bucket"`
		Key              string                 `json:"key"`
		KeyPrefix        string                 `json:"keyPrefix"`
		PartCount        int                    `json:"partCount"`
		ExpiresInSeconds int64                  `json:"expiresInSeconds"`
		Parts            []*ModelRepoUploadPart `json:"parts"`
		CompleteURL      string                 `json:"completeUrl"`
		AbortURL         string                 `json:"abortUrl"`
		SessionID        string                 `json:"sessionId"`
		Status           string                 `json:"status"`
		PartSizeBytes    json.RawMessage        `json:"partSizeBytes"`
	}

	var aux alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	*m = ModelRepoUpload{
		UploadID:         aux.UploadID,
		Bucket:           aux.Bucket,
		Key:              aux.Key,
		KeyPrefix:        aux.KeyPrefix,
		PartCount:        aux.PartCount,
		ExpiresInSeconds: aux.ExpiresInSeconds,
		Parts:            aux.Parts,
		CompleteURL:      aux.CompleteURL,
		AbortURL:         aux.AbortURL,
		SessionID:        aux.SessionID,
		Status:           aux.Status,
	}

	if len(aux.PartSizeBytes) == 0 || string(aux.PartSizeBytes) == "null" {
		m.PartSizeBytes = 0
		return nil
	}

	if aux.PartSizeBytes[0] == '"' {
		var s string
		if err := json.Unmarshal(aux.PartSizeBytes, &s); err != nil {
			return err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			m.PartSizeBytes = 0
			return nil
		}
		value, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid partSizeBytes value %q: %w", s, err)
		}
		m.PartSizeBytes = value
		return nil
	}

	var value int64
	if err := json.Unmarshal(aux.PartSizeBytes, &value); err != nil {
		return err
	}
	m.PartSizeBytes = value

	return nil
}

// ModelRepoUploadPart represents a single pre-signed URL within a multipart upload session.
type ModelRepoUploadPart struct {
	PartNumber int    `json:"partNumber"`
	URL        string `json:"url"`
	ExpiresAt  string `json:"expiresAt"`
}

// ModelRepoMutationResult represents the payload returned by model repository mutations.
type ModelRepoMutationResult struct {
	Success bool             `json:"success"`
	Message string           `json:"message"`
	Model   *Model           `json:"model,omitempty"`
	Version *ModelVersion    `json:"version,omitempty"`
	Upload  *ModelRepoUpload `json:"upload,omitempty"`
}

// CompleteModelRepoUploadResult represents the response when marking a multipart upload session complete.
type CompleteModelRepoUploadResult struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	SessionID string `json:"sessionId,omitempty"`
	Status    string `json:"status,omitempty"`
}

// ModelVersionStatusMutationResult captures the payload from updateModelVersionStatus.
type ModelVersionStatusMutationResult struct {
	Success      bool          `json:"success"`
	Message      string        `json:"message"`
	ModelVersion *ModelVersion `json:"modelVersion,omitempty"`
}

// AddModelToRepoInput captures the information required to upload a model to the repository.
type AddModelToRepoInput struct {
	Owner               string                 `json:"owner,omitempty"`
	Name                string                 `json:"name"`
	HuggingFaceModel    string                 `json:"huggingFaceModel,omitempty"`
	Provider            string                 `json:"provider,omitempty"`
	CredentialType      string                 `json:"credentialType,omitempty"`
	CredentialReference string                 `json:"credentialReference,omitempty"`
	ModelStatus         string                 `json:"modelStatus,omitempty"`
	Metadata            map[string]interface{} `json:"metadata,omitempty"`
}

// GetModelsInput defines filters that can be used when listing models from the repository.
type GetModelsInput struct {
	Provider string `json:"provider,omitempty"`
	Name     string `json:"name,omitempty"`
}

// GetModelInput captures the identifiers needed to retrieve a single model.
type GetModelInput struct {
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

// RemoveModelInput captures the identifiers that can be used to delete a model from the repository.
type RemoveModelInput struct {
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

// UpdateModelVersionStatusInput captures the identifier and target status for a model version.
type UpdateModelVersionStatusInput struct {
	Hash   string `json:"hash,omitempty"`
	UUID   string `json:"uuid,omitempty"`
	Status string `json:"status"`
}

// CreateModelRepoUploadInput defines the payload used to start a multipart upload for a model version.
type CreateModelRepoUploadInput struct {
	Owner               string                 `json:"owner,omitempty"`
	Name                string                 `json:"name,omitempty"`
	ModelVersionUUID    string                 `json:"modelVersionUuid,omitempty"`
	FileName            string                 `json:"fileName"`
	FileSizeBytes       string                 `json:"fileSizeBytes"`
	PartSizeBytes       string                 `json:"partSizeBytes,omitempty"`
	ContentType         string                 `json:"contentType,omitempty"`
	Metadata            map[string]interface{} `json:"metadata,omitempty"`
	CredentialType      string                 `json:"credentialType,omitempty"`
	CredentialReference string                 `json:"credentialReference,omitempty"`
}

// ModelRepoUploadBatchFileInput is one file of a createModelRepoUploadBatch manifest,
// mirroring CreateModelRepoUploadInput's per-file fields (including their string typing;
// see the field comments below for why).
type ModelRepoUploadBatchFileInput struct {
	FileName string `json:"fileName"`
	// FileSizeBytes is the file's total size in bytes, decimal-encoded as a string rather
	// than a number because the server's fileSizeBytes GraphQL field is a String: GraphQL's
	// Int is 32-bit, which can't represent files/parts above ~2GiB, and JSON numbers above
	// 2^53 lose precision. Mirrors CreateModelRepoUploadInput.FileSizeBytes.
	FileSizeBytes string `json:"fileSizeBytes"`
	// PartSizeBytes optionally overrides the multipart upload's chunk size in bytes (same
	// string-for-large-integer reasoning as FileSizeBytes above); the server picks a default
	// when empty. Mirrors CreateModelRepoUploadInput.PartSizeBytes and the CLI's --part-size flag.
	PartSizeBytes string `json:"partSizeBytes,omitempty"`
	ContentType   string `json:"contentType,omitempty"`
}

// CreateModelRepoUploadBatchInput starts upload sessions for every file in Files in one
// request. All files land on the same model version.
type CreateModelRepoUploadBatchInput struct {
	Owner               string                          `json:"owner,omitempty"`
	Name                string                          `json:"name,omitempty"`
	Files               []ModelRepoUploadBatchFileInput `json:"files"`
	ModelVersionUUID    string                          `json:"modelVersionUuid,omitempty"`
	Metadata            map[string]interface{}          `json:"metadata,omitempty"`
	CredentialType      string                          `json:"credentialType,omitempty"`
	CredentialReference string                          `json:"credentialReference,omitempty"`
}

// ModelRepoUploadBatchResult represents the payload returned by createModelRepoUploadBatch.
type ModelRepoUploadBatchResult struct {
	Success bool               `json:"success"`
	Message string             `json:"message"`
	Model   *Model             `json:"model,omitempty"`
	Version *ModelVersion      `json:"version,omitempty"`
	Uploads []*ModelRepoUpload `json:"uploads"`
}

// modelRepoUploadSessionFields is the GraphQL field selection for a single upload
// session, shared by createModelRepoUpload's `upload` and createModelRepoUploadBatch's
// `uploads`, which return the same ModelRepoUpload shape one-at-a-time vs. batched. Kept
// as one constant so the two mutations can't drift out of sync as fields are added.
const modelRepoUploadSessionFields = `
                                        sessionId
                                        status
                                        uploadId
                                        bucket
                                        key
                                        keyPrefix
                                        partSizeBytes
                                        partCount
                                        expiresInSeconds
                                        parts {
                                                partNumber
                                                url
                                                expiresAt
                                        }
                                        completeUrl
                                        abortUrl`

// modelRepoModelFields is the GraphQL field selection for the `model` object returned
// alongside upload-session mutations, shared for the same reason as
// modelRepoUploadSessionFields above.
const modelRepoModelFields = `
                                        id
                                        owner
                                        name
                                        provider
                                        status
                                        updatedAt`

// modelRepoVersionFields is the GraphQL field selection for the `version` object
// returned alongside upload-session mutations, shared for the same reason as
// modelRepoUploadSessionFields above.
const modelRepoVersionFields = `
                                        uuid
                                        hash
                                        status
                                        metadata
                                        createdAt
                                        updatedAt`

// ModelRepoStorageUsage is a namespace's storage usage and quota state. The byte counts are
// strings because they exceed a graphql Int.
type ModelRepoStorageUsage struct {
	OwnerID        string  `json:"ownerId"`
	CommittedBytes string  `json:"committedBytes"`
	ReservedBytes  string  `json:"reservedBytes"`
	UsedBytes      string  `json:"usedBytes"`
	LimitBytes     *string `json:"limitBytes"`
	AvailableBytes *string `json:"availableBytes"`
	Enforced       bool    `json:"enforced"`
}

// AddModelToRepo uploads a new model to the RunPod model repository.
func AddModelToRepo(input *AddModelToRepoInput) (*Model, error) {
	if input == nil {
		return nil, fmt.Errorf("input cannot be nil")
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, fmt.Errorf("name cannot be empty")
	}

	payload := map[string]interface{}{
		"name": name,
	}

	addString := func(key, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		payload[key] = value
	}

	addString("owner", input.Owner)
	addString("huggingFaceModel", input.HuggingFaceModel)
	addString("provider", input.Provider)
	addString("credentialType", input.CredentialType)
	addString("credentialReference", input.CredentialReference)
	addString("modelStatus", input.ModelStatus)

	if len(input.Metadata) > 0 {
		payload["metadata"] = input.Metadata
	}

	variables := map[string]interface{}{
		"input": payload,
	}

	gqlInput := Input{
		Query: `
                mutation addModelToRepo($input: AddModelToRepoInput!) {
                        addModelToRepo(input: $input) {
                                success
                                message
	                                model {
	                                        id
	                                        owner
	                                        name
	                                        provider
                                        status
                                        createdAt
                                        updatedAt
                                        users {
                                                userId
                                                credentialType
                                                credentialReference
                                                status
                                                updatedAt
                                        }
                                        versions {
                                                hash
                                                status
                                                metadata
                                                createdAt
                                                updatedAt
                                        }
                                }
                        }
                }
                `,
		Variables: variables,
	}

	res, err := Query(gqlInput)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	rawData, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, modelRepoHTTPError(res.StatusCode, rawData)
	}

	var data struct {
		Data *struct {
			AddModelToRepo *ModelRepoMutationResult `json:"addModelToRepo"`
		} `json:"data"`
		Errors []*GraphQLError `json:"errors"`
	}
	if err = json.Unmarshal(rawData, &data); err != nil {
		return nil, err
	}
	if len(data.Errors) > 0 {
		return nil, modelRepoGraphQLError(data.Errors[0])
	}
	if data.Data == nil || data.Data.AddModelToRepo == nil {
		return nil, fmt.Errorf("data is nil: %s", string(rawData))
	}

	result := data.Data.AddModelToRepo
	if !result.Success {
		if result.Message != "" {
			return nil, errors.New(result.Message)
		}
		return nil, fmt.Errorf("model creation failed: %s", string(rawData))
	}
	if result.Model == nil {
		return nil, fmt.Errorf("model is nil: %s", string(rawData))
	}

	return result.Model, nil
}

// GetModels retrieves models that match the provided filters from the repository.
func GetModels(input *GetModelsInput) ([]*Model, error) {
	gqlInput := Input{
		Query: `
	query myModels {
	myModels {
	        id
	        owner
        provider
        name
        status
        createdAt
        updatedAt
        users {
                userId
                credentialType
                credentialReference
                status
                updatedAt
        }
        versions {
                uuid
                hash
                status
                metadata
                createdAt
                updatedAt
        }
	}
	}
	`,
		Variables: nil,
	}

	res, err := Query(gqlInput)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	rawData, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, modelRepoHTTPError(res.StatusCode, rawData)
	}

	var data struct {
		Data *struct {
			MyModels []*Model `json:"myModels"`
		} `json:"data"`
		Errors []*GraphQLError `json:"errors"`
	}
	if err = json.Unmarshal(rawData, &data); err != nil {
		return nil, err
	}
	if len(data.Errors) > 0 {
		return nil, modelRepoGraphQLError(data.Errors[0])
	}
	if data.Data == nil {
		return nil, fmt.Errorf("data is nil: %s", string(rawData))
	}

	models := data.Data.MyModels

	if models == nil {
		models = []*Model{}
	}
	if input != nil {
		if input.Provider != "" {
			filtered := make([]*Model, 0, len(models))
			for _, m := range models {
				if m != nil && m.Provider == input.Provider {
					filtered = append(filtered, m)
				}
			}
			models = filtered
		}
		if input.Name != "" {
			filtered := make([]*Model, 0, len(models))
			for _, m := range models {
				if m != nil && m.Name == input.Name {
					filtered = append(filtered, m)
				}
			}
			models = filtered
		}
	}

	return models, nil
}

// GetModel retrieves a single model from the repository that matches the provided owner and name.
func GetModel(input *GetModelInput) (*Model, error) {
	if input == nil {
		return nil, fmt.Errorf("input cannot be nil")
	}

	owner := strings.TrimSpace(input.Owner)
	if owner == "" {
		return nil, fmt.Errorf("owner cannot be empty")
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, fmt.Errorf("name cannot be empty")
	}

	variables := map[string]interface{}{
		"owner": owner,
		"name":  name,
	}

	gqlInput := Input{
		Query: `
                query myModel($owner: String!, $name: String!) {
                        myModel(owner: $owner, name: $name) {
                                id
                                owner
                                name
                                provider
                                status
                                createdAt
                                updatedAt
                                users {
                                        userId
                                        credentialType
                                        credentialReference
                                        status
                                        updatedAt
                                }
                                versions {
                                        uuid
                                        hash
                                        status
                                        metadata
                                        createdAt
                                        updatedAt
                                }
                        }
                }
                `,
		Variables: variables,
	}

	res, err := Query(gqlInput)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	rawData, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, modelRepoHTTPError(res.StatusCode, rawData)
	}

	var data struct {
		Data *struct {
			MyModel *Model `json:"myModel"`
		} `json:"data"`
		Errors []*GraphQLError `json:"errors"`
	}
	if err = json.Unmarshal(rawData, &data); err != nil {
		return nil, err
	}
	if len(data.Errors) > 0 {
		return nil, modelRepoGraphQLError(data.Errors[0])
	}
	if data.Data == nil || data.Data.MyModel == nil {
		return nil, fmt.Errorf("data is nil: %s", string(rawData))
	}

	return data.Data.MyModel, nil
}

// RemoveModel deletes a model from the RunPod model repository.
func RemoveModel(input *RemoveModelInput) (*ModelRepoMutationResult, error) {
	if input == nil {
		return nil, fmt.Errorf("input cannot be nil")
	}

	if input.Owner == "" {
		return nil, fmt.Errorf("owner cannot be empty")
	}
	if input.Name == "" {
		return nil, fmt.Errorf("name cannot be empty")
	}

	variables := map[string]interface{}{
		"input": map[string]interface{}{
			"owner": input.Owner,
			"name":  input.Name,
		},
	}

	gqlInput := Input{
		Query: `
                mutation removeModelFromRepo($input: RemoveModelFromRepoInput!) {
                        removeModelFromRepo(input: $input) {
                                success
                                message
                                model {
                                        id
                                        owner
                                        name
                                        provider
                                        status
                                        createdAt
                                        updatedAt
                                        users {
                                                userId
                                                credentialType
                                                credentialReference
                                                status
                                                updatedAt
                                        }
                                        versions {
                                                hash
                                                status
                                                metadata
                                                createdAt
                                                updatedAt
                                        }
                                }
                        }
                }
                `,
		Variables: variables,
	}

	res, err := Query(gqlInput)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	rawData, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, modelRepoHTTPError(res.StatusCode, rawData)
	}

	var data struct {
		Data *struct {
			RemoveModelFromRepo *ModelRepoMutationResult `json:"removeModelFromRepo"`
		} `json:"data"`
		Errors []*GraphQLError `json:"errors"`
	}
	if err = json.Unmarshal(rawData, &data); err != nil {
		return nil, err
	}
	if len(data.Errors) > 0 {
		return nil, modelRepoGraphQLError(data.Errors[0])
	}
	if data.Data == nil || data.Data.RemoveModelFromRepo == nil {
		return nil, fmt.Errorf("data is nil: %s", string(rawData))
	}

	result := data.Data.RemoveModelFromRepo
	if !result.Success {
		if result.Message != "" {
			return nil, errors.New(result.Message)
		}
		return nil, fmt.Errorf("model removal failed: %s", string(rawData))
	}

	return result, nil
}

// CreateModelRepoUpload initializes a multipart upload session for a model version artifact.
func CreateModelRepoUpload(input *CreateModelRepoUploadInput) (*ModelRepoMutationResult, error) {
	if input == nil {
		return nil, fmt.Errorf("input cannot be nil")
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, fmt.Errorf("name cannot be empty")
	}
	fileName := strings.TrimSpace(input.FileName)
	if fileName == "" {
		return nil, fmt.Errorf("fileName cannot be empty")
	}

	fileSize := strings.TrimSpace(input.FileSizeBytes)
	if fileSize == "" {
		return nil, fmt.Errorf("fileSizeBytes cannot be empty")
	}

	payload := map[string]interface{}{
		"fileName":      fileName,
		"fileSizeBytes": fileSize,
		"name":          name,
	}

	addString := func(key, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		payload[key] = value
	}

	addString("owner", input.Owner)
	addString("partSizeBytes", input.PartSizeBytes)
	addString("contentType", input.ContentType)
	addString("credentialType", input.CredentialType)
	addString("credentialReference", input.CredentialReference)
	addString("modelVersionUuid", input.ModelVersionUUID)

	if len(input.Metadata) > 0 {
		payload["metadata"] = input.Metadata
	}

	variables := map[string]interface{}{
		"input": payload,
	}

	gqlInput := Input{
		Query: fmt.Sprintf(`
                mutation createModelRepoUpload($input: CreateModelRepoUploadInput!) {
                        createModelRepoUpload(input: $input) {
                                success
                                message
                                upload {%s
                                }
                                model {%s
                                }
                                version {%s
                                }
                        }
                }
                `, modelRepoUploadSessionFields, modelRepoModelFields, modelRepoVersionFields),
		Variables: variables,
	}

	res, err := Query(gqlInput)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	rawData, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, modelRepoHTTPError(res.StatusCode, rawData)
	}

	var data struct {
		Data *struct {
			CreateModelRepoUpload *ModelRepoMutationResult `json:"createModelRepoUpload"`
		} `json:"data"`
		Errors []*GraphQLError `json:"errors"`
	}
	if err = json.Unmarshal(rawData, &data); err != nil {
		return nil, err
	}
	if len(data.Errors) > 0 {
		return nil, modelRepoGraphQLError(data.Errors[0])
	}
	if data.Data == nil || data.Data.CreateModelRepoUpload == nil {
		return nil, fmt.Errorf("data is nil: %s", string(rawData))
	}

	result := data.Data.CreateModelRepoUpload
	if !result.Success {
		if result.Message != "" {
			return nil, errors.New(result.Message)
		}
		return nil, fmt.Errorf("createModelRepoUpload failed: %s", string(rawData))
	}

	if result.Upload == nil {
		return nil, fmt.Errorf("upload is nil: %s", string(rawData))
	}

	return result, nil
}

// CreateModelRepoUploadBatch starts an upload session and presigned URLs for every file in
// input.Files, which must all belong to the same model version.
func CreateModelRepoUploadBatch(input *CreateModelRepoUploadBatchInput) (*ModelRepoUploadBatchResult, error) {
	if input == nil {
		return nil, fmt.Errorf("input cannot be nil")
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, fmt.Errorf("name cannot be empty")
	}
	if len(input.Files) == 0 {
		return nil, fmt.Errorf("files cannot be empty")
	}

	files := make([]map[string]interface{}, 0, len(input.Files))
	for i, file := range input.Files {
		fileName := strings.TrimSpace(file.FileName)
		if fileName == "" {
			return nil, fmt.Errorf("files[%d].fileName cannot be empty", i)
		}
		fileSize := strings.TrimSpace(file.FileSizeBytes)
		if fileSize == "" {
			return nil, fmt.Errorf("files[%d].fileSizeBytes cannot be empty", i)
		}

		entry := map[string]interface{}{
			"fileName":      fileName,
			"fileSizeBytes": fileSize,
		}
		if v := strings.TrimSpace(file.PartSizeBytes); v != "" {
			entry["partSizeBytes"] = v
		}
		if v := strings.TrimSpace(file.ContentType); v != "" {
			entry["contentType"] = v
		}
		files = append(files, entry)
	}

	payload := map[string]interface{}{
		"name":  name,
		"files": files,
	}

	addString := func(key, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		payload[key] = value
	}

	addString("owner", input.Owner)
	addString("credentialType", input.CredentialType)
	addString("credentialReference", input.CredentialReference)
	addString("modelVersionUuid", input.ModelVersionUUID)

	if len(input.Metadata) > 0 {
		payload["metadata"] = input.Metadata
	}

	variables := map[string]interface{}{
		"input": payload,
	}

	gqlInput := Input{
		Query: fmt.Sprintf(`
                mutation createModelRepoUploadBatch($input: CreateModelRepoUploadBatchInput!) {
                        createModelRepoUploadBatch(input: $input) {
                                success
                                message
                                uploads {%s
                                }
                                model {%s
                                }
                                version {%s
                                }
                        }
                }
                `, modelRepoUploadSessionFields, modelRepoModelFields, modelRepoVersionFields),
		Variables: variables,
	}

	res, err := Query(gqlInput)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	rawData, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, modelRepoHTTPError(res.StatusCode, rawData)
	}

	var data struct {
		Data *struct {
			CreateModelRepoUploadBatch *ModelRepoUploadBatchResult `json:"createModelRepoUploadBatch"`
		} `json:"data"`
		Errors []*GraphQLError `json:"errors"`
	}
	if err = json.Unmarshal(rawData, &data); err != nil {
		return nil, err
	}
	if len(data.Errors) > 0 {
		return nil, modelRepoGraphQLError(data.Errors[0])
	}
	if data.Data == nil || data.Data.CreateModelRepoUploadBatch == nil {
		return nil, fmt.Errorf("data is nil: %s", string(rawData))
	}

	result := data.Data.CreateModelRepoUploadBatch
	if !result.Success {
		if result.Message != "" {
			return nil, errors.New(result.Message)
		}
		return nil, fmt.Errorf("createModelRepoUploadBatch failed: %s", string(rawData))
	}

	if len(result.Uploads) != len(input.Files) {
		return nil, fmt.Errorf("expected %d upload sessions, got %d: %s", len(input.Files), len(result.Uploads), string(rawData))
	}

	return result, nil
}

// GetModelRepoStorageUsage fetches storage usage and quota state for owner. An empty owner
// defaults to the acting user's own namespace.
func GetModelRepoStorageUsage(owner string) (*ModelRepoStorageUsage, error) {
	variables := map[string]interface{}{}
	if owner = strings.TrimSpace(owner); owner != "" {
		variables["owner"] = owner
	}

	gqlInput := Input{
		Query: `
                query modelRepoStorageUsage($owner: String) {
                        modelRepoStorageUsage(owner: $owner) {
                                ownerId
                                committedBytes
                                reservedBytes
                                usedBytes
                                limitBytes
                                availableBytes
                                enforced
                        }
                }
                `,
		Variables: variables,
	}

	res, err := Query(gqlInput)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	rawData, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, modelRepoHTTPError(res.StatusCode, rawData)
	}

	var data struct {
		Data *struct {
			ModelRepoStorageUsage *ModelRepoStorageUsage `json:"modelRepoStorageUsage"`
		} `json:"data"`
		Errors []*GraphQLError `json:"errors"`
	}
	if err = json.Unmarshal(rawData, &data); err != nil {
		return nil, err
	}
	if len(data.Errors) > 0 {
		return nil, modelRepoGraphQLError(data.Errors[0])
	}
	if data.Data == nil || data.Data.ModelRepoStorageUsage == nil {
		return nil, fmt.Errorf("data is nil: %s", string(rawData))
	}

	return data.Data.ModelRepoStorageUsage, nil
}

// CompleteModelRepoUpload notifies the Model Repo service that an upload session has finished uploading to storage.
func CompleteModelRepoUpload(sessionID string) (*CompleteModelRepoUploadResult, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("sessionId cannot be empty")
	}

	variables := map[string]interface{}{
		"input": map[string]interface{}{
			"sessionId": sessionID,
		},
	}

	gqlInput := Input{
		Query: `
                mutation completeModelRepoUpload($input: CompleteModelRepoUploadInput!) {
                        completeModelRepoUpload(input: $input) {
                                success
                                message
                                sessionId
                                status
                        }
                }
                `,
		Variables: variables,
	}

	res, err := Query(gqlInput)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	rawData, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, modelRepoHTTPError(res.StatusCode, rawData)
	}

	var data struct {
		Data *struct {
			CompleteModelRepoUpload *CompleteModelRepoUploadResult `json:"completeModelRepoUpload"`
		} `json:"data"`
		Errors []*GraphQLError `json:"errors"`
	}
	if err = json.Unmarshal(rawData, &data); err != nil {
		return nil, err
	}
	if len(data.Errors) > 0 {
		return nil, modelRepoGraphQLError(data.Errors[0])
	}
	if data.Data == nil || data.Data.CompleteModelRepoUpload == nil {
		return nil, fmt.Errorf("data is nil: %s", string(rawData))
	}

	result := data.Data.CompleteModelRepoUpload
	if !result.Success {
		if result.Message != "" {
			return nil, errors.New(result.Message)
		}
		return nil, fmt.Errorf("completeModelRepoUpload failed: %s", string(rawData))
	}

	return result, nil
}

// CompleteModelRepoUploadBatch finalizes every session in sessionIDs with a single GraphQL
// request instead of one per session: the server has no dedicated batch-complete mutation,
// so this fans the list out as one aliased completeModelRepoUpload field (c0, c1, ...) per
// session inside a single mutation document.
//
// completeModelRepoUpload's return type (CompleteModelRepoUploadResult) is non-null, so per
// the GraphQL spec, an error thrown by *any one* aliased field (e.g. an unknown or
// already-finalized session) nulls the entire response's top-level data -- even though the
// server still executes every other aliased mutation field in the document (root mutation
// fields execute serially, in document order, regardless of a sibling's error). That means
// on any single failure this function cannot read back whether the *other* sessions in the
// batch actually completed: it reports exactly which session(s) errored (matched via the
// GraphQL errors' `path`), but the batch's overall outcome must be treated as unconfirmed,
// not "everything else failed too".
func CompleteModelRepoUploadBatch(sessionIDs []string) ([]*CompleteModelRepoUploadResult, error) {
	if len(sessionIDs) == 0 {
		return nil, nil
	}

	variables := make(map[string]interface{}, len(sessionIDs))
	var query strings.Builder
	query.WriteString("mutation completeModelRepoUploadBatch(")
	for i, sessionID := range sessionIDs {
		sessionID = strings.TrimSpace(sessionID)
		if sessionID == "" {
			return nil, fmt.Errorf("sessionIds[%d] cannot be empty", i)
		}
		if i > 0 {
			query.WriteString(", ")
		}
		fmt.Fprintf(&query, "$input%d: CompleteModelRepoUploadInput!", i)
		variables[fmt.Sprintf("input%d", i)] = map[string]interface{}{"sessionId": sessionID}
	}
	query.WriteString(") {\n")
	for i := range sessionIDs {
		fmt.Fprintf(&query, "\tc%d: completeModelRepoUpload(input: $input%d) {\n\t\tsuccess\n\t\tmessage\n\t\tsessionId\n\t\tstatus\n\t}\n", i, i)
	}
	query.WriteString("}")

	gqlInput := Input{Query: query.String(), Variables: variables}

	res, err := Query(gqlInput)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	rawData, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, modelRepoHTTPError(res.StatusCode, rawData)
	}

	var data struct {
		Data   map[string]*CompleteModelRepoUploadResult `json:"data"`
		Errors []struct {
			Message string        `json:"message"`
			Path    []interface{} `json:"path"`
		} `json:"errors"`
	}
	if err = json.Unmarshal(rawData, &data); err != nil {
		return nil, err
	}

	if len(data.Errors) > 0 {
		failedByAlias := make(map[string]string, len(data.Errors))
		for _, gqlErr := range data.Errors {
			if len(gqlErr.Path) == 0 {
				continue
			}
			if alias, ok := gqlErr.Path[0].(string); ok {
				failedByAlias[alias] = gqlErr.Message
			}
		}
		var failed []string
		for i, sessionID := range sessionIDs {
			if msg, ok := failedByAlias[fmt.Sprintf("c%d", i)]; ok {
				failed = append(failed, fmt.Sprintf("%s: %s", sessionID, msg))
			}
		}
		if len(failed) == 0 {
			// Path didn't resolve to a known alias (unexpected shape); fall back to the
			// raw first error rather than claiming zero failures.
			return nil, modelRepoGraphQLError(&GraphQLError{Message: data.Errors[0].Message})
		}
		return nil, fmt.Errorf(
			"%d of %d sessions failed to finalize (%s); the server-side outcome of the other %d sessions in this same batch request is not confirmed by this response and must be reconciled separately, not assumed failed",
			len(failed), len(sessionIDs), strings.Join(failed, "; "), len(sessionIDs)-len(failed),
		)
	}
	if data.Data == nil {
		return nil, fmt.Errorf("data is nil: %s", string(rawData))
	}

	results := make([]*CompleteModelRepoUploadResult, len(sessionIDs))
	for i, sessionID := range sessionIDs {
		result := data.Data[fmt.Sprintf("c%d", i)]
		if result == nil {
			return nil, fmt.Errorf("missing result for session %s: %s", sessionID, string(rawData))
		}
		if !result.Success {
			if result.Message != "" {
				return nil, fmt.Errorf("session %s: %s", sessionID, result.Message)
			}
			return nil, fmt.Errorf("completeModelRepoUpload failed for session %s: %s", sessionID, string(rawData))
		}
		results[i] = result
	}
	return results, nil
}

// UpdateModelVersionStatus updates the status for a model version by hash.
func UpdateModelVersionStatus(hash, status string) (*ModelVersion, error) {
	return UpdateModelVersionStatusByIdentifier(&UpdateModelVersionStatusInput{
		Hash:   hash,
		Status: status,
	})
}

// UpdateModelVersionStatusByIdentifier updates the status for a model version by hash or UUID.
func UpdateModelVersionStatusByIdentifier(input *UpdateModelVersionStatusInput) (*ModelVersion, error) {
	gqlInput, err := newUpdateModelVersionStatusInput(input)
	if err != nil {
		return nil, err
	}

	res, err := Query(gqlInput)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	rawData, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, modelRepoHTTPError(res.StatusCode, rawData)
	}

	var data struct {
		Data *struct {
			UpdateModelVersionStatus *ModelVersionStatusMutationResult `json:"updateModelVersionStatus"`
		} `json:"data"`
		Errors []*GraphQLError `json:"errors"`
	}
	if err = json.Unmarshal(rawData, &data); err != nil {
		return nil, err
	}
	if len(data.Errors) > 0 {
		return nil, modelRepoGraphQLError(data.Errors[0])
	}
	if data.Data == nil || data.Data.UpdateModelVersionStatus == nil {
		return nil, fmt.Errorf("data is nil: %s", string(rawData))
	}

	result := data.Data.UpdateModelVersionStatus
	if !result.Success {
		if result.Message != "" {
			return nil, errors.New(result.Message)
		}
		return nil, fmt.Errorf("updateModelVersionStatus failed: %s", string(rawData))
	}
	if result.ModelVersion == nil {
		return nil, fmt.Errorf("modelVersion is nil: %s", string(rawData))
	}

	return result.ModelVersion, nil
}

func newUpdateModelVersionStatusInput(input *UpdateModelVersionStatusInput) (Input, error) {
	if input == nil {
		return Input{}, fmt.Errorf("input cannot be nil")
	}

	hash := strings.TrimSpace(input.Hash)
	uuid := strings.TrimSpace(input.UUID)
	if hash == "" && uuid == "" {
		return Input{}, fmt.Errorf("either hash or uuid must be provided")
	}
	if hash != "" && uuid != "" {
		return Input{}, fmt.Errorf("only one of hash or uuid can be provided")
	}

	status := strings.TrimSpace(input.Status)
	if status == "" {
		return Input{}, fmt.Errorf("status cannot be empty")
	}

	variables := map[string]interface{}{
		"status": status,
	}
	if hash != "" {
		variables["hash"] = hash
	}
	if uuid != "" {
		variables["uuid"] = uuid
	}

	return Input{
		Query: `
                mutation updateModelVersionStatus($uuid: ID, $hash: ID, $status: ModelVersionStatus!) {
                        updateModelVersionStatus(uuid: $uuid, hash: $hash, status: $status) {
                                success
                                message
                                modelVersion {
                                        uuid
                                        hash
                                        status
                                        metadata
                                        createdAt
                                        updatedAt
                                }
                        }
                }
                `,
		Variables: variables,
	}, nil
}
