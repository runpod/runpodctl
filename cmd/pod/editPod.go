package pod

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/runpod/runpodctl/api"

	"github.com/spf13/cobra"
)

const MaxPorts = 10

type EditPodCmdConfig struct {
	containerDiskInGb *int
	dockerArgs        *string
	env               *[]string
	imageName         *string
	ports             *[]string
	volumeInGb        *int
	volumeMountPath   *string
	dryrun            bool
}

var editConf EditPodCmdConfig

var EditPodCmd = &cobra.Command{
	Use:   "pod id|name",
	Args:  cobra.ExactArgs(1),
	Short: "edit pod config",
	Long:  "Edit (update) pod configuration without releasing the machine \n(may result in wiping of container disk; use a volume to persist data)",
	Run: func(cmd *cobra.Command, args []string) {
		setUserProviderOptions(cmd, editOptBindings)
		cobra.CheckErr(assertValidPorts(editConf.ports))

		pods, err := api.GetPods()
		cobra.CheckErr(err)

		podID := args[0]
		pod := findPod(pods, podID)
		if pod == nil {
			cobra.CheckErr(fmt.Errorf("no such pod with ID or name \"%s\"", podID))
		}

		input := newInputFromPod(pod)
		err = populatePodEditJobInput(pod, input, &editConf)
		cobra.CheckErr(err)

		if editConf.dryrun {
			b, err := json.MarshalIndent(input, "", "  ")
			cobra.CheckErr(err)
			fmt.Fprint(os.Stderr, "Dryrun - edit pod request to backend (not sent):\n")
			os.Stdout.Write(b)
			return
		}

		fmt.Fprintf(os.Stderr, "Updating pod: %s\n", input.PodId)
		resp, err := api.UpdatePod(input)
		cobra.CheckErr(err)

		b, err := json.MarshalIndent(resp, "", "  ")
		cobra.CheckErr(err)

		fmt.Fprint(os.Stderr, "New pod configuration::\n")
		os.Stdout.Write(b)
	},
}

func init() {
	editOptBindings = make(bindings)
	b := editOptBindings

	EditPodCmd.Flags().BoolVarP(&editConf.dryrun, "dryrun", "s", false, "container volume path")

	bind(b, EditPodCmd, &editConf.containerDiskInGb, "containerDiskSize", 0, "container disk size in GB")
	bind(b, EditPodCmd, &editConf.dockerArgs, "args", "",
		"container arguments. Use the special argument \"[]\" to clear any arguments and use "+
			"the image's default entrypoint or CMD (if present in the image)")
	bind(b, EditPodCmd, &editConf.env, "env", nil, "container arguments")
	bind(b, EditPodCmd, &editConf.imageName, "imageName", "", "container image name")
	bind(b, EditPodCmd, &editConf.ports, "ports", nil, "ports to expose; max 10 http and 10 tcp allowed; e.g. '8888/http'")
	bind(b, EditPodCmd, &editConf.volumeInGb, "volumeSize", 0, "persistent volume disk size in GB")
	bind(b, EditPodCmd, &editConf.volumeMountPath, "volumePath", "", "container volume path")
}

// region option binding machinery

type bindings = map[string]func(cmd *cobra.Command)

var editOptBindings bindings

// populates the command's option struct with only what was specified by the user on the cmd line
func setUserProviderOptions(cmd *cobra.Command, bindings bindings) {
	for _, resolve := range bindings {
		resolve(cmd)
	}
}

