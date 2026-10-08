package api

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// TemplatePortConfig represents a named port shown in the Runpod dashboard.
type TemplatePortConfig struct {
	Port string `json:"port"`
	Name string `json:"name"`
}

// Template represents a runpod template
type Template struct {
	ID                      string               `json:"id"`
	Name                    string               `json:"name"`
	ImageName               string               `json:"imageName"`
	IsServerless            bool                 `json:"isServerless,omitempty"`
	IsPublic                bool                 `json:"isPublic,omitempty"`
	IsRunpod                bool                 `json:"isRunpod,omitempty"`
	Category                string               `json:"category,omitempty"`
	Ports                   []string             `json:"ports,omitempty"`
	PortsConfig             []TemplatePortConfig `json:"portsConfig,omitempty"`
	DockerEntrypoint        []string             `json:"dockerEntrypoint,omitempty"`
	DockerStartCmd          []string             `json:"dockerStartCmd,omitempty"`
	Env                     map[string]string    `json:"env,omitempty"`
	ContainerDiskInGb       int                  `json:"containerDiskInGb,omitempty"`
	ContainerRegistryAuthID string               `json:"containerRegistryAuthId,omitempty"`
	VolumeInGb              int                  `json:"volumeInGb,omitempty"`
	VolumeMountPath         string               `json:"volumeMountPath,omitempty"`
	Readme                  string               `json:"readme,omitempty"`
}

type templateEnvPair struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type templatePorts []string

func (p *templatePorts) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return nil
		}
		parts := strings.Split(s, ",")
		ports := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			ports = append(ports, part)
		}
		*p = ports
		return nil
	}

	var ports []string
	if err := json.Unmarshal(data, &ports); err != nil {
		return err
	}
	*p = ports
	return nil
}

type templateGraphQL struct {
	ID                      string               `json:"id"`
	Name                    string               `json:"name"`
	ImageName               string               `json:"imageName"`
	IsServerless            bool                 `json:"isServerless,omitempty"`
	IsPublic                bool                 `json:"isPublic,omitempty"`
	IsRunpod                bool                 `json:"isRunpod,omitempty"`
	Category                string               `json:"category,omitempty"`
	Ports                   templatePorts        `json:"ports,omitempty"`
	PortsConfig             []TemplatePortConfig `json:"portsConfig,omitempty"`
	Env                     []templateEnvPair    `json:"env,omitempty"`
	ContainerDiskInGb       int                  `json:"containerDiskInGb,omitempty"`
	ContainerRegistryAuthID string               `json:"containerRegistryAuthId,omitempty"`
	VolumeInGb              int                  `json:"volumeInGb,omitempty"`
	VolumeMountPath         string               `json:"volumeMountPath,omitempty"`
	Readme                  string               `json:"readme,omitempty"`
}

// v2Template is a template as rest v2 reports it. toTemplate maps it onto the
// field names the cli has always printed; readme, portsConfig and isRunpod have
// no v2 field and are filled in from graphql where the cli shows them.
type v2Template struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Image      string            `json:"image"`
	Serverless bool              `json:"serverless"`
	Public     bool              `json:"public"`
	Category   string            `json:"category"`
	Ports      []string          `json:"ports"`
	Env        map[string]string `json:"env"`
	Disk       int               `json:"disk"`
	Registry   *string           `json:"registry"`
	Mounts     v2TemplateMounts  `json:"mounts"`
	Entrypoint []string          `json:"entrypoint"`
	Cmd        []string          `json:"cmd"`
}

type v2PersistentMount struct {
	Size int    `json:"size"`
	Path string `json:"path"`
}

type v2TemplateMounts struct {
	Persistent *v2PersistentMount `json:"persistent,omitempty"`
}

