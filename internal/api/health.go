package api

// EndpointHealth is the live readiness snapshot for a serverless endpoint.
//
// It is served by the invoke service (api.runpod.ai/v2/<id>/health), not the
// rest control plane, so it follows RUNPOD_INVOKE_URL rather than
// RUNPOD_API_URL — same rule as the invoke urls reported by serverless
// create/get/list.
type EndpointHealth struct {
	Jobs    EndpointHealthJobs    `json:"jobs"`
	Workers EndpointHealthWorkers `json:"workers"`
}

// EndpointHealthJobs are the endpoint's job counters.
type EndpointHealthJobs struct {
	Completed  int `json:"completed"`
	Failed     int `json:"failed"`
	InProgress int `json:"inProgress"`
	InQueue    int `json:"inQueue"`
	Retried    int `json:"retried"`
}

// EndpointHealthWorkers are the endpoint's live worker counts. Ready/Running are
// the only two that mean "this endpoint can serve a request right now".
type EndpointHealthWorkers struct {
	Idle         int `json:"idle"`
	Initializing int `json:"initializing"`
	Ready        int `json:"ready"`
	Running      int `json:"running"`
	Throttled    int `json:"throttled"`
	Unhealthy    int `json:"unhealthy"`
}