// bind performs late binding of variables to pointer fields, meaning it will only populate fields
// that the user has explicitly provided on the command line. Options provided by the user on the command
// line will have non-nil pointers, the others all nil. The binding happens in two phases:
//  1. The normal Cobra variable binding happens, which registers the options as valid for the command,
//     and also defines which pointer field should be bound to which option.
//  2. When the Run method of the command executes, a *resolution* step is performed, where it's figured
//     out *which* of all the options the user actually provided on the cmd line.
//
// The signature mimics that of cobra's TypeVarP methods, so should be intuitive.
// Only differences 1) pointer-to-pointer binding instead of a direct pointer has to be passed as the
// variable to bind instead of a direct pointer (reference). The code catches such mistakes with a runtime
// cobra error.
// 2) a bindings map has to be passed in, so as to make testing easier and also allow this code to be
// reused by other commands facing the same challenge.
//
// In short the machinery consists of three parts:
// 1. A binding map to hold all the bound options.
// 2. a 'bind' (ing) function for establishing option -> variable relationship.
// 3. a resolution function to resolve the late bindings during the Run phase.
func bind[T any](bindings bindings, cmd *cobra.Command, pp **T, name string, value T, usage string) {
	fatalErr := fmt.Errorf("Bug: got impossible type mismatch between option binding field and value of "+
		"type: %T for option: %v", value, name)

	switch v := any(value).(type) {
	case string:
		cmd.Flags().String(name, v, usage)
		bindings[name] = func(cmd *cobra.Command) {
			if cmd.Flags().Changed(name) {
				if field, ok := any(pp).(**string); ok {
					s, err := cmd.Flags().GetString(name)
					cobra.CheckErr(err)
					*field = &s
					return
				}
				cobra.CheckErr(fatalErr)
			}
		}
	case int:
		cmd.Flags().Int(name, v, usage)
		bindings[name] = func(cmd *cobra.Command) {
			if cmd.Flags().Changed(name) {
				if field, ok := any(pp).(**int); ok {
					n, err := cmd.Flags().GetInt(name)
					cobra.CheckErr(err)
					*field = &n
					return
				}
				cobra.CheckErr(fatalErr)
			}
		}
	case []string:
		cmd.Flags().StringSlice(name, nil, usage)
		bindings[name] = func(cmd *cobra.Command) {
			if cmd.Flags().Changed(name) {
				if field, ok := any(pp).(**[]string); ok {
					slice, err := cmd.Flags().GetStringSlice(name)
					cobra.CheckErr(err)
					*field = &slice
					return
				}
				cobra.CheckErr(fatalErr)
			}
		}
	default:
		cobra.CheckErr(fmt.Errorf("binding of %T not supported, used for binding option: %v", value, name))
	}
}

// endregion option binding machinery

func findPod(pods []*api.Pod, podID string) *api.Pod {
	for _, pod := range pods {
		if pod.Id == podID || pod.Name == podID {
			return pod
		}
	}
	return nil
}

// overrides the default input values (those of the existing pod) with any value(s) the user explicitly provided
func populatePodEditJobInput(pod *api.Pod, input *api.PodEditJobInput, cfg *EditPodCmdConfig) error {
	if cfg.ports != nil {
		portsStr := strings.Join(*cfg.ports, ",")
		conditionallySet(pod, &portsStr, &input.Ports)
	}
	conditionallySet(pod, cfg.containerDiskInGb, &input.ContainerDiskInGb)
	conditionallySet(pod, cfg.dockerArgs, &input.DockerArgs)
	conditionallySet(pod, cfg.imageName, &input.ImageName)
	conditionallySet(pod, cfg.volumeMountPath, &input.VolumeMountPath)
	conditionallySet(pod, cfg.volumeInGb, &input.VolumeInGb)

	if cfg.env == nil { // sending null to the backend will make it keep the pre-existing value for the pod, where-as empty array deletes all envs.
		return nil
	}
	input.Env = make([]*api.PodEnv, len(*cfg.env))
	for i, v := range *cfg.env {
		e := strings.Split(v, "=")
		if len(e) != 2 {
			return fmt.Errorf("wrong env value: %s", e)
		}
		input.Env[i] = &api.PodEnv{Key: e[0], Value: e[1]}
	}
	return nil
}

// pointers are used to identify if a flag was set by the user or not.
// pointers have two states; assigned or not (nil). That's the only reason for pointer use and the c-like pointer gymnastics.
func conditionallySet[T any](pod *api.Pod, configField *T, inputField *T) {
	if configField != nil {
		fmt.Fprintf(os.Stderr, "[*] updating (pod: %s) image: %v -> %v\n", pod.Id, *inputField, *configField)
		*inputField = *configField
	}
}

func newInputFromPod(pod *api.Pod) *api.PodEditJobInput {
	return &api.PodEditJobInput{
		PodId:             pod.Id,
		ContainerDiskInGb: pod.ContainerDiskInGb,
		DockerArgs:        pod.DockerArgs,
		ImageName:         pod.ImageName,
		Ports:             pod.Ports,
		VolumeInGb:        pod.VolumeInGb,
		VolumeMountPath:   pod.VolumeMountPath,
	}
}

func assertValidPorts(ports *[]string) error {
	if ports == nil {
		return nil
	}
	var nTCP, nHTTP int
	expr := regexp.MustCompile("^\\d+/(http|tcp)$")
	for _, port := range *ports {
		match := expr.FindStringSubmatch(port)
		if match == nil {
			return fmt.Errorf("Invalid port syntax. Must be number/http or number/tcp. Got: %s", port)
		}
		if match[1] == "tcp" {
			nTCP++
		} else if match[1] == "http" {
			nHTTP++
		}
	}
	if nTCP > MaxPorts {
		return fmt.Errorf("Maximum number of TCP ports (%d) exceeded. %d were provided", MaxPorts, nTCP)
	}
	if nHTTP > MaxPorts {
		return fmt.Errorf("Maximum number of HTTP ports (%d) exceeded. %d were provided", MaxPorts, nHTTP)
	}
	return nil
}
