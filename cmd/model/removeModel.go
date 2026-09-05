package model

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/runpod/runpodctl/api"
	internalapi "github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var (
	removeOwner   string
	removeName    string
	removeHash    string
	removeVersion string
)

var (
	getModelForRemove                    = api.GetModel
	removeModelFromRepo                  = api.RemoveModel
	updateModelVersionStatusByIdentifier = api.UpdateModelVersionStatusByIdentifier
	listDependentEndpoints               = defaultListDependentEndpoints
)

// DependentEndpoint identifies a serverless endpoint whose model reference
// points at a Model Repo model/version that is about to be removed.
type DependentEndpoint struct {
	ID   string
	Name string
}

// dependentEndpointsError blocks a removal because active endpoints still
// reference the model/version. There is no override: a model/version that is
// in use cannot be removed at all. ErrorCode gives agents a stable, machine
// readable way to detect this specific condition rather than parsing the
// message string.
type dependentEndpointsError struct {
	endpoints []DependentEndpoint
}

func (e *dependentEndpointsError) Error() string {
	return fmt.Sprintf(
		"cannot remove: %d endpoint(s) reference this model: %s. detach or replace it first with 'runpodctl serverless update <endpoint-id> --clear-models' (or --model-reference <new-url>), then retry",
		len(e.endpoints), formatDependentEndpoints(e.endpoints),
	)
}

func (e *dependentEndpointsError) ErrorCode() string { return "dependent_endpoints" }

func formatDependentEndpoints(endpoints []DependentEndpoint) string {
	parts := make([]string, len(endpoints))
	for i, ep := range endpoints {
		name := ep.Name
		if name == "" {
			name = ep.ID
		}
		parts[i] = fmt.Sprintf("%s (%s)", ep.ID, name)
	}
	return strings.Join(parts, ", ")
}

// defaultListDependentEndpoints lists serverless endpoints whose model
// references point at the given Model Repo model (owner/name), optionally
// pinned to a specific version hash. Pass hash="" to match any version of the
// model (used when removing the whole model rather than a single version).
//
// This goes through the GraphQL api (api.Query), not internal/api's REST
// client: verified live that REST GET /v1/endpoints and /v1/endpoints/{id}
// never include modelReferences at all (confirmed by inspecting the raw REST
// response for a real endpoint), so ListEndpoints() would silently find zero
// dependents every time. modelReferences is exposed via GraphQL, e.g.
// myself { endpoints { modelReferences } }.
func defaultListDependentEndpoints(owner, name, hash string) ([]DependentEndpoint, error) {
	gqlInput := api.Input{
		Query: `
		query listEndpointsForModelDependencyCheck {
			myself {
				endpoints {
					id
					name
					modelReferences
				}
			}
		}
		`,
	}

	res, err := api.Query(gqlInput)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	rawData, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, &internalapi.GraphQLError{
			Message: fmt.Sprintf("statuscode %d: %s", res.StatusCode, string(rawData)),
			Status:  res.StatusCode,
		}
	}

	var data struct {
		Data *struct {
			Myself *struct {
				Endpoints []struct {
					ID              string   `json:"id"`
					Name            string   `json:"name"`
					ModelReferences []string `json:"modelReferences"`
				} `json:"endpoints"`
			} `json:"myself"`
		} `json:"data"`
		Errors []*api.GraphQLError `json:"errors"`
	}
	if err := json.Unmarshal(rawData, &data); err != nil {
		return nil, err
	}
	if len(data.Errors) > 0 {
		// same Model Repo access-error shape as api/model.go: the resolver
		// throws before returning data, so this is where a disabled/inaccessible
		// Model Repo surfaces. Wrap it as a typed GraphQLError so it carries the
		// stable "graphql_error" code through checkDependentEndpoints' %w wrap
		// instead of falling back to cli_error.
		return nil, &internalapi.GraphQLError{Message: data.Errors[0].Message}
	}
	if data.Data == nil || data.Data.Myself == nil {
		return nil, fmt.Errorf("data is nil: %s", string(rawData))
	}

	var dependents []DependentEndpoint
	for _, ep := range data.Data.Myself.Endpoints {
		for _, ref := range ep.ModelReferences {
			if modelReferenceMatches(ref, owner, name, hash) {
				dependents = append(dependents, DependentEndpoint{ID: ep.ID, Name: ep.Name})
				break
			}
		}
	}

	return dependents, nil
}

// modelReferenceMatches reports whether an endpoint's model reference string
// points at the given Model Repo model/version. Model Repo references are
// emitted client-side as "https://local/<owner>/<name>:<hash>" (see
// formatModelURL), but verified live against prod that the backend persists
// the scheme+host in uppercase ("https://LOCAL/..."). The prefix comparison is
// therefore case-insensitive; the owner/name/hash path segments stay
// case-sensitive exact matches.
func modelReferenceMatches(ref, owner, name, hash string) bool {
	const schemeHost = "https://local/"
	if len(ref) < len(schemeHost) || !strings.EqualFold(ref[:len(schemeHost)], schemeHost) {
		return false
	}

	path := ref[len(schemeHost):]
	prefix := owner + "/" + name + ":"
	if !strings.HasPrefix(path, prefix) {
		return false
	}

	if hash == "" {
		return true
	}
	return path[len(prefix):] == hash
}

