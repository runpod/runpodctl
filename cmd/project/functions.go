package project

import (
	"cli/api"
	"embed"
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

func launchDevPod(config *toml.Tree) (string, error) {
	//TODO create pod
	return "", nil
}
func startProject() error {
	//parse project toml
	config := loadProjectConfig()
	fmt.Println(config)
	projectId := config.GetPath([]string{"project", "uuid"}).(string)
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
	println(sshConn)
	// Run a command
	// cmd := "ls -l"
	// if err := sshConn.Run(cmd); err != nil {
	// 	fmt.Println("Failed to run: %s", err)
	// }
	//create remote folder structure
	//rsync project files
	//activate venv on remote
	//create file watcher
	//run launch api server / hot reload loop
	return nil
}
