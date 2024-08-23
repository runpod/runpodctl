package project

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/runpod/runpodctl/api"

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
	reader := bufio.NewReader(os.Stdin)
	fmt.Print(inputPromptPrefix + message)

	selection, err := reader.ReadString('\n')
	if err != nil {
		fmt.Println("An error occurred while reading input. Please try again.", err)
		return prompt(message)
	}

	selection = strings.TrimSpace(selection)
	if selection == "" {
		return prompt(message)
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
		// ctrl c for example
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
		// For the printed name, replace _ with spaces
		name := template.Name()
		name = strings.Replace(name, "_", " ", -1)
		options = append(options, StarterTemplateOption{Name: name, Value: template.Name()})
	}
	getStarterTemplate := promptui.Select{
		Label:     "Select a Starter Project:",
		Items:     options,
		Templates: promptTemplates,
	}
	i, _, err := getStarterTemplate.Run()
	if err != nil {
		// ctrl c for example
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
			fmt.Print("Provide a name for your project:\n")
			projectName = prompt("")
		}
		fmt.Print("\n   Project name set to '" + projectName + "'.\n\n")

		// Project Examples
		fmt.Print("Select a starter project to begin with:\n")

		if modelType == "" {
			starterExample, err := selectStarterTemplate()
			modelType = starterExample
			if err != nil {
				modelType = ""
			}
		}

		fmt.Println("")

		// Model Name
		if modelType != "Hello_World" {
			fmt.Print("   Enter the name of the Hugging Face model you would like to use:\n")
			fmt.Print("   Leave blank to use the default model for the selected project.\n   > ")
			fmt.Scanln(&modelName)
			fmt.Println("")
		}

		// CUDA Version
		cudaVersion := promptChoice("Select a CUDA version for your project:",
			[]string{"11.8.0", "12.1.0", "12.2.0"}, "11.8.0")

		fmt.Println("\n   Using CUDA version: " + cudaVersion + "\n")

		// Python Version
		pythonVersion := promptChoice("Select a Python version for your project:",
			[]string{"3.8", "3.9", "3.10", "3.11"}, "3.10")

		fmt.Println("\n   Using Python version: " + pythonVersion)

		// Project Summary
		fmt.Println("\nProject Summary:")
		fmt.Println("----------------")
		fmt.Printf("- Project Name    : %s\n", projectName)
		fmt.Printf("- Starter Project : %s\n", modelType)
		fmt.Printf("- CUDA version    : %s\n", cudaVersion)
		fmt.Printf("- Python version  : %s\n", pythonVersion)

		// Confirm
		currentDir, err := os.Getwd()
		if err != nil {
			fmt.Println("Error getting current directory:", err)
			return
		}

		projectDir := filepath.Join(currentDir, projectName)
		if _, err := os.Stat(projectDir); !os.IsNotExist(err) {
			fmt.Printf("\nA directory with the name '%s' already exists in the current path.\n", projectName)
			confirm := promptChoice("Continue with overwrite?", []string{"yes", "no"}, "no")
			if confirm != "yes" {
				fmt.Println("Project creation cancelled.")
				return
			}
		} else {
			fmt.Printf("\nCreating project '%s' in directory '%s'\n", projectName, projectDir)
		}

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

		config := loadTomlConfig("runpod.toml")
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

func printEndpointSuccess(endpointId string) {
	fmt.Printf("Project deployed successfully! Endpoint ID: %s\n", endpointId)
	fmt.Println("Monitor and edit your endpoint at:")
	fmt.Printf("https://www.runpod.io/console/serverless/user/endpoint/%s\n", endpointId)
	fmt.Println("The following URLs are available:")
	fmt.Printf("    - https://api.runpod.ai/v2/%s/runsync\n", endpointId)
	fmt.Printf("    - https://api.runpod.ai/v2/%s/run\n", endpointId)
	fmt.Printf("    - https://api.runpod.ai/v2/%s/health\n", endpointId)
}

var DeployProjectFromEndpointConfigCmd = &cobra.Command{
	Use:   "deploy-from-config",
	Args:  cobra.ExactArgs(0),
	Short: "deploys your project as an endpoint",
	Long:  "deploys a serverless endpoint from the provided endpoint config",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Deploying project...")
		endpointId, err := upsertProjectFromEndpointConfig()
		if err != nil {
			fmt.Println("Failed to deploy project: ", err)
			return
		}
		printEndpointSuccess(endpointId)
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
		printEndpointSuccess(endpointId)
	},
}

var GenerateEndpointConfigCmd = &cobra.Command{
	Use:   "generate-endpoint-config",
	Args:  cobra.ExactArgs(0),
	Short: "generates an endpoint configuration file for the current project",
	Long:  "generates an endpoint configuration file for the current project",
	Run: func(cmd *cobra.Command, args []string) {
		projectDir, err := os.Getwd()
		if err != nil {
			log.Fatalf("Error getting current directory: %v", err)
			return
		}
		projectConfig := loadTomlConfig("runpod.toml")
		projectId := mustGetPathAs[string](projectConfig, "project", "uuid")
		err = buildEndpointConfig(projectDir, projectId)
		if err != nil {
			log.Fatalf("Error generating endpoint configuration: %v", err)
			return
		}
	},
}

var BuildProjectDockerfileCmd = &cobra.Command{
	Use:   "build-dockerfile",
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
	BuildProjectDockerfileCmd.Flags().BoolVar(&includeEnvInDockerfile, "include-env", false, "Incorporate environment variables defined in runpod.toml into the generated Dockerfile.")
}
