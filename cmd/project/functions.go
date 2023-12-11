package project

import (
	"cli/api"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/pelletier/go-toml"
)

//go:embed starter_templates/*
var starterTemplates embed.FS

//go:embed example.toml
var tomlTemplate embed.FS

const basePath string = "starter_templates"

func baseDockerImage(cudaVersion string) string {
	return fmt.Sprintf("runpod/base:0.4.0-cuda%s", cudaVersion)
}

func copyFiles(files fs.FS, source string, dest string) error {
	return fs.WalkDir(starterTemplates, source, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Skip the base directory
		if path == source {
			return nil
		}
		// Generate the corresponding path in the new project folder
		newPath := filepath.Join(dest, path[len(source):])
		if d.IsDir() {
			return os.MkdirAll(newPath, os.ModePerm)
		} else {
			content, err := fs.ReadFile(starterTemplates, path)
			if err != nil {
				return err
			}
			return os.WriteFile(newPath, content, 0644)
		}
	})
}
func createNewProject(projectName string, networkVolumeId string, cudaVersion string,
	pythonVersion string, modelType string, modelName string, initCurrentDir bool) {
	projectFolder, _ := os.Getwd()
	if !initCurrentDir {
		projectFolder = filepath.Join(projectFolder, projectName)
		_, err := os.Stat(projectFolder)
		if os.IsNotExist(err) {
			os.Mkdir(projectFolder, 0755)
		}
		if modelType == "" {
			modelType = "default"
		}
		templatePath := filepath.Join(basePath, modelType)
		//load selected starter template
		err = copyFiles(starterTemplates, templatePath, projectFolder)
		if err != nil {
			panic(err)
		}
		requirementsPath := filepath.Join(projectFolder, "builder", "requirements.txt")
		requirementsContentBytes, _ := os.ReadFile(requirementsPath)
		requirementsContent := string(requirementsContentBytes)
		//in requirements, replace <<RUNPOD>> with runpod-python import
		//TODO determine version to lock runpod-python at
		requirementsContent = strings.ReplaceAll(requirementsContent, "<<RUNPOD>>", "runpod")
		os.WriteFile(requirementsPath, []byte(requirementsContent), 0644)
	}
	//generate project toml
	tomlBytes, _ := tomlTemplate.ReadFile("example.toml")
	projectToml, _ := toml.LoadBytes(tomlBytes)
	projectUuid := uuid.New().String()[0:8]
	projectToml.SetComment("RunPod Project Configuration") //TODO why does this not appear
	projectToml.SetPath([]string{"title"}, projectName)
	projectToml.SetPath([]string{"project", "name"}, projectName)
	projectToml.SetPath([]string{"project", "uuid"}, projectUuid)
	projectToml.SetPath([]string{"project", "base_image"}, baseDockerImage(cudaVersion))
	projectToml.SetPath([]string{"project", "storage_id"}, networkVolumeId)
	projectToml.SetPath([]string{"template", "model_type"}, modelType)
	projectToml.SetPath([]string{"template", "model_name"}, modelName)
	projectToml.SetPath([]string{"runtime", "python_version"}, pythonVersion)
	tomlPath := filepath.Join(projectFolder, "runpod.toml")
	os.WriteFile(tomlPath, []byte(projectToml.String()), 0644)
}

func loadProjectConfig() *toml.Tree {
	projectFolder, _ := os.Getwd()
	tomlPath := filepath.Join(projectFolder, "runpod.toml")
	toml, err := toml.LoadFile(tomlPath)
	if err != nil {
		panic("runpod.toml not found in the current directory.")
	}
	return toml

}

func getProjectPod(projectId string) (string, error) {
	pods, err := api.GetPods()
	if err != nil {
		return "", err
	}
	for _, pod := range pods {
		if strings.Contains(pod.Name, projectId) {
			return pod.Id, nil
		}
	}
	return "", nil
}

