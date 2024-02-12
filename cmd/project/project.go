package project

import (
	"cli/api"
	"errors"
	"fmt"
	"os"

	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	projectName             string
	modelType               string
	modelName               string
	initCurrentDir          bool
	setDefaultNetworkVolume bool
	includeEnvInDockerfile  bool
	showPrefixInPodLogs     bool
)

const inputPromptPrefix string = "   > "

func prompt(message string) string {
	var selection string = ""
	for selection == "" {
		fmt.Print(inputPromptPrefix + message)
		fmt.Scanln(&selection)
	}
	return selection
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
	var selection string = ""
	for !contains(selection, choices) {
		selection = ""
		fmt.Println(message)
		fmt.Print("   Available options: ")
		for _, choice := range choices {
			fmt.Printf("%s", choice)
			if choice == defaultChoice {
				fmt.Print(" (default)")
			}
			if choice != choices[len(choices)-1] {
				fmt.Print(", ")
			}

		}

		fmt.Print("\n   > ")

		fmt.Scanln(&selection)

		if selection == "" {
			return defaultChoice
		}
	}
	return selection
}

func selectNetworkVolume() (networkVolumeId string, err error) {
	networkVolumes, err := api.GetNetworkVolumes()
	if err != nil {
		fmt.Println("Error fetching network volumes:", err)
		return "", err
	}
	if len(networkVolumes) == 0 {
		fmt.Println("No network volumes found. Please create one and try again. (https://runpod.io/console/user/storage)")
		return "", errors.New("no network volumes found")
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
		Label:     "Select a Starter Example:",
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
	Use:     "create",
	Aliases: []string{"new"},
	Args:    cobra.ExactArgs(0),
	Short:   "Creates a new project",
	Long:    "Creates a new RunPod project folder on your local machine.",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Print("Welcome to the RunPod Project Creator!\n--------------------------------------\n\n")

		// Project Name
		if projectName == "" {
			fmt.Print("1. Project Name:\n")
			fmt.Print("   Please enter the name of your project.\n")
			projectName = prompt("")
		}
		fmt.Print("\n   Project name set to '" + projectName + "'.\n\n")

		// Project Examples
		fmt.Print("2. Starter Example:\n")
		fmt.Print("   Choose a starter example to begin with.\n")

		if modelType == "" {
			starterExample, err := selectStarterTemplate()
			modelType = starterExample
			if err != nil {
				modelType = ""
			}
		}

		fmt.Println("")

		// Model Name
		if modelType != "Hello World" {
			fmt.Print("   Model Name:\n")
			fmt.Print("   Please enter the name of the Hugging Face model you would like to use.\n")
			fmt.Print("   Leave blank to use the default model for the selected example.\n   > ")
			fmt.Scanln(&modelName)
			fmt.Println("")
		}

		// Project Configuration
		fmt.Print("3. Configuration:\n")
		fmt.Print("   Let's configure the project environment.\n\n")

		// CUDA Version
		fmt.Println("   CUDA Version:")
		cudaVersion := promptChoice("   Choose a CUDA version for your project.",
			[]string{"11.8.0", "12.1.0", "12.2.0"}, "11.8.0")

		fmt.Println("\n   Using CUDA version: " + cudaVersion)

		// Python Version
		fmt.Println("\n   Python Version:")
		pythonVersion := promptChoice("   Choose a Python version for your project.",
			[]string{"3.8", "3.9", "3.10", "3.11"}, "3.10")

		fmt.Println("\n   Using Python version: " + pythonVersion)

		// Project Summary
		fmt.Println("\nProject Summary:")
		fmt.Println("----------------")
		fmt.Printf("- Project Name    : %s\n", projectName)
		fmt.Printf("- Starter Example : %s\n", modelType)
		fmt.Printf("- CUDA version    : %s\n", cudaVersion)
		fmt.Printf("- Python version  : %s\n", pythonVersion)

		// Confirm
		currentDir, err := os.Getwd()
		if err != nil {
			fmt.Println("Error getting current directory:", err)
			return
		}

		fmt.Printf("\nThe project will be created in the current directory: \n%s\n\n", currentDir)
		confirm := promptChoice("Proceed with creation?", []string{"yes", "no"}, "yes")
		if confirm != "yes" {
			fmt.Println("Project creation cancelled.")
			return
		}

		fmt.Println("\nCreating project...")

		// Create Project
		createNewProject(projectName, cudaVersion, pythonVersion, modelType, modelName, initCurrentDir)
		fmt.Printf("\nProject %s created successfully! \nNavigate to your project directory with `cd %s`\n\n", projectName, projectName)
		fmt.Println("Tip: Run `runpodctl project dev` to start a development session for your project.")
	},
}

var StartProjectCmd = &cobra.Command{
	Use:     "dev",
	Aliases: []string{"start"},
	Args:    cobra.ExactArgs(0),
	Short:   "Start a development session for the current project",
	Long:    "This command establishes a connection between your local development environment and your RunPod project environment, allowing for real-time synchronization of changes.",
	Run: func(cmd *cobra.Command, args []string) {
		// Check for the existence of 'runpod.toml' in the current directory
		if _, err := os.Stat("runpod.toml"); os.IsNotExist(err) {
			fmt.Println("No 'runpod.toml' found in the current directory.")
			fmt.Println("Please navigate to your project directory and try again.")
			return
		}

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
	// Set up flags for the project commands
	NewProjectCmd.Flags().StringVarP(&projectName, "name", "n", "", "Set the project name, a directory with this name will be created in the current path.")
	NewProjectCmd.Flags().BoolVarP(&initCurrentDir, "init", "i", false, "Initialize the project in the current directory instead of creating a new one.")
	StartProjectCmd.Flags().BoolVar(&setDefaultNetworkVolume, "select-volume", false, "Choose a new default network volume for the project.")

	NewProjectCmd.Flags().StringVarP(&modelName, "model", "m", "", "Specify the Hugging Face model name for the project.")
	NewProjectCmd.Flags().StringVarP(&modelType, "type", "t", "", "Specify the model type for the project.")

	StartProjectCmd.Flags().BoolVar(&showPrefixInPodLogs, "prefix-pod-logs", true, "Include the Pod ID as a prefix in log messages from the project Pod.")
	BuildProjectCmd.Flags().BoolVar(&includeEnvInDockerfile, "include-env", false, "Incorporate environment variables defined in runpod.toml into the generated Dockerfile.")
}
