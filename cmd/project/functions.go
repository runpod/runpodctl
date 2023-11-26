package project

import (
	"os"
	"path/filepath"
)

func createNewProject(projectName string, networkVolumeId string, cudaVersion string,
	pythonVersion string, modelType string, modelName string, initCurrentDir bool) {
	projectFolder, _ := os.Getwd()
	if !initCurrentDir {
		projectFolder = filepath.Join(projectFolder, projectName)
		_, err := os.Stat(projectFolder)
		if os.IsNotExist(err) {
			os.Mkdir(projectFolder, 0700)
		}
		if modelType == "" {
			modelType = "default"
		}
		templateDir := filepath.Join(STARTER_TEMPLATES, modelType)
	}
	//create files
	//folder structure (check for --init)
	//project toml
}
