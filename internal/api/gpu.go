package api

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/runpod/runpodctl/internal/configenv"
)

// GpuType represents a GPU type
type GpuType struct {
	ID             string `json:"id"`
	DisplayName    string `json:"displayName"`
	MemoryInGb     int    `json:"memoryInGb"`
	SecureCloud    bool   `json:"secureCloud"`
	CommunityCloud bool   `json:"communityCloud"`
	// the user-facing names (securePricePerHr / communityPricePerHr) are set by
	// cmd/gpu's own output struct, which is the only thing that marshals gpu data.
	SecurePrice    float64 `json:"securePrice"`
	CommunityPrice float64 `json:"communityPrice"`
}

// GpuDataCenterAvailability is a GPU's stock status in a single data center.
type GpuDataCenterAvailability struct {
	DataCenterID string `json:"dataCenterId"`
	StockStatus  string `json:"stockStatus"`
}

// GpuTypeWithAvailability includes availability info
type GpuTypeWithAvailability struct {
	GpuType
	// StockStatus is the best availability across all data centers.
	StockStatus string `json:"stockStatus,omitempty"`
	Available   bool   `json:"available"`
	// DataCenterAvailability breaks availability down per data center so agents
	// can pick a location that will actually schedule.
	DataCenterAvailability []GpuDataCenterAvailability `json:"dataCenterAvailability,omitempty"`
}

// DataCenter represents a data center
type DataCenter struct {
	ID              string                        `json:"id"`
	Name            string                        `json:"name"`
	Location        string                        `json:"location"`
	GpuAvailability []GpuAvailabilityInDataCenter `json:"gpuAvailability,omitempty"`
}

// GpuAvailabilityInDataCenter represents GPU availability in a datacenter
type GpuAvailabilityInDataCenter struct {
	GpuTypeID   string `json:"gpuTypeId"`
	DisplayName string `json:"displayName"`
	StockStatus string `json:"stockStatus"`
}

// User represents user account info
type User struct {
	ID                string  `json:"id"`
	Email             string  `json:"email"`
	ClientBalance     float64 `json:"clientBalance"`
	CurrentSpendPerHr float64 `json:"currentSpendPerHr"`
	SpendLimit        float64 `json:"spendLimit"`
	NotifyPodsStale   bool    `json:"notifyPodsStale"`
	NotifyPodsGeneral bool    `json:"notifyPodsGeneral"`
	NotifyLowBalance  bool    `json:"notifyLowBalance"`
}

// graphqlRequest makes a GraphQL request
func (c *Client) graphqlRequest(query string, variables map[string]interface{}) ([]byte, error) {
	apiURL := configenv.GraphQLURL()
	if apiURL == "" {
		apiURL = "https://api.runpod.io/graphql"
	}

	// temporarily swap base URL for GraphQL
	origBaseURL := c.baseURL
	c.baseURL = apiURL
	defer func() { c.baseURL = origBaseURL }()

	body := map[string]interface{}{
		"query":     query,
		"variables": variables,
	}

	return c.Post("", body)
}

// v2CatalogGpu is a gpu type from GET /v2/catalog/gpus.
type v2CatalogGpu struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Memory       int    `json:"memory"`
	Secure       bool   `json:"secure"`
	Community    bool   `json:"community"`
	Pool         string `json:"pool"`
	Availability string `json:"availability"`
	Price        struct {
		Secure    float64 `json:"secure"`
		Community float64 `json:"community"`
	} `json:"price"`
	DataCenters []struct {
		ID           string `json:"id"`
		Availability string `json:"availability"`
	} `json:"dataCenters"`
}

