package project

import (
	"cli/api"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var projectName string
var modelType string
var modelName string
var initCurrentDir bool
var setDefaultNetworkVolume bool
var includeEnvInDockerfile bool
var showPrefixInPodLogs bool

const inputPromptPrefix string = "   > "

func prompt(message string) string {
	var s string = ""
	for s == "" {
		fmt.Print(inputPromptPrefix + message + ": ")
		fmt.Scanln(&s)
	}
	return s
}

func contains(input string, choices []string) bool {
	for _, choice := range choices {
		if input == choice {
			return true
		}
	}
	return false
}

func promptChoice(message string, choices []string, defaultChoice string) string {
	var s string = ""
	for !contains(s, choices) {
		s = ""
		fmt.Print(inputPromptPrefix + message + " (" + strings.Join(choices, ", ") + ") " + "[" + defaultChoice + "]" + ": ")
		fmt.Scanln(&s)
		if s == "" {
			return defaultChoice
		}
	}
	return s
}

func selectNetworkVolume() (networkVolumeId string, err error) {
	networkVolumes, err := api.GetNetworkVolumes()
	if err != nil {
		fmt.Println("Something went wrong trying to fetch network volumes")
		fmt.Println(err)
		return "", err
	}
	if len(networkVolumes) == 0 {
		fmt.Println("You do not have any network volumes.")
		fmt.Println("Please create a network volume (https://runpod.io/console/user/storage) and try again.")
		return "", errors.New("account has no network volumes")
	}
	promptTemplates := &promptui.SelectTemplates{
		Label:    inputPromptPrefix + "{{ . }}",
		Active:   ` {{ "●" | cyan }} {{ .Name | cyan }}`,
		Inactive: `   {{ .Name | white }}`,
		Selected: `   {{ .Name | white }}`,
	}
	options := []NetVolOption{}
	for _, networkVolume := range networkVolumes {
		options = append(options, NetVolOption{Name: fmt.Sprintf("%s: %s (%d GB, %s)", networkVolume.Id, networkVolume.Name, networkVolume.Size, networkVolume.DataCenterId), Value: networkVolume.Id})
	}
	getNetworkVolume := promptui.Select{
		Label:     "Select a Network Volume:",
		Items:     options,
		Templates: promptTemplates,
	}
	i, _, err := getNetworkVolume.Run()
	if err != nil {
		//ctrl c for example
		return "", err
	}
	networkVolumeId = options[i].Value
	return networkVolumeId, nil
}

func selectStarterTemplate() (template string, err error) {
	type StarterTemplateOption struct {
		Name  string // The string to display
		Value string // The actual value to use
	}
	templates, err := starterTemplates.ReadDir("starter_examples")
	if err != nil {
		fmt.Println("Something went wrong trying to fetch the starter project.")
		fmt.Println(err)
		return "", err
	}
	promptTemplates := &promptui.SelectTemplates{
		Label:    inputPromptPrefix + "{{ . }}",
		Active:   ` {{ "●" | cyan }} {{ .Name | cyan }}`,
		Inactive: `   {{ .Name | white }}`,
		Selected: `   {{ .Name | white }}`,
	}
	options := []StarterTemplateOption{}
	for _, template := range templates {
		options = append(options, StarterTemplateOption{Name: template.Name(), Value: template.Name()})
	}
	getStarterTemplate := promptui.Select{
		Label:     "Select a Starter Project:",
		Items:     options,
		Templates: promptTemplates,
	}
	i, _, err := getStarterTemplate.Run()
	if err != nil {
		//ctrl c for example
		return "", err
	}
	template = options[i].Value
	return template, nil
}

// Define a struct that holds the display string and the corresponding value
type NetVolOption struct {
	Name  string // The string to display
	Value string // The actual value to use
}

var NewProjectCmd = &cobra.Command{
	Use:   "create",
	Args:  cobra.ExactArgs(0),
	Short: "creates a new project",
	Long:  "creates a new RunPod project folder on your local machine",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Creating a new project...")

		// Project Name
		if projectName == "" {
			projectName = prompt("Enter the project name")
		}
		fmt.Println("Project name: " + projectName)

		// Starter Example
		if modelType == "" {
			starterExample, err := selectStarterTemplate()
			modelType = starterExample
			if err != nil {
				modelType = ""
			}
		}

		// CUDA Version
		cudaVersion := promptChoice("Select CUDA version [default: 11.8.0]: ",
			[]string{"11.1.1", "11.8.0", "12.1.0"}, "11.8.0")

		// Python Version
		pythonVersion := promptChoice("Select Python version [default: 3.10]: ",
			[]string{"3.8", "3.9", "3.10", "3.11"}, "3.10")

		// Project Summary
		fmt.Println("\nProject Summary:")
		fmt.Println("------------------------------------------------")
		fmt.Printf("Project name    : %s\n", projectName)
		fmt.Printf("Starter project : %s\n", modelType)
		fmt.Printf("CUDA version    : %s\n", cudaVersion)
		fmt.Printf("Python version  : %s\n", pythonVersion)
		fmt.Println("------------------------------------------------")

		// Confirm
		currentDir, err := os.Getwd()
		if err != nil {
			fmt.Println("Error getting current directory:", err)
			return
		}

		fmt.Printf("\nThe project will be created in the current directory: %s\n", currentDir)
		confirm := promptChoice("Proceed with creation? [yes/no, default: yes]: ", []string{"yes", "no"}, "yes")
		if confirm != "yes" {
			fmt.Println("Project creation cancelled.")
			return
		}

		// Create Project
		createNewProject(projectName, cudaVersion, pythonVersion, modelType, modelName, initCurrentDir)
		fmt.Printf("\nProject %s created successfully! Run `cd %s` to change directory to your project.\n", projectName, projectName)
		fmt.Println("From your project root run `runpodctl project dev` to start a development session.")
	},
}

