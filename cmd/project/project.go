package project

import (
	"cli/api"
	"fmt"
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
	networkVolumeId = options[i].Value
	return networkVolumeId, nil
}
func selectStarterTemplate() (template string, err error) {
	type StarterTemplateOption struct {
		Name  string // The string to display
		Value string // The actual value to use
	}
	templates, err := starterTemplates.ReadDir("starter_templates")
	if err != nil {
		fmt.Println("Something went wrong trying to fetch starter templates")
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
		Label:     "Select a Starter Template:",
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
		if modelType == "" {
			template, err := selectStarterTemplate()
			modelType = template
			if err != nil {
				modelType = ""
			}
		}
		cudaVersion := promptChoice("Select a CUDA version, or press enter to use the default",
			[]string{"11.1.1", "11.8.0", "12.1.0"}, "11.8.0")
		pythonVersion := promptChoice("Select a Python version, or press enter to use the default",
			[]string{"3.8", "3.9", "3.10", "3.11"}, "3.10")
		fmt.Printf(`
Project Summary:
   - Project Name: %s
   - Starter Template: %s
   - CUDA Version: %s
   - Python Version: %s
		`, projectName, modelType, cudaVersion, pythonVersion)
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
			networkVolumeId = netVolId
			viper.Set(fmt.Sprintf("project_volumes.%s", projectId), networkVolumeId)
			viper.WriteConfig()
			if err != nil {
				return
			}
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

var BuildProjectCmd = &cobra.Command{
	Use:   "build",
	Args:  cobra.ExactArgs(0),
	Short: "build Dockerfile for current project",
	Long:  "build a Dockerfile for the Runpod project in the current folder",
	Run: func(cmd *cobra.Command, args []string) {
		buildProjectDockerfile()
		// config := loadProjectConfig()
		// projectConfig := config.Get("project").(*toml.Tree)
		// projectId := projectConfig.Get("uuid").(string)
		// projectName := projectConfig.Get("name").(string)
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
	NewProjectCmd.Flags().StringVarP(&modelName, "model", "m", "", "model name")
	NewProjectCmd.Flags().StringVarP(&modelType, "type", "t", "", "model type")
	NewProjectCmd.Flags().BoolVarP(&initCurrentDir, "init", "i", false, "use the current directory as the project directory")

	StartProjectCmd.Flags().BoolVar(&setDefaultNetworkVolume, "select-volume", false, "select a new default network volume for current project")
	StartProjectCmd.Flags().BoolVar(&showPrefixInPodLogs, "prefix-pod-logs", true, "prefix logs from development pod with pod id")
	BuildProjectCmd.Flags().BoolVar(&includeEnvInDockerfile, "include-env", false, "include environment variables from runpod.toml in generated Dockerfile")

}
