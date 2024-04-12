package project

// Returns default model name for the example model type
func getDefaultModelName(modelType string) string {
	switch modelType {
	case "LLM":
		return "google/flan-t5-base"
	case "Stable_Diffusion":
		return "stabilityai/sdxl-turbo"
	case "Text_to_Audio":
		return "facebook/musicgen-small"
	}

	return ""
}