var StartProjectCmd = &cobra.Command{
	Use:     "dev",
	Aliases: []string{"start"},
	Args:    cobra.ExactArgs(0),
	Short:   "starts a development session for the current project",
	Long:    "connects your local environment and the project environment on your Pod. Changes propagate to the project environment in real time.",
	Run: func(cmd *cobra.Command, args []string) {
		config := loadProjectConfig()
		projectId := config.GetPath([]string{"project", "uuid"}).(string)
		networkVolumeId := viper.GetString(fmt.Sprintf("project_volumes.%s", projectId))
		cachedNetVolExists := false
		networkVolumes, err := api.GetNetworkVolumes()
		if err == nil {
			for _, networkVolume := range networkVolumes {
				if networkVolume.Id == networkVolumeId {
					cachedNetVolExists = true
				}
			}
		}
		if setDefaultNetworkVolume || networkVolumeId == "" || !cachedNetVolExists {
			netVolId, err := selectNetworkVolume()
			if err != nil {
				return
			}
			networkVolumeId = netVolId
			viper.Set(fmt.Sprintf("project_volumes.%s", projectId), networkVolumeId)
			viper.WriteConfig()
		}
		startProject(networkVolumeId)
	},
}

var DeployProjectCmd = &cobra.Command{
	Use:   "deploy",
	Args:  cobra.ExactArgs(0),
	Short: "deploys your project as an endpoint",
	Long:  "deploys a serverless endpoint for the RunPod project in the current folder",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Deploying project...")
		networkVolumeId, err := selectNetworkVolume()
		if err != nil {
			return
		}
		endpointId, err := deployProject(networkVolumeId)
		if err != nil {
			fmt.Println("Failed to deploy project: ", err)
			return
		}
		fmt.Printf("Project deployed successfully! Endpoint ID: %s\n", endpointId)
		fmt.Println("Monitor and edit your endpoint at:")
		fmt.Printf("https://www.runpod.io/console/serverless/user/endpoint/%s\n", endpointId)
		fmt.Println("The following URLs are available:")
		fmt.Printf("    - https://api.runpod.ai/v2/%s/runsync\n", endpointId)
		fmt.Printf("    - https://api.runpod.ai/v2/%s/run\n", endpointId)
		fmt.Printf("    - https://api.runpod.ai/v2/%s/health\n", endpointId)
	},
}

var BuildProjectCmd = &cobra.Command{
	Use:   "build",
	Args:  cobra.ExactArgs(0),
	Short: "builds Dockerfile for current project",
	Long:  "builds a local Dockerfile for the project in the current folder. You can use this Dockerfile to build an image and deploy it to any API server.",
	Run: func(cmd *cobra.Command, args []string) {
		buildProjectDockerfile()
		// config := loadProjectConfig()
		// projectConfig := config.Get("project").(*toml.Tree)
		// projectId := projectConfig.Get("uuid").(string)
		// projectName := config.Get("name").(string)
		// //print next steps
		// fmt.Println("Next steps:")
		// fmt.Println()
		// suggestedDockerTag := fmt.Sprintf("runpod-sls-worker-%s-%s:0.1", projectName, projectId)
		// //docker build
		// fmt.Println("# Build Docker image")
		// fmt.Printf("docker build -t %s .\n", suggestedDockerTag)
		// //dockerhub push
		// fmt.Println("# Push Docker image to a container registry such as Dockerhub")
		// fmt.Printf("docker push %s\n", suggestedDockerTag)
		// //go to runpod url and deploy
		// fmt.Println()
		// fmt.Println("Deploy docker image as a serverless endpoint on Runpod")
		// fmt.Println("https://www.runpod.io/console/serverless")
	},
}

func init() {
	NewProjectCmd.Flags().StringVarP(&projectName, "name", "n", "", "project name")
	// NewProjectCmd.Flags().StringVarP(&modelName, "model", "m", "", "model name")
	// NewProjectCmd.Flags().StringVarP(&modelType, "type", "t", "", "model type")
	NewProjectCmd.Flags().BoolVarP(&initCurrentDir, "init", "i", false, "use the current directory as the project directory")

	StartProjectCmd.Flags().BoolVar(&setDefaultNetworkVolume, "select-volume", false, "select a new default network volume for current project")
	StartProjectCmd.Flags().BoolVar(&showPrefixInPodLogs, "prefix-pod-logs", true, "prefix logs from project Pod with Pod ID")
	BuildProjectCmd.Flags().BoolVar(&includeEnvInDockerfile, "include-env", false, "include environment variables from runpod.toml in generated Dockerfile")

}