func attemptPodLaunch(config *toml.Tree, environmentVariables map[string]string, selectedGpuTypes []string) (pod map[string]interface{}, err error) {
	projectConfig := config.Get("project").(*toml.Tree)
	//attempt to launch a pod with the given configuration.
	for _, gpuType := range selectedGpuTypes {
		fmt.Printf("Trying to get a pod with %s... ", gpuType)
		podEnv := []*api.PodEnv{}
		for k, v := range environmentVariables {
			podEnv = append(podEnv, &api.PodEnv{Key: k, Value: v})
		}
		input := api.CreatePodInput{
			CloudType:         "ALL",
			ContainerDiskInGb: int(projectConfig.Get("container_disk_size_gb").(int64)),
			// DeployCost:      projectConfig.Get(""),
			DockerArgs:      "",
			Env:             podEnv,
			GpuCount:        int(projectConfig.Get("gpu_count").(int64)),
			GpuTypeId:       gpuType,
			ImageName:       projectConfig.Get("base_image").(string),
			MinMemoryInGb:   1,
			MinVcpuCount:    1,
			Name:            fmt.Sprintf("%s-dev (%s)", projectConfig.Get("name"), projectConfig.Get("uuid")),
			NetworkVolumeId: projectConfig.Get("storage_id").(string),
			Ports:           strings.ReplaceAll(projectConfig.Get("ports").(string), " ", ""),
			SupportPublicIp: true,
			StartSSH:        true,
			// TemplateId:      projectConfig.Get(""),
			VolumeInGb:      0,
			VolumeMountPath: projectConfig.Get("volume_mount_path").(string),
		}
		pod, err := api.CreatePod(&input)
		if err != nil {
			fmt.Println("Unavailable.")
			continue
		}
		fmt.Println("Success!")
		return pod, nil
	}
	return nil, errors.New("none of the selected GPU types were available")
}

func launchDevPod(config *toml.Tree) (string, error) {
	fmt.Println("Deploying development pod on RunPod...")
	//construct env vars
	environmentVariables := map[string]string{}
	tomlEnvVars := config.GetPath([]string{"project", "env_vars"})
	if tomlEnvVars != nil {
		tomlEnvVarsMap := tomlEnvVars.(*toml.Tree).ToMap()
		for k, v := range tomlEnvVarsMap {
			environmentVariables[k] = v.(string)
		}
	}
	environmentVariables["RUNPOD_PROJECT_ID"] = config.GetPath([]string{"project", "uuid"}).(string)
	// prepare gpu types
	selectedGpuTypes := []string{}
	tomlGpuTypes := config.GetPath([]string{"project", "gpu_types"})
	if tomlGpuTypes != nil {
		for _, v := range tomlGpuTypes.([]interface{}) {
			selectedGpuTypes = append(selectedGpuTypes, v.(string))
		}
	}
	tomlGpu := config.GetPath([]string{"project", "gpu"}) //legacy
	if tomlGpu != nil {
		selectedGpuTypes = append(selectedGpuTypes, tomlGpu.(string))
	}
	// attempt to launch a pod with the given configuration
	new_pod, err := attemptPodLaunch(config, environmentVariables, selectedGpuTypes)
	if err != nil {
		fmt.Println(err)
		return "", err
	}
	return new_pod["id"].(string), nil
}

