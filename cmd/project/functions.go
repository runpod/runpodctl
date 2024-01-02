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
	"time"

	"github.com/google/uuid"
	"github.com/pelletier/go-toml"
)

// TODO: embed all hidden files even those not at top level
//
//go:embed starter_templates/* starter_templates/*/.*
var starterTemplates embed.FS

//go:embed example.toml
var tomlTemplate embed.FS

const basePath string = "starter_templates"

func baseDockerImage(cudaVersion string) string {
	return fmt.Sprintf("runpod/base:0.4.4-cuda%s", cudaVersion)
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
func createNewProject(projectName string, cudaVersion string,
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
	return "", errors.New("pod does not exist for project")
}
func getProjectEndpoint(projectId string) (string, error) {
	endpoints, err := api.GetEndpoints()
	if err != nil {
		return "", err
	}
	for _, endpoint := range endpoints {
		if strings.Contains(endpoint.Name, projectId) {
			fmt.Println(endpoint.Id)
			return endpoint.Id, nil
		}
	}
	return "", errors.New("endpoint does not exist for project")
}

func attemptPodLaunch(config *toml.Tree, networkVolumeId string, environmentVariables map[string]string, selectedGpuTypes []string) (pod map[string]interface{}, err error) {
	projectConfig := config.Get("project").(*toml.Tree)
	//attempt to launch a pod with the given configuration.
	for _, gpuType := range selectedGpuTypes {
		fmt.Printf("Trying to get a pod with %s... ", gpuType)
		podEnv := mapToApiEnv(environmentVariables)
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
			NetworkVolumeId: networkVolumeId,
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

func launchDevPod(config *toml.Tree, networkVolumeId string) (string, error) {
	fmt.Println("Deploying development pod on RunPod...")
	//construct env vars
	environmentVariables := createEnvVars(config)
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
	new_pod, err := attemptPodLaunch(config, networkVolumeId, environmentVariables, selectedGpuTypes)
	if err != nil {
		fmt.Println(err)
		return "", err
	}
	fmt.Printf("Check on pod status at https://www.runpod.io/console/pods/%s\n", new_pod["id"].(string))
	return new_pod["id"].(string), nil
}

func createEnvVars(config *toml.Tree) map[string]string {
	environmentVariables := map[string]string{}
	tomlEnvVars := config.GetPath([]string{"project", "env_vars"})
	if tomlEnvVars != nil {
		tomlEnvVarsMap := tomlEnvVars.(*toml.Tree).ToMap()
		for k, v := range tomlEnvVarsMap {
			environmentVariables[k] = v.(string)
		}
	}
	environmentVariables["RUNPOD_PROJECT_ID"] = config.GetPath([]string{"project", "uuid"}).(string)
	return environmentVariables
}
func mapToApiEnv(env map[string]string) []*api.PodEnv {
	podEnv := []*api.PodEnv{}
	for k, v := range env {
		podEnv = append(podEnv, &api.PodEnv{Key: k, Value: v})
	}
	return podEnv
}

func startProject(networkVolumeId string) error {
	//parse project toml
	config := loadProjectConfig()
	fmt.Println(config)
	projectId := config.GetPath([]string{"project", "uuid"}).(string)
	projectName := config.GetPath([]string{"project", "name"}).(string)
	//check for existing pod
	projectPodId, err := getProjectPod(projectId)
	if projectPodId == "" || err != nil {
		//or try to get pod with one of gpu types
		projectPodId, err = launchDevPod(config, networkVolumeId)
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
	volumePath := projectConfig.Get("volume_mount_path").(string)
	projectPathUuid := filepath.Join(volumePath, projectConfig.Get("uuid").(string))
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
	venvPath := "/" + filepath.Join(projectId, "venv")
	archivedVenvPath := filepath.Join(projectPathUuid, "dev-venv.tar.zst")
	fmt.Printf("Activating Python virtual environment %s on pod %s\n", venvPath, projectPodId)
	sshConn.RunCommands([]string{
		fmt.Sprintf(`
		if ! [ -f %s/bin/activate ] 
		then
			if [ -f %s ]
			then
				echo "Retrieving existing venv from network volume..."
				mkdir -p %s && tar -xf %s -C %s
			else
				echo "Creating new venv..."
				python%s -m virtualenv %s
			fi
		fi`, venvPath, archivedVenvPath, venvPath, archivedVenvPath, venvPath, config.GetPath([]string{"runtime", "python_version"}).(string), venvPath),
		fmt.Sprintf(`source %s/bin/activate && 
		cd %s && 
		python -m pip install --upgrade pip && 
		python -m pip install -v --requirement %s --report /installreport.json`,
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

	if source %s/bin/activate; then
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

	function tar_venv {
		if ! [ $(cat /installreport.json | grep "install" | grep -c "\[\]") -eq 1 ]
		then
			tar -c -C %s . | zstd -T0 > /venv.tar.zst;
			mv /venv.tar.zst %s;
			echo "synced venv to network volume"
		fi
	}

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
	tar_venv &
	python %s --rp_serve_api --rp_api_host="0.0.0.0" --rp_api_port=8080 --rp_api_concurrency=1 &
	last_pid=$!

	echo -e "- Started API server with PID: $last_pid" && echo ""
	echo "Connect to the API server at:"
	echo ">  https://$RUNPOD_POD_ID-8080.proxy.runpod.net" && echo ""

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
			python -m pip install --upgrade pip && python -m pip install -r %s --report /installreport.json
			tar_venv &
		fi

		if [[ $changed_file == *".runpodignore"* ]]; then
			update_exclude_pattern
		fi

		python %s --rp_serve_api --rp_api_host="0.0.0.0" --rp_api_port=8080 --rp_api_concurrency=1 &
		last_pid=$!

		echo "Restarted API server with PID: $last_pid"
	done
	`, venvPath, projectPathUuidDev, projectName, venvPath, archivedVenvPath, handlerPath, remoteProjectPath, pipReqPath, handlerPath)
	fmt.Println()
	fmt.Println("Starting project development endpoint...")
	sshConn.RunCommand(launchApiServer)
	return nil
}

func deployProject(networkVolumeId string) (endpointId string, err error) {
	//parse project toml
	config := loadProjectConfig()
	projectId := config.GetPath([]string{"project", "uuid"}).(string)
	projectConfig := config.Get("project").(*toml.Tree)
	projectName := projectConfig.Get("name").(string)
	projectPathUuid := filepath.Join(projectConfig.Get("volume_mount_path").(string), projectConfig.Get("uuid").(string))
	projectPathUuidProd := filepath.Join(projectPathUuid, "prod")
	remoteProjectPath := filepath.Join(projectPathUuidProd, projectConfig.Get("name").(string))
	//check for existing pod
	projectPodId, err := getProjectPod(projectId)
	if projectPodId == "" || err != nil {
		//or try to get pod with one of gpu types
		projectPodId, err = launchDevPod(config, networkVolumeId)
		if err != nil {
			return "", err
		}
	}
	//open ssh connection
	sshConn, err := PodSSHConnection(projectPodId)
	if err != nil {
		fmt.Println("error establishing ssh connection to pod: ", err)
		return "", err
	}
	//sync remote dev to remote prod
	sshConn.RunCommand(fmt.Sprintf("mkdir -p %s", remoteProjectPath))
	fmt.Printf("Syncing files to pod %s prod\n", projectPodId)
	cwd, _ := os.Getwd()
	sshConn.Rsync(cwd, projectPathUuidProd, false)
	//activate venv on remote
	venvPath := filepath.Join(projectPathUuidProd, "venv")
	fmt.Printf("Activating Python virtual environment: %s on pod %s\n", venvPath, projectPodId)
	sshConn.RunCommands([]string{
		fmt.Sprintf("python%s -m venv %s", config.GetPath([]string{"runtime", "python_version"}).(string), venvPath),
		fmt.Sprintf(`source %s/bin/activate && 
		cd %s && 
		python -m pip install --upgrade pip && 
		python -m pip install -v --requirement %s`,
			venvPath, remoteProjectPath, config.GetPath([]string{"runtime", "requirements_path"}).(string)),
	})
	env := mapToApiEnv(createEnvVars(config))
	// Construct the docker start command
	handlerPath := filepath.Join(remoteProjectPath, config.GetPath([]string{"runtime", "handler_path"}).(string))
	activateCmd := fmt.Sprintf(". %s/bin/activate", venvPath)
	pythonCmd := fmt.Sprintf("python -u %s", handlerPath)
	dockerStartCmd := "bash -c \"" + activateCmd + " && " + pythonCmd + "\""
	//deploy new template
	projectEndpointTemplateId, err := api.CreateTemplate(&api.CreateTemplateInput{
		Name:              fmt.Sprintf("%s-endpoint-%s-%d", projectName, projectId, time.Now().UnixMilli()),
		ImageName:         projectConfig.Get("base_image").(string),
		Env:               env,
		DockerStartCmd:    dockerStartCmd,
		IsServerless:      true,
		ContainerDiskInGb: 10,
		VolumeMountPath:   projectConfig.Get("volume_mount_path").(string),
		StartSSH:          true,
		IsPublic:          false,
		Readme:            "",
	})
	if err != nil {
		fmt.Println("error making template")
		return "", err
	}
	//deploy / update endpoint
	deployedEndpointId, err := getProjectEndpoint(projectId)
	if err != nil {
		deployedEndpointId, err = api.CreateEndpoint(&api.CreateEndpointInput{
			Name:            fmt.Sprintf("%s-endpoint-%s", projectName, projectId),
			TemplateId:      projectEndpointTemplateId,
			NetworkVolumeId: networkVolumeId,
			GpuIds:          "AMPERE_16",
			IdleTimeout:     5,
			ScalerType:      "QUEUE_DELAY",
			ScalerValue:     4,
			WorkersMin:      0,
			WorkersMax:      3,
		})
		if err != nil {
			fmt.Println("error making endpoint")
			return "", err
		}
	} else {
		err = api.UpdateEndpointTemplate(deployedEndpointId, projectEndpointTemplateId)
		if err != nil {
			fmt.Println("error updating endpoint template")
			return "", err
		}
	}
	return deployedEndpointId, nil
}
