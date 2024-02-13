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
# uuid 					 - Unique identifier for the project. Generated automatically.
# volume_mount_path 	 - Default volume mount path in serverless environment. Changing this may affect data persistence.
# base_image 			 - Base Docker image used for the project environment. Includes essential packages and CUDA support.
#              		  	   Use 'runpod/base' as a starting point. Customize only if you need additional packages or configurations.
# gpu_types 			 - List of preferred GPU types for your development pod, ordered by priority.
#             		       The pod will use the first available type from this list.
#             		       For a full list of supported GPU types, visit: https://docs.runpod.io/references/gpu-types
# gpu_count 			 - Number of GPUs to allocate for the pod.
# volume_mount_path 	 - Default volume mount path in serverless environment. Changing this may affect data persistence.
# ports 				 - Ports to expose and their protocols. Configure as needed for your application's requirements.
# container_disk_size_gb - Disk space allocated for the container. Adjust according to your project's needs.

uuid = "%s"
base_image = "runpod/base:0.5.0-cuda%s"
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
volume_mount_path = "/runpod-volume"
ports = "4040/http, 8080/http, 22/tcp" # FileBrowser, FastAPI, SSH
container_disk_size_gb = 100

[project.env_vars]
# Environment variables for the pod.
# For full list of base environment variables, visit: https://github.com/runpod/containers/blob/main/official-templates/base/Dockerfile
# POD_INACTIVITY_TIMEOUT - Duration (in seconds) before terminating the pod after the last SSH session ends.
# RUNPOD_DEBUG_LEVEL 	 - Log level for RunPod. Set to 'debug' for detailed logs.
# UVICORN_LOG_LEVEL 	 - Log level for Uvicorn. Set to 'warning' for minimal logs.

POD_INACTIVITY_TIMEOUT = "120"
RUNPOD_DEBUG_LEVEL = "debug"
UVICORN_LOG_LEVEL = "warning"

[endpoint]
# Configuration for the deployed endpoint. 
# For full list of endpoint configurations, visit: https://docs.runpod.io/serverless/references/endpoint-configurations
# active_workers - The minimum number of workers your endpoint will have running at any given point.
#                  Setting this amount to 1 will result in "always on" workers. 
#                  This will allow you to have a worker ready to respond to job requests without incurring any cold start delay.
# max_workers    - The maximum number of workers your endpoint will have running at any given point.

active_workers = 0
max_workers = 3
flashboot = true

[runtime]
# python_version 	- Python version to use for the project.
# handler_path 		- Path to the handler file for the project.
# requirements_path - Path to the requirements file for the project.

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
	}
}