func (t *v2Template) toTemplate() Template {
	out := Template{
		ID:                t.ID,
		Name:              t.Name,
		ImageName:         t.Image,
		IsServerless:      t.Serverless,
		IsPublic:          t.Public,
		Category:          t.Category,
		Ports:             t.Ports,
		DockerEntrypoint:  t.Entrypoint,
		DockerStartCmd:    t.Cmd,
		ContainerDiskInGb: t.Disk,
	}
	if len(t.Env) > 0 {
		out.Env = t.Env
	}
	if t.Registry != nil {
		out.ContainerRegistryAuthID = *t.Registry
	}
	if p := t.Mounts.Persistent; p != nil {
		out.VolumeInGb = p.Size
		out.VolumeMountPath = p.Path
	}
	return out
}

func parseV2Template(data []byte) (*Template, error) {
	var t v2Template
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	out := t.toTemplate()
	return &out, nil
}

type v2TemplateList struct {
	Templates []v2Template `json:"templates"`
}

// TemplateType for filtering
type TemplateType string

const (
	TemplateTypeAll       TemplateType = "all"
	TemplateTypeOfficial  TemplateType = "official"
	TemplateTypeCommunity TemplateType = "community"
	TemplateTypeUser      TemplateType = "user"
)

// TemplateListOptions for listing templates
type TemplateListOptions struct {
	Type   TemplateType
	Search string // search term to filter by name/image
	Limit  int
	Offset int
}

// TemplateListResponse is the response from listing templates
type TemplateListResponse struct {
	Templates []Template `json:"templates"`
}

// TemplateCreateRequest is the request to create a template
type TemplateCreateRequest struct {
	Name                    string            `json:"name"`
	ImageName               string            `json:"imageName"`
	IsServerless            bool              `json:"isServerless,omitempty"`
	Ports                   []string          `json:"ports,omitempty"`
	DockerEntrypoint        []string          `json:"dockerEntrypoint,omitempty"`
	DockerStartCmd          []string          `json:"dockerStartCmd,omitempty"`
	Env                     map[string]string `json:"env,omitempty"`
	ContainerDiskInGb       int               `json:"containerDiskInGb,omitempty"`
	ContainerRegistryAuthID string            `json:"containerRegistryAuthId,omitempty"`
	VolumeInGb              int               `json:"volumeInGb,omitempty"`
	VolumeMountPath         string            `json:"volumeMountPath,omitempty"`
	Readme                  string            `json:"readme,omitempty"`
}

// TemplateUpdateRequest is the request to update a template
type TemplateUpdateRequest struct {
	Name              string            `json:"name,omitempty"`
	ImageName         string            `json:"imageName,omitempty"`
	Ports             []string          `json:"ports,omitempty"`
	Env               map[string]string `json:"env,omitempty"`
	Readme            string            `json:"readme,omitempty"`
	ContainerDiskInGb *int              `json:"containerDiskInGb,omitempty"`
	// ContainerRegistryAuthID is a pointer so an empty string can clear the auth.
	ContainerRegistryAuthID *string `json:"containerRegistryAuthId,omitempty"`
}

