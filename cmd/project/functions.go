package project

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed starter_templates/*
var starterTemplates embed.FS

const basePath string = "starter_templates"

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
		templatePath := filepath.Join(basePath, modelType)
		//load selected starter template
		err = fs.WalkDir(starterTemplates, templatePath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			// Skip the base directory
			if path == templatePath {
				return nil
			}
			// Generate the corresponding path in the new project folder
			newPath := filepath.Join(projectFolder, path[len(templatePath):])
			if d.IsDir() {
				return os.MkdirAll(newPath, os.ModePerm)
			} else {
				content, err := fs.ReadFile(starterTemplates, path)
				if err != nil {
					return err
				}
				//if requirements, replace <<RUNPOD>> with runpod-python import
				return os.WriteFile(newPath, content, 0644)
			}
		})

		if err != nil {
			panic(err)
		}
	}
	//project toml
}