func (c *Client) listCatalogGpus(params url.Values) ([]v2CatalogGpu, error) {
	data, err := c.GetV2("/catalog/gpus", params)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Gpus []v2CatalogGpu `json:"gpus"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return resp.Gpus, nil
}

// legacyStockStatus spells a v2 availability (HIGH/MEDIUM/LOW/NONE) the way the
// cli has always printed it (High/Medium/Low), so agents comparing against
// those values keep matching.
func legacyStockStatus(availability string) string {
	switch strings.ToUpper(availability) {
	case "HIGH":
		return "High"
	case "MEDIUM":
		return "Medium"
	case "LOW":
		return "Low"
	case "NONE", "":
		return ""
	default:
		return availability
	}
}

// ListGpuTypes returns all available GPU types (filters out deprecated/unavailable),
// with pod availability from the v2 catalog.
func (c *Client) ListGpuTypes(includeUnavailable bool) ([]GpuTypeWithAvailability, error) {
	gpus, err := c.listCatalogGpus(url.Values{"include": {"AVAILABILITY"}, "product": {"POD"}})
	if err != nil {
		return nil, err
	}

	var result []GpuTypeWithAvailability
	for _, gpu := range gpus {
		// the overall status is the better of v2's top-level availability and the
		// best data center: the top level also counts community stock that has no
		// data center entry, but it can read NONE while a data center reads LOW
		// (observed 2026-10-03 for the RTX A4000 without a cloud filter).
		stockStatus := legacyStockStatus(gpu.Availability)
		var perDC []GpuDataCenterAvailability
		for _, dc := range gpu.DataCenters {
			stock := legacyStockStatus(dc.Availability)
			if betterStock(stock, stockStatus) {
				stockStatus = stock
			}
			if stock == "" {
				stock = "none"
			}
			perDC = append(perDC, GpuDataCenterAvailability{DataCenterID: dc.ID, StockStatus: stock})
		}
		available := hasStock(stockStatus)
		if !includeUnavailable && !available {
			continue
		}
		if gpu.ID == "unknown" {
			continue
		}
		result = append(result, GpuTypeWithAvailability{
			GpuType: GpuType{
				ID:             gpu.ID,
				DisplayName:    gpu.Name,
				MemoryInGb:     gpu.Memory,
				SecureCloud:    gpu.Secure,
				CommunityCloud: gpu.Community,
				SecurePrice:    gpu.Price.Secure,
				CommunityPrice: gpu.Price.Community,
			},
			StockStatus:            stockStatus,
			Available:              available,
			DataCenterAvailability: perDC,
		})
	}
	return result, nil
}

// stockOrder ranks the api's known stock levels. KEEP IN SYNC with the api
// enum; TestStockRank pins the known set so a new value shows up as a test
// failure in review rather than as a silently misranked gpu.
var stockOrder = map[string]int{"high": 4, "medium": 3, "low": 2}

// noStockStatuses are literal values that mean "offered here, nothing in stock".
// They must rank BELOW low rather than land in the unknown bucket: an unknown
// value ranks above absent, so a literal "none" would otherwise win the
// top-level stockStatus and make available report true for a gpu with no stock.
var noStockStatuses = map[string]bool{"none": true, "unavailable": true, "out of stock": true, "no stock": true}

// stockRank scores a stock status for comparison. An unrecognized non-empty
// status must outrank a genuinely absent one: previously every unknown value
// fell through a map lookup to 0 and therefore tied with "no stock", so a new
// api level (or a casing change) would lose to "Low" and the top-level
// stockStatus would under-report a gpu that is in fact available.
//
// An unknown value cannot be ordered against the known ones, so it sits just
// above "none". The per-datacenter breakdown carries every raw value, which is
// where an agent that needs the truth should look.
func stockRank(s string) int {
	normalized := strings.ToLower(strings.TrimSpace(s))
	if normalized == "" || noStockStatuses[normalized] {
		return 0
	}
	if rank, ok := stockOrder[normalized]; ok {
		return rank
	}
	return 1
}

// hasStock reports whether a stock status means anything is actually available.
// Kept next to stockRank so the two cannot disagree: rank 0 means "nothing here",
// and `available` must say the same.
func hasStock(status string) bool {
	return stockRank(status) > 0
}

func betterStock(a, b string) bool {
	return stockRank(a) > stockRank(b)
}

// ServerlessGpuPool is a serverless gpu pool. saveEndpoint's gpuIds field
// accepts pool ids (e.g. "ADA_24"), not the gpu type ids (e.g. "NVIDIA A40")
// that 'gpu list' and --gpu-id use, so we map between them with this.
type ServerlessGpuPool struct {
	ID         string   `json:"id"`
	GpuTypeIDs []string `json:"gpuTypeIds"`
}

// ListServerlessGpuPools returns the serverless gpu pools and their member
// gpu type ids, grouped from each catalog gpu's `pool`.
func (c *Client) ListServerlessGpuPools() ([]ServerlessGpuPool, error) {
	gpus, err := c.listCatalogGpus(nil)
	if err != nil {
		return nil, err
	}
	var pools []ServerlessGpuPool
	index := map[string]int{}
	for _, gpu := range gpus {
		if gpu.Pool == "" {
			continue
		}
		i, ok := index[gpu.Pool]
		if !ok {
			i = len(pools)
			index[gpu.Pool] = i
			pools = append(pools, ServerlessGpuPool{ID: gpu.Pool})
		}
		pools[i].GpuTypeIDs = append(pools[i].GpuTypeIDs, gpu.ID)
	}
	return pools, nil
}

// ResolveServerlessGpuPoolID maps a --gpu-id value to the gpu pool id(s) that
// saveEndpoint expects. it accepts a pool id (returned as-is), a gpu type id
// (translated to its pool id), or a comma-separated mix of those (e.g. a hub
// config's gpu list). a failed pools query is an error, not a fallback to the
// input: the value is usually a gpu type id, and passing one through
// unresolved gets the whole write rejected with `Invalid GPU Pool ID`, which
// masks the lookup failure that actually caused it.
func (c *Client) ResolveServerlessGpuPoolID(gpuID string) (string, error) {
	pools, err := c.ListServerlessGpuPools()
	if err != nil {
		return "", fmt.Errorf("failed to look up serverless gpu pools: %w", err)
	}

	poolIDs := make([]string, 0, len(pools))
	for _, p := range pools {
		poolIDs = append(poolIDs, p.ID)
	}
	sort.Strings(poolIDs) // the catalog's order is arbitrary

	resolveOne := func(id string) (string, bool) {
		for _, p := range pools {
			if strings.EqualFold(p.ID, id) {
				return p.ID, true
			}
		}
		for _, p := range pools {
			for _, t := range p.GpuTypeIDs {
				if strings.EqualFold(t, id) {
					return p.ID, true
				}
			}
		}
		return "", false
	}

	seen := make(map[string]bool)
	resolved := make([]string, 0)
	for _, tok := range strings.Split(gpuID, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		poolID, ok := resolveOne(tok)
		if !ok {
			return "", fmt.Errorf("unknown gpu id %q; use a gpu id from 'runpodctl gpu list' or a pool id (one of: %s)", tok, strings.Join(poolIDs, ", "))
		}
		if !seen[poolID] {
			seen[poolID] = true
			resolved = append(resolved, poolID)
		}
	}

	return strings.Join(resolved, ","), nil
}

// ListDataCenters returns the deployable data centers with GPU availability.
// v2 reports a region (e.g. NORTH_AMERICA) where graphql had a country-level
// location; it is carried in Location.
func (c *Client) ListDataCenters() ([]DataCenter, error) {
	data, err := c.GetV2("/catalog/datacenters", url.Values{"include": {"GPU_AVAILABILITY"}})
	if err != nil {
		return nil, err
	}
	var resp struct {
		DataCenters []struct {
			ID              string `json:"id"`
			Name            string `json:"name"`
			Region          string `json:"region"`
			GpuAvailability []struct {
				ID           string `json:"id"`
				Name         string `json:"name"`
				Availability string `json:"availability"`
			} `json:"gpuAvailability"`
		} `json:"dataCenters"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	dataCenters := make([]DataCenter, 0, len(resp.DataCenters))
	for _, dc := range resp.DataCenters {
		out := DataCenter{ID: dc.ID, Name: dc.Name, Location: dc.Region}
		for _, g := range dc.GpuAvailability {
			out.GpuAvailability = append(out.GpuAvailability, GpuAvailabilityInDataCenter{
				GpuTypeID:   g.ID,
				DisplayName: g.Name,
				StockStatus: legacyStockStatus(g.Availability),
			})
		}
		dataCenters = append(dataCenters, out)
	}
	return dataCenters, nil
}

// GetUser returns the current user's account info
func (c *Client) GetUser() (*User, error) {
	query := `
		query {
			myself {
				id
				email
				clientBalance
				currentSpendPerHr
				spendLimit
				notifyPodsStale
				notifyPodsGeneral
				notifyLowBalance
			}
		}
	`

	data, err := c.graphqlRequest(query, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			Myself *User `json:"myself"`
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

	return resp.Data.Myself, nil
}