func startProject() error {
	//parse project toml
	config := loadProjectConfig()
	fmt.Println(config)
	projectId := config.GetPath([]string{"project", "uuid"}).(string)
	projectName := config.GetPath([]string{"project", "name"}).(string)
	//check for existing pod
	projectPodId, err := getProjectPod(projectId)
	if projectPodId == "" || err != nil {
		//or try to get pod with one of gpu types
		projectPodId, err = launchDevPod(config)
		if err != nil {
			return err
		}
	}
	//open ssh connection
	sshConn, err := PodSSHConnection(projectPodId)
	if err != nil {
		fmt.Println("error establishing ssh connection to pod: ", err)
		return err
	}
	fmt.Println(fmt.Sprintf("Project %s pod (%s) created.", projectName, projectPodId))
	//create remote folder structure
	projectConfig := config.Get("project").(*toml.Tree)
	projectPathUuid := filepath.Join(projectConfig.Get("volume_mount_path").(string), projectConfig.Get("uuid").(string))
	projectPathUuidDev := filepath.Join(projectPathUuid, "dev")
	projectPathUuidProd := filepath.Join(projectPathUuid, "prod")
	remoteProjectPath := filepath.Join(projectPathUuidDev, projectConfig.Get("name").(string))
	fmt.Printf("Checking pod project folder: %s on pod %s\n", remoteProjectPath, projectPodId)
	sshConn.RunCommands([]string{fmt.Sprintf("mkdir -p %s %s", remoteProjectPath, projectPathUuidProd)})
	//rsync project files
	fmt.Printf("Syncing files to pod %s\n", projectPodId)
	cwd, _ := os.Getwd()
	sshConn.Rsync(cwd, projectPathUuidDev, false)
	//activate venv on remote
	venvPath := filepath.Join(projectPathUuidDev, "venv")
	sshConn.RunCommands([]string{
		fmt.Sprintf("python%s -m venv %s", config.GetPath([]string{"runtime", "python_version"}).(string), venvPath),
		fmt.Sprintf(`source %s/bin/activate && 
		cd %s && 
		python -m pip install --upgrade pip && 
		python -m pip install -v --requirement %s`,
			venvPath, remoteProjectPath, config.GetPath([]string{"runtime", "requirements_path"}).(string)),
	})
	//create file watcher
	go sshConn.SyncDir(cwd, projectPathUuidDev)
	//run launch api server / hot reload loop
	pipReqPath := filepath.Join(remoteProjectPath, config.GetPath([]string{"runtime", "requirements_path"}).(string))
	handlerPath := filepath.Join(remoteProjectPath, config.GetPath([]string{"runtime", "handler_path"}).(string))
	launchApiServer := fmt.Sprintf(`
	pkill inotify

	function force_kill {
		kill $1 2>/dev/null
		sleep 1

		if ps -p $1 > /dev/null; then
			echo "Graceful kill failed, attempting SIGKILL..."
			kill -9 $1 2>/dev/null
			sleep 1

			if ps -p $1 > /dev/null; then
				echo "Failed to kill process with PID: $1"
				exit 1
			else
				echo "Killed process with PID: $1 using SIGKILL"
			fi

		else
			echo "Killed process with PID: $1"
		fi
	}

	function cleanup {
		echo "Cleaning up..."
		force_kill $last_pid
	}

	trap cleanup EXIT SIGINT

	if source %s/venv/bin/activate; then
		echo -e "- Activated virtual environment."
	else
		echo "Failed to activate virtual environment."
		exit 1
	fi

	if cd %s/%s; then
		echo -e "- Changed to project directory."
	else
		echo "Failed to change directory."
		exit 1
	fi

	exclude_pattern='(__pycache__|\\.pyc$)'
	function update_exclude_pattern {
		exclude_pattern='(__pycache__|\\.pyc$)'
		if [[ -f .runpodignore ]]; then
			while IFS= read -r line; do
				line=$(echo "$line" | tr -d '[:space:]')
				[[ "$line" =~ ^#.*$ || -z "$line" ]] && continue # Skip comments and empty lines
				exclude_pattern="${exclude_pattern}|(${line})"
			done < .runpodignore
			echo -e "- Ignoring files matching pattern: $exclude_pattern"
		fi
	}
	update_exclude_pattern

	# Start the API server in the background, and save the PID
	python %s --rp_serve_api --rp_api_host="0.0.0.0" --rp_api_port=8080 --rp_api_concurrency=1 &
	last_pid=$!

	echo -e "- Started API server with PID: $last_pid" && echo ""
	echo "Connect to the API server at:"
	echo ">  https://$RUNPOD_POD_ID-8080.proxy.runpod.net/docs" && echo ""

	while true; do
		if changed_file=$(inotifywait -q -r -e modify,create,delete --exclude "$exclude_pattern" %s --format '%%w%%f'); then
			echo "Detected changes in: $changed_file"
		else
			echo "Failed to detect changes."
			exit 1
		fi

		force_kill $last_pid

		if [[ $changed_file == *"requirements"* ]]; then
			echo "Installing new requirements..."
			python -m pip install --upgrade pip && python -m pip install -r %s
		fi

		if [[ $changed_file == *".runpodignore"* ]]; then
			update_exclude_pattern
		fi

		python %s --rp_serve_api --rp_api_host="0.0.0.0" --rp_api_port=8080 --rp_api_concurrency=1 &
		last_pid=$!

		echo "Restarted API server with PID: $last_pid"
	done
	`, projectPathUuidDev, projectPathUuidDev, projectName, handlerPath, remoteProjectPath, pipReqPath, handlerPath)
	fmt.Println()
	fmt.Println("Starting project development endpoint...")
	sshConn.RunCommand(launchApiServer)
	return nil
}