// checkDependentEndpoints looks up endpoints that reference owner/name
// (optionally pinned to hash) and blocks the removal if any are found. There
// is no override for this: a model/version in active use cannot be removed.
// A failure to complete the check itself blocks the removal too — if we
// cannot verify it is safe, it is not safe to proceed.
func checkDependentEndpoints(owner, name, hash string) error {
	dependents, err := listDependentEndpoints(owner, name, hash)
	if err != nil {
		return fmt.Errorf("failed to check for dependent endpoints, refusing to remove: %w", err)
	}

	if len(dependents) == 0 {
		return nil
	}

	return &dependentEndpointsError{endpoints: dependents}
}

var removeCmd = &cobra.Command{
	Use:     "remove",
	Aliases: []string{"rm", "delete"},
	Args:    cobra.ExactArgs(0),
	Short:   "remove a model",
	Long:    "remove a model from the runpod model repository. blocked if any endpoints still reference it — a model/version in active use cannot be removed.",
	Example: `  # blocked if an endpoint still references this model, listing which one(s)
  runpodctl model remove --owner <owner> --name my-model

  # detach it from the endpoint first, then remove
  runpodctl serverless update <endpoint-id> --clear-models
  runpodctl model remove --owner <owner> --name my-model`,
	RunE: runRemoveModel,
}

var RemoveModelCmd = &cobra.Command{
	Use:    "model",
	Args:   cobra.ExactArgs(0),
	Short:  "deprecated: use 'runpodctl model remove'",
	Long:   "",
	Hidden: true,
	RunE:   runRemoveModel,
}

func init() {
	bindRemoveModelFlags(removeCmd)
	bindRemoveModelFlags(RemoveModelCmd)
	removeCmd.MarkFlagRequired("owner")      //nolint
	removeCmd.MarkFlagRequired("name")       //nolint
	RemoveModelCmd.MarkFlagRequired("owner") //nolint
	RemoveModelCmd.MarkFlagRequired("name")  //nolint
}

func bindRemoveModelFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&removeOwner, "owner", "", "model owner")
	cmd.Flags().StringVar(&removeName, "name", "", "model name")
	cmd.Flags().StringVar(&removeHash, "hash", "", "model version hash to remove")
	cmd.Flags().StringVar(&removeVersion, "version", "", "model version uuid to remove")
}

func runRemoveModel(cmd *cobra.Command, args []string) error {
	if removeOwner == "" || removeName == "" {
		return fmt.Errorf("both --owner and --name must be provided")
	}

	hash := strings.TrimSpace(removeHash)
	version := strings.TrimSpace(removeVersion)
	if hash != "" && version != "" {
		return fmt.Errorf("only one of --hash or --version can be provided")
	}

	var result *api.ModelRepoMutationResult
	var err error
	if hash != "" || version != "" {
		result, err = removeModelVersion(removeOwner, removeName, hash, version)
	} else {
		if err := checkDependentEndpoints(removeOwner, removeName, ""); err != nil {
			return err
		}
		input := &api.RemoveModelInput{
			Owner: removeOwner,
			Name:  removeName,
		}
		result, err = removeModelFromRepo(input)
	}

	if err != nil {

		return err
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(result, &output.Config{Format: format})
}

func removeModelVersion(owner, name, hash, version string) (*api.ModelRepoMutationResult, error) {
	model, err := getModelForRemove(&api.GetModelInput{
		Owner: owner,
		Name:  name,
	})
	if err != nil {
		return nil, err
	}

	target := findModelVersion(model, hash, version)
	if target == nil {
		if hash != "" {
			return nil, fmt.Errorf("model version hash %q not found for %s/%s", hash, owner, name)
		}
		return nil, fmt.Errorf("model version %q not found for %s/%s", version, owner, name)
	}

	// endpoints reference a version by its hash (see formatModelURL), so the
	// dependent-endpoint check always keys off target.Hash regardless of
	// whether the caller identified the version by --hash or --version.
	if err := checkDependentEndpoints(owner, name, target.Hash); err != nil {
		return nil, err
	}

	input := &api.UpdateModelVersionStatusInput{
		Status: api.ModelVersionStatusPodRemoved,
	}
	if hash != "" {
		input.Hash = target.Hash
	} else {
		input.UUID = target.UUID
	}

	updatedVersion, err := updateModelVersionStatusByIdentifier(input)
	if err != nil {
		return nil, err
	}
	patchModelVersion(model, target, updatedVersion)

	return &api.ModelRepoMutationResult{
		Success: true,
		Model:   model,
		Version: updatedVersion,
	}, nil
}

func findModelVersion(model *api.Model, hash, version string) *api.ModelVersion {
	if model == nil {
		return nil
	}

	for _, modelVersion := range model.Versions {
		if modelVersion == nil {
			continue
		}
		if hash != "" && modelVersion.Hash == hash {
			return modelVersion
		}
		if version != "" && modelVersion.UUID == version {
			return modelVersion
		}
	}

	return nil
}

func patchModelVersion(model *api.Model, target, updated *api.ModelVersion) {
	if model == nil || target == nil || updated == nil {
		return
	}

	for i, modelVersion := range model.Versions {
		if modelVersion == target {
			model.Versions[i] = updated
			return
		}
	}
}
