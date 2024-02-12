package project

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

func generateProjectToml(projectFolder, filename, projectName, cudaVersion, pythonVersion string) {
	template := `# RunPod Project Configuration


name = "%s"


[project]
uuid = "%s" # Unique identifier for the project. Generated automatically.

# Base Docker image used for the project environment. Includes essential packages and CUDA support.
# Use 'runpod/base' as a starting point. Customize only if you need additional packages or configurations.
base_image = "runpod/base:0.5.0-cuda%s"

# List of preferred GPU types for your development pod, ordered by priority.
# The pod will use the first available type from this list.
# For a full list of supported GPU types, visit: https://docs.runpod.io/references/gpu-types
gpu_types = [
    "NVIDIA GeForce RTX 4080",  # 16GB
    "NVIDIA RTX A4000",         # 16GB
    "NVIDIA RTX A4500",         # 20GB
    "NVIDIA RTX A5000",         # 24GB
    "NVIDIA GeForce RTX 3090",  # 24GB
    "NVIDIA GeForce RTX 4090",  # 24GB
    "NVIDIA RTX A6000",         # 48GB
    "NVIDIA A100 80GB PCIe",    # 80GB
]

gpu_count = 1

# Default volume mount path in serverless environment. Changing this may affect data persistence.
volume_mount_path = "/runpod-volume"

# Ports to expose and their protocols. Configure as needed for your application's requirements.
# The base image uses 4040 for FileBrowser, 8080 for FastAPI and 22 for SSH
ports = "4040/http, 8080/http, 22/tcp"

# Disk space allocated for the container. Adjust according to your project's needs.
container_disk_size_gb = 100


[project.env_vars]
# Environment variables for the pod.

# Duration (in seconds) before terminating the pod after the last SSH session ends.
POD_INACTIVITY_TIMEOUT = "120"

RUNPOD_DEBUG_LEVEL = "debug"
UVICORN_LOG_LEVEL = "warning"

# Configurations for caching Hugging Face models and datasets to improve load times and reduce bandwidth.
HF_HOME = "/runpod-volume/.cache/huggingface/"
HF_DATASETS_CACHE = "/runpod-volume/.cache/huggingface/datasets/"
DEFAULT_HF_METRICS_CACHE = "/runpod-volume/.cache/huggingface/metrics/"
DEFAULT_HF_MODULES_CACHE = "/runpod-volume/.cache/huggingface/modules/"
HUGGINGFACE_HUB_CACHE = "/runpod-volume/.cache/huggingface/hub/"
HUGGINGFACE_ASSETS_CACHE = "/runpod-volume/.cache/huggingface/assets/"

# Enable this to use the HF Hub transfer service for faster Hugging Face downloads.
HF_HUB_ENABLE_HF_TRANSFER = "1" # Requires 'hf_transfer' Python package.

# Directories for caching Python dependencies, speeding up subsequent installations.
VIRTUALENV_OVERRIDE_APP_DATA = "/runpod-volume/.cache/virtualenv/"
PIP_CACHE_DIR = "/runpod-volume/.cache/pip/"


[runtime]
# Runtime configuration for the project.

python_version = "%s"
handler_path = "src/handler.py"
requirements_path = "builder/requirements.txt"
`

	// Format the template with dynamic content
	content := fmt.Sprintf(template, projectName, uuid.New().String()[0:8], cudaVersion, pythonVersion)

	// Write the content to a TOML file
	tomlPath := filepath.Join(projectFolder, filename)
	err := os.WriteFile(tomlPath, []byte(content), 0644)
	if err != nil {
		fmt.Printf("Failed to write the TOML file: %s\n", err)
	} else {
		fmt.Println("TOML file generated successfully with dynamic content.")
	}
}