// ListTemplates returns templates (user's own via REST API)
func (c *Client) ListTemplates() ([]Template, error) {
	templates := []Template{}
	err := c.getV2AllPages("/templates", nil, func(data []byte) error {
		var page v2TemplateList
		if err := json.Unmarshal(data, &page); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}
		for i := range page.Templates {
			templates = append(templates, page.Templates[i].toTemplate())
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// v1 listed templates by name, ignoring case; v2 lists newest first. keep
	// v1's order so --limit/--offset page through the same templates.
	sort.SliceStable(templates, func(i, j int) bool {
		return strings.ToLower(templates[i].Name) < strings.ToLower(templates[j].Name)
	})
	return templates, nil
}

// rp-migrate: keep-v1 start
// backfillUserTemplateReadmes fills in readme, which rest v2 has no field for,
// from one graphql read of the user's templates. best-effort: a failure leaves
// the readmes empty rather than failing the listing.
func (c *Client) backfillUserTemplateReadmes(templates []Template) {
	if len(templates) == 0 {
		return
	}
	data, err := c.graphqlRequest(`query { myself { podTemplates { id readme } } }`, nil)
	if err != nil {
		return
	}
	var resp struct {
		Data struct {
			Myself struct {
				PodTemplates []struct {
					ID     string `json:"id"`
					Readme string `json:"readme"`
				} `json:"podTemplates"`
			} `json:"myself"`
		} `json:"data"`
	}
	if json.Unmarshal(data, &resp) != nil {
		return
	}
	readmes := make(map[string]string, len(resp.Data.Myself.PodTemplates))
	for _, t := range resp.Data.Myself.PodTemplates {
		readmes[t.ID] = t.Readme
	}
	for i := range templates {
		templates[i].Readme = readmes[templates[i].ID]
	}
}

// rp-migrate: keep-v1 end

// listOfficialTemplates reads runpod's official templates from the v2 catalog.
func (c *Client) listOfficialTemplates() ([]Template, error) {
	data, err := c.GetV2("/catalog/templates", url.Values{"source": {"official"}})
	if err != nil {
		return nil, err
	}
	var list v2TemplateList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	templates := make([]Template, 0, len(list.Templates))
	for i := range list.Templates {
		t := list.Templates[i].toTemplate()
		t.IsRunpod = true
		templates = append(templates, listingFields(t))
	}
	return templates, nil
}

// listingFields keeps the summary fields the official/community listing has
// always printed (the graphql podTemplates selection); ports, env and the start
// command are only shown by `template get`.
func listingFields(t Template) Template {
	return Template{
		ID:                t.ID,
		Name:              t.Name,
		ImageName:         t.ImageName,
		IsServerless:      t.IsServerless,
		IsPublic:          t.IsPublic,
		IsRunpod:          t.IsRunpod,
		Category:          t.Category,
		ContainerDiskInGb: t.ContainerDiskInGb,
		VolumeInGb:        t.VolumeInGb,
		VolumeMountPath:   t.VolumeMountPath,
	}
}

// ListAllTemplates returns templates based on filter options
// Uses the v2 catalog for official, GraphQL podTemplates(input:) for community,
// and rest v2 for user templates
//
// Default behavior (no type specified): official + community templates
// --type official: only RunPod official templates
// --type community: only community templates
// --type user: only user's own templates
// --all: everything including user templates
func (c *Client) ListAllTemplates(opts *TemplateListOptions) ([]Template, error) {
	query := `
		query PodTemplates($input: PodTemplateInput) {
			podTemplates(input: $input) {
				id
				name
				imageName
				isServerless
				isPublic
				isRunpod
				category
				containerDiskInGb
				volumeInGb
				volumeMountPath
			}
		}
	`

	var allTemplates []Template

	// Determine what to fetch based on type filter
	// Default (no type): official + community (NOT user - they need to explicitly ask)
	fetchOfficial := opts == nil || opts.Type == "" || opts.Type == TemplateTypeAll || opts.Type == TemplateTypeOfficial
	fetchCommunity := opts == nil || opts.Type == "" || opts.Type == TemplateTypeAll || opts.Type == TemplateTypeCommunity
	fetchUser := opts != nil && (opts.Type == TemplateTypeAll || opts.Type == TemplateTypeUser)

	// Fetch official Runpod templates FIRST, from the v2 catalog
	if fetchOfficial && (opts == nil || opts.Type != TemplateTypeCommunity) {
		if official, err := c.listOfficialTemplates(); err == nil {
			allTemplates = append(allTemplates, official...)
		}
	}

	// Fetch community templates SECOND. these stay on graphql: the v2 catalog's
	// community set is a capped, curated ~100, against ~1,200 here.
	// rp-migrate: keep-v1 start
	if fetchCommunity && (opts == nil || opts.Type != TemplateTypeOfficial) {
		variables := map[string]interface{}{
			"input": map[string]interface{}{
				"isRunpod": false,
			},
		}
		data, err := c.graphqlRequest(query, variables)
		if err == nil {
			var resp struct {
				Data struct {
					PodTemplates []Template `json:"podTemplates"`
				} `json:"data"`
			}
			if json.Unmarshal(data, &resp) == nil {
				allTemplates = append(allTemplates, resp.Data.PodTemplates...)
			}
		}
	}
	// rp-migrate: keep-v1 end

	// Fetch user's own templates LAST
	if fetchUser {
		userTemplates, err := c.ListTemplates()
		if err == nil {
			c.backfillUserTemplateReadmes(userTemplates)
			allTemplates = append(allTemplates, userTemplates...)
		}
	}

	// Apply search filter (client-side, matching runpod-assistant behavior)
	if opts != nil && opts.Search != "" {
		searchTerm := strings.ToLower(opts.Search)
		var filtered []Template
		for _, t := range allTemplates {
			if strings.Contains(strings.ToLower(t.ID), searchTerm) ||
				strings.Contains(strings.ToLower(t.Name), searchTerm) ||
				strings.Contains(strings.ToLower(t.ImageName), searchTerm) {
				filtered = append(filtered, t)
			}
		}
		allTemplates = filtered
	}

	// Apply pagination
	if opts != nil {
		if opts.Offset > 0 && opts.Offset < len(allTemplates) {
			allTemplates = allTemplates[opts.Offset:]
		}
		if opts.Limit > 0 && opts.Limit < len(allTemplates) {
			allTemplates = allTemplates[:opts.Limit]
		}
	}

	return allTemplates, nil
}

// GetTemplate returns a single template by ID. rest v2 serves the user's own
// templates and public ones alike; readme, portsConfig and isRunpod have no v2
// field, so they are read from graphql (best-effort, as the label backfill always
// was). a template v2 cannot read falls back to graphql entirely.
func (c *Client) GetTemplate(templateID string) (*Template, error) {
	data, err := c.GetV2("/templates/"+url.PathEscape(templateID), nil)
	if err != nil {
		// rp-migrate: keep-v1
		return c.getTemplateByIDGraphQL(templateID)
	}
	template, err := parseV2Template(data)
	if err != nil {
		return nil, err
	}
	// rp-migrate: keep-v1 start
	if gql, gqlErr := c.getTemplateByIDGraphQL(templateID); gqlErr == nil {
		template.Readme = gql.Readme
		template.PortsConfig = gql.PortsConfig
		template.IsRunpod = gql.IsRunpod
	}
	// rp-migrate: keep-v1 end
	return template, nil
}

// getTemplateByIDGraphQL retrieves a template by ID using GraphQL
func (c *Client) getTemplateByIDGraphQL(templateID string) (*Template, error) {
	query := `
		query GetTemplate($id: String!) {
			podTemplate(id: $id) {
				id
				name
				imageName
				isServerless
				isPublic
				isRunpod
				category
				ports
				portsConfig {
					port
					name
				}
				env {
					key
					value
				}
				containerDiskInGb
				containerRegistryAuthId
				volumeInGb
				volumeMountPath
				readme
			}
		}
	`

	variables := map[string]interface{}{
		"id": templateID,
	}

	data, err := c.graphqlRequest(query, variables)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			PodTemplate *templateGraphQL `json:"podTemplate"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(resp.Errors) > 0 {
		return nil, newGraphQLError(resp.Errors[0].Message)
	}

	if resp.Data.PodTemplate == nil {
		return nil, NewNotFoundError("template not found: %s", templateID)
	}

	return templateFromGraphQL(resp.Data.PodTemplate), nil
}

func templateFromGraphQL(source *templateGraphQL) *Template {
	if source == nil {
		return nil
	}

	template := &Template{
		ID:                      source.ID,
		Name:                    source.Name,
		ImageName:               source.ImageName,
		IsServerless:            source.IsServerless,
		IsPublic:                source.IsPublic,
		IsRunpod:                source.IsRunpod,
		Category:                source.Category,
		Ports:                   []string(source.Ports),
		PortsConfig:             source.PortsConfig,
		ContainerDiskInGb:       source.ContainerDiskInGb,
		ContainerRegistryAuthID: source.ContainerRegistryAuthID,
		VolumeInGb:              source.VolumeInGb,
		VolumeMountPath:         source.VolumeMountPath,
		Readme:                  source.Readme,
	}

	if len(source.Env) > 0 {
		env := make(map[string]string, len(source.Env))
		for _, pair := range source.Env {
			if pair.Key == "" {
				continue
			}
			env[pair.Key] = pair.Value
		}
		if len(env) > 0 {
			template.Env = env
		}
	}

	return template
}

type v2TemplateWrite struct {
	Name       string            `json:"name,omitempty"`
	Image      string            `json:"image,omitempty"`
	Serverless *bool             `json:"serverless,omitempty"`
	Ports      []string          `json:"ports,omitempty"`
	Entrypoint []string          `json:"entrypoint,omitempty"`
	Cmd        []string          `json:"cmd,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	Disk       *int              `json:"disk,omitempty"`
	// an empty string clears the registry. v2 ignores null here, despite the
	// schema allowing it (observed 2026-10-02).
	Registry *string           `json:"registry,omitempty"`
	Mounts   *v2TemplateMounts `json:"mounts,omitempty"`
}

// v1DefaultTemplatePorts is what v1 gave a template created without ports. v2
// defaults to none, so the cli sends v1's default to keep `template create`
// without --ports behaving as before.
var v1DefaultTemplatePorts = []string{"8888/http", "22/tcp"}

// CreateTemplate creates a new template. Readme has no v2 field; callers set it
// over graphql (GraphQLClient.UpdateTemplateReadme).
func (c *Client) CreateTemplate(req *TemplateCreateRequest) (*Template, error) {
	ports := req.Ports
	if len(ports) == 0 {
		ports = v1DefaultTemplatePorts
	}
	body := &v2TemplateWrite{
		Name:       req.Name,
		Image:      req.ImageName,
		Serverless: &req.IsServerless,
		Ports:      ports,
		Entrypoint: req.DockerEntrypoint,
		Cmd:        req.DockerStartCmd,
		Env:        req.Env,
	}
	if req.ContainerDiskInGb > 0 {
		body.Disk = &req.ContainerDiskInGb
	}
	if req.ContainerRegistryAuthID != "" {
		body.Registry = &req.ContainerRegistryAuthID
	}
	if req.VolumeInGb > 0 {
		body.Mounts = &v2TemplateMounts{Persistent: &v2PersistentMount{Size: req.VolumeInGb, Path: req.VolumeMountPath}}
	}
	data, err := c.PostV2("/templates", body)
	if err != nil {
		return nil, err
	}
	return parseV2Template(data)
}

// UpdateTemplate updates an existing template. Readme has no v2 field; callers
// set it over graphql (GraphQLClient.UpdateTemplateReadme).
func (c *Client) UpdateTemplate(templateID string, req *TemplateUpdateRequest) (*Template, error) {
	body := &v2TemplateWrite{
		Name:  req.Name,
		Image: req.ImageName,
		Ports: req.Ports,
		Env:   req.Env,
		Disk:  req.ContainerDiskInGb,
	}
	if req.ContainerRegistryAuthID != nil {
		body.Registry = req.ContainerRegistryAuthID
	}
	data, err := c.PatchV2("/templates/"+url.PathEscape(templateID), body)
	if err != nil {
		return nil, err
	}
	return parseV2Template(data)
}

// DeleteTemplate deletes a template
func (c *Client) DeleteTemplate(templateID string) error {
	_, err := c.DeleteV2("/templates/" + url.PathEscape(templateID))
	return err
}
