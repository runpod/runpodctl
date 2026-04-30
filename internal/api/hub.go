package api

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Listing represents a hub listing (repo)
type Listing struct {
	ID                 string      `json:"id"`
	Title              string      `json:"title,omitempty"`
	RepoName           string      `json:"repoName"`
	RepoOwner          string      `json:"repoOwner"`
	RepoOwnerAvatarUrl string      `json:"repoOwnerAvatarUrl,omitempty"`
	Description        string      `json:"description,omitempty"`
	Category           string      `json:"category,omitempty"`
	Type               string      `json:"type"`
	Tags               []string    `json:"tags,omitempty"`
	Stars              int         `json:"stars"`
	Views              int         `json:"views"`
	Deploys            int         `json:"deploys"`
	OpenIssues         int         `json:"openIssues"`
	Watchers           int         `json:"watchers"`
	Language           string      `json:"language,omitempty"`
	CreatedAt          string      `json:"createdAt"`
	UpdatedAt          string      `json:"updatedAt"`
	ListedRelease      *HubRelease `json:"listedRelease,omitempty"`
}

// HubRelease represents a release of a hub listing
type HubRelease struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	TagName    string    `json:"tagName"`
	Body       string    `json:"body,omitempty"`
	Readme     string    `json:"readme,omitempty"`
	Branch     string    `json:"branch,omitempty"`
	License    string    `json:"license,omitempty"`
	Config     string    `json:"config,omitempty"`
	Deploys    int       `json:"deploys"`
	IconUrl    string    `json:"iconUrl,omitempty"`
	AuthorName string    `json:"authorName,omitempty"`
	CreatedAt  string    `json:"createdAt"`
	ReleasedAt string    `json:"releasedAt"`
	UpdatedAt  string    `json:"updatedAt"`
	Build      *GitBuild `json:"build,omitempty"`
	Tests      string    `json:"tests,omitempty"`
}

// GitBuild represents a build from a hub release
type GitBuild struct {
	ImageName string `json:"imageName,omitempty"`
}

// HubReleaseConfig is the parsed config from a hub release
type HubReleaseConfig struct {
	ContainerDiskInGb int                   `json:"containerDiskInGb,omitempty"`
	GpuIDs            string                `json:"gpuIds,omitempty"`
	GpuCount          int                   `json:"gpuCount,omitempty"`
	Env               []HubReleaseConfigEnv `json:"env,omitempty"`
}

// HubReleaseConfigEnv is an env var entry in the hub release config
type HubReleaseConfigEnv struct {
	Key   string                    `json:"key"`
	Input *HubReleaseConfigEnvInput `json:"input,omitempty"`
}

// HubReleaseConfigEnvInput describes the env var input metadata
type HubReleaseConfigEnvInput struct {
	Default interface{} `json:"default,omitempty"`
}

// ListingsOptions for listing/searching hub entries
type ListingsOptions struct {
	SearchQuery    string
	Category       string
	Limit          int
	Offset         int
	OrderBy        string
	OrderDirection string
	Owner          string
	Type           string // client-side filter: POD or SERVERLESS
}

// ListListings returns hub listings with optional filtering
func (c *Client) ListListings(opts *ListingsOptions) ([]Listing, error) {
	query := `
		query Listings($input: ListingsInput!) {
			listings(input: $input) {
				id
				title
				repoName
				repoOwner
				description
				category
				type
				tags
				stars
				views
				deploys
				language
				createdAt
				updatedAt
			}
		}
	`

	input := map[string]interface{}{}
	if opts != nil {
		if opts.SearchQuery != "" {
			input["searchQuery"] = opts.SearchQuery
		}
		if opts.Category != "" {
			input["category"] = opts.Category
		}
		if opts.Limit > 0 {
			input["limit"] = opts.Limit
		}
		if opts.Offset > 0 {
			input["offset"] = opts.Offset
		}
		if opts.OrderBy != "" {
			input["orderBy"] = opts.OrderBy
		}
		if opts.OrderDirection != "" {
			input["orderDirection"] = opts.OrderDirection
		}
		if opts.Owner != "" {
			input["owner"] = opts.Owner
		}
	}

	variables := map[string]interface{}{
		"input": input,
	}

	data, err := c.graphqlRequest(query, variables)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			Listings []Listing `json:"listings"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("graphql error: %s", resp.Errors[0].Message)
	}

	listings := resp.Data.Listings

	// client-side type filter (ListingsInput has no type field)
	if opts != nil && opts.Type != "" {
		filterType := strings.ToUpper(opts.Type)
		var filtered []Listing
		for _, l := range listings {
			if strings.ToUpper(l.Type) == filterType {
				filtered = append(filtered, l)
			}
		}
		listings = filtered
	}

	return listings, nil
}

const listingFullFields = `
	id
	title
	repoName
	repoOwner
	repoOwnerAvatarUrl
	description
	category
	type
	tags
	stars
	views
	deploys
	openIssues
	watchers
	language
	createdAt
	updatedAt
	listedRelease {
		id
		name
		tagName
		body
		readme
		branch
		license
		config
		deploys
		iconUrl
		authorName
		createdAt
		releasedAt
		updatedAt
		build {
			imageName
		}
		tests
	}
`

// GetListing returns a single hub listing by ID
func (c *Client) GetListing(listingID string) (*Listing, error) {
	query := fmt.Sprintf(`
		query GetListing($id: String!) {
			listing(id: $id) {
				%s
			}
		}
	`, listingFullFields)

	variables := map[string]interface{}{
		"id": listingID,
	}

	data, err := c.graphqlRequest(query, variables)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			Listing *Listing `json:"listing"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("graphql error: %s", resp.Errors[0].Message)
	}

	if resp.Data.Listing == nil {
		return nil, fmt.Errorf("hub listing not found: %s", listingID)
	}

	return resp.Data.Listing, nil
}

// GetListingFromRepo returns a hub listing by repo owner and name
func (c *Client) GetListingFromRepo(owner, name string) (*Listing, error) {
	query := fmt.Sprintf(`
		query GetListingFromRepo($repoOwner: String!, $repoName: String!) {
			listingFromRepo(repoOwner: $repoOwner, repoName: $repoName) {
				%s
			}
		}
	`, listingFullFields)

	variables := map[string]interface{}{
		"repoOwner": owner,
		"repoName":  name,
	}

	data, err := c.graphqlRequest(query, variables)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			Listing *Listing `json:"listingFromRepo"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("graphql error: %s", resp.Errors[0].Message)
	}

	if resp.Data.Listing == nil {
		return nil, fmt.Errorf("hub listing not found: %s/%s", owner, name)
	}

	return resp.Data.Listing, nil
}
