package project

import (
	"cli/api"
	"fmt"
	"strings"

	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

var projectName string
var modelType string
var modelName string
var initCurrentDir bool

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
		return "", err
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
	return options[i].Value, nil
}

// Define a struct that holds the display string and the corresponding value
type NetVolOption struct {
	Name  string // The string to display
	Value string // The actual value to use
}

var NewProjectCmd = &cobra.Command{
	Use:   "new",
	Args:  cobra.ExactArgs(0),
	Short: "create a new project",
	Long:  "create a new Runpod project folder",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Creating a new project...")
		if projectName == "" {
			projectName = prompt("Enter the project name")
		} else {
			fmt.Println("Project name: " + projectName)
		}
		cudaVersion := promptChoice("Select a CUDA version, or press enter to use the default",
			[]string{"11.1.1", "11.8.0", "12.1.0"}, "11.8.0")
		pythonVersion := promptChoice("Select a Python version, or press enter to use the default",
			[]string{"3.8", "3.9", "3.10", "3.11"}, "3.10")

		fmt.Printf(`
Project Summary:
   - Project Name: %s
   - CUDA Version: %s
   - Python Version: %s
		`, projectName, cudaVersion, pythonVersion)
		fmt.Println()
		fmt.Println("The project will be created in the current directory.")
		//TODO confirm y/n
		createNewProject(projectName, cudaVersion,
			pythonVersion, modelType, modelName, initCurrentDir)
		fmt.Printf("Project %s created successfully!", projectName)
		fmt.Println()
		fmt.Println("From your project root run `runpodctl project start` to start a development pod.")
	},
}

var StartProjectCmd = &cobra.Command{
	Use:   "start",
	Args:  cobra.ExactArgs(0),
	Short: "start current project",
	Long:  "start a development pod session for the Runpod project in the current folder",
	Run: func(cmd *cobra.Command, args []string) {
		networkVolumeId, err := selectNetworkVolume()
		if err != nil {
			return
		}
		startProject(networkVolumeId)
	},
}

var DeployProjectCmd = &cobra.Command{
	Use:   "deploy",
	Args:  cobra.ExactArgs(0),
	Short: "deploy current project",
	Long:  "deploy an endpoint for the Runpod project in the current folder",
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
		fmt.Println("The following urls are available:")
		fmt.Printf("    - https://api.runpod.ai/v2/%s/runsync\n", endpointId)
		fmt.Printf("    - https://api.runpod.ai/v2/%s/run\n", endpointId)
		fmt.Printf("    - https://api.runpod.ai/v2/%s/health\n", endpointId)
	},
}

// var BuildProjectCmd = &cobra.Command{
// 	Use:   "build",
// 	Args:  cobra.ExactArgs(0),
// 	Short: "build Docker image for current project",
// 	Long:  "build a Docker image for the Runpod project in the current folder",
// 	Run: func(cmd *cobra.Command, args []string) {
// 		//parse project toml
// 		//build Dockerfile
// 		//base image: from toml
// 		//run setup.sh for system deps
// 		//pip install requirements
// 		//cmd: start handler
// 		//docker build
// 		//print next steps
// 	},
// }

func init() {
	NewProjectCmd.Flags().StringVarP(&projectName, "name", "n", "", "project name")
	NewProjectCmd.Flags().StringVarP(&modelName, "model", "m", "", "model name")
	NewProjectCmd.Flags().StringVarP(&modelType, "type", "t", "", "model type")
	NewProjectCmd.Flags().BoolVarP(&initCurrentDir, "init", "i", false, "use the current directory as the project directory")
}
