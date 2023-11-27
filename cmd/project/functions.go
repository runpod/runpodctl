package project

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/pelletier/go-toml"
)

//go:embed starter_templates/*
var starterTemplates embed.FS

//go:embed example.toml
var tomlTemplate embed.FS

const basePath string = "starter_templates"

var defaultGpuTypes = [...]string{
	"NVIDIA RTX A4000", "NVIDIA RTX A4500", "NVIDIA RTX A5000",
	"NVIDIA GeForce RTX 3090", "NVIDIA RTX A6000",
}

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
		//in requirements, replace <<RUNPOD>> with runpod-python import
	}
	//generate project toml
	tomlBytes, _ := tomlTemplate.ReadFile("example.toml")
	projectToml, _ := toml.LoadBytes(tomlBytes)
	projectUuid := uuid.New().String()[0:8]
	projectToml.SetComment("RunPod Project Configuration")
	projectToml.SetPath([]string{"title"}, projectName)
	projectToml.SetPath([]string{"project", "name"}, projectName)
	projectToml.SetPath([]string{"project", "uuid"}, projectUuid)
	projectToml.SetPath([]string{"project", "base_image"}, baseDockerImage(cudaVersion))
	projectToml.SetPath([]string{"project", "storage_id"}, networkVolumeId)
	projectToml.SetPath([]string{"template", "model_type"}, modelType)
	projectToml.SetPath([]string{"template", "model_name"}, modelName)
	projectToml.SetPath([]string{"runtime", "python_version"}, pythonVersion)
	fmt.Println(projectToml)
}
