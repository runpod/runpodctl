package billing

import "strings"

func normalizeGpuGrouping(value string) string {
	normalized := strings.TrimSpace(value)
	switch strings.ToLower(normalized) {
	case "gpuid", "gpu-id", "gpu_type_id", "gpu-type-id", "gputypeid":
		return "gpuTypeId"
	default:
		return value
	}
}
