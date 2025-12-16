package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// ErrModelRepoNotImplemented is retained for backwards compatibility with callers that
// handled the previous unimplemented model repository helpers.
var ErrModelRepoNotImplemented = errors.New("model repository functionality not yet implemented")

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
	Hash        string                 `json:"hash,omitempty"`
	VersionHash string                 `json:"versionHash,omitempty"`
	Status      string                 `json:"status,omitempty"`
	CreatedAt   string                 `json:"createdAt,omitempty"`
	UpdatedAt   string                 `json:"updatedAt,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// ModelVersionStatus constants used when updating a model version's status.
const (
	ModelVersionStatusReady = "READY"
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
	Name                string                 `json:"name"`
	CredentialType      string                 `json:"credentialType,omitempty"`
	CredentialReference string                 `json:"credentialReference,omitempty"`
	ModelStatus         string                 `json:"modelStatus,omitempty"`
	VersionStatus       string                 `json:"versionStatus,omitempty"`
	Metadata            map[string]interface{} `json:"metadata,omitempty"`
}

// GetModelsInput defines filters that can be used when listing models from the repository.
type GetModelsInput struct {
	Provider string `json:"provider,omitempty"`
	Name     string `json:"name,omitempty"`
	All      bool   `json:"-"`
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

// CreateModelRepoUploadInput defines the payload used to start a multipart upload for a model version.
type CreateModelRepoUploadInput struct {
	Name                string                 `json:"name,omitempty"`
	FileName            string                 `json:"fileName"`
	FileSizeBytes       string                 `json:"fileSizeBytes"`
	PartSizeBytes       string                 `json:"partSizeBytes,omitempty"`
	ContentType         string                 `json:"contentType,omitempty"`
	Metadata            map[string]interface{} `json:"metadata,omitempty"`
	CredentialType      string                 `json:"credentialType,omitempty"`
	CredentialReference string                 `json:"credentialReference,omitempty"`
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

	addString("credentialType", input.CredentialType)
	addString("credentialReference", input.CredentialReference)
	addString("modelStatus", input.ModelStatus)
	addString("versionStatus", input.VersionStatus)

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
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("statuscode %d: %s", res.StatusCode, string(rawData))
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
		return nil, errors.New(data.Errors[0].Message)
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
	queryName := "myModels"
	fieldName := "myModels"
	if input != nil && input.All {
		queryName = "models"
		fieldName = "models"
	}

	gqlInput := Input{
		Query: fmt.Sprintf(`
query %s {
%s {
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
                hash
                status
                metadata
                createdAt
                updatedAt
        }
}
}
`, queryName, fieldName),
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
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("statuscode %d: %s", res.StatusCode, string(rawData))
	}

	var data struct {
		Data *struct {
			MyModels []*Model `json:"myModels"`
			Models   []*Model `json:"models"`
		} `json:"data"`
		Errors []*GraphQLError `json:"errors"`
	}
	if err = json.Unmarshal(rawData, &data); err != nil {
		return nil, err
	}
	if len(data.Errors) > 0 {
		return nil, errors.New(data.Errors[0].Message)
	}
	if data.Data == nil {
		return nil, fmt.Errorf("data is nil: %s", string(rawData))
	}

	var models []*Model
	switch fieldName {
	case "models":
		models = data.Data.Models
	default:
		models = data.Data.MyModels
	}

	if models == nil {
		return nil, fmt.Errorf("data is nil: %s", string(rawData))
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
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("statuscode %d: %s", res.StatusCode, string(rawData))
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
		return nil, errors.New(data.Errors[0].Message)
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
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("statuscode %d: %s", res.StatusCode, string(rawData))
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
		return nil, errors.New(data.Errors[0].Message)
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

	addString("partSizeBytes", input.PartSizeBytes)
	addString("contentType", input.ContentType)
	addString("credentialType", input.CredentialType)
	addString("credentialReference", input.CredentialReference)

	if len(input.Metadata) > 0 {
		payload["metadata"] = input.Metadata
	}

	variables := map[string]interface{}{
		"input": payload,
	}

	gqlInput := Input{
		Query: `
                mutation createModelRepoUpload($input: CreateModelRepoUploadInput!) {
                        createModelRepoUpload(input: $input) {
                                success
                                message
                                upload {
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
                                        abortUrl
                                }
                                model {
                                        id
                                        name
                                        provider
                                        status
                                        updatedAt
                                }
                                version {
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
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("statuscode %d: %s", res.StatusCode, string(rawData))
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
		return nil, errors.New(data.Errors[0].Message)
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
		return nil, fmt.Errorf("statuscode %d: %s", res.StatusCode, string(rawData))
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
		return nil, errors.New(data.Errors[0].Message)
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

// UpdateModelVersionStatus updates the status for a model version in the repository.
func UpdateModelVersionStatus(hash, status string) (*ModelVersion, error) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return nil, fmt.Errorf("hash cannot be empty")
	}

	status = strings.TrimSpace(status)
	if status == "" {
		return nil, fmt.Errorf("status cannot be empty")
	}

	variables := map[string]interface{}{
		"hash":   hash,
		"status": status,
	}

	gqlInput := Input{
		Query: `
                mutation updateModelVersionStatus($hash: ID!, $status: ModelVersionStatus!) {
                        updateModelVersionStatus(hash: $hash, status: $status) {
                                success
                                message
                                modelVersion {
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
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("statuscode %d: %s", res.StatusCode, string(rawData))
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
		return nil, errors.New(data.Errors[0].Message)
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
