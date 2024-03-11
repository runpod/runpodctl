package diagnostic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func getPodMachineID(podID, apiKey string) string {
	url := fmt.Sprintf("https://api.runpod.io/graphql?api_key=%s", apiKey)
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	query := `
        query Pod($podId: String!) {
          pod(input: { podId: $podId }) {
            machineId
          }
        }
    `
	data := map[string]interface{}{
		"query":     query,
		"variables": map[string]string{"podId": podID},
	}
	jsonData, _ := json.Marshal(data)

	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Failed to fetch machineId: %v\n", err)
		return ""
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if pod, ok := result["data"].(map[string]interface{})["pod"].(map[string]interface{}); ok {
		return pod["machineId"].(string)
	}
	return ""
}

func collectEnvInfo() map[string]string {
	fmt.Println("Collecting environment information...")
	envInfo := map[string]string{
		"RUNPOD_POD_ID":              os.Getenv("RUNPOD_POD_ID"),
		"Template CUDA_VERSION":      os.Getenv("CUDA_VERSION"),
		"NVIDIA_DRIVER_CAPABILITIES": os.Getenv("NVIDIA_DRIVER_CAPABILITIES"),
		"NVIDIA_VISIBLE_DEVICES":     os.Getenv("NVIDIA_VISIBLE_DEVICES"),
		"NVIDIA_PRODUCT_NAME":        os.Getenv("NVIDIA_PRODUCT_NAME"),
		"RUNPOD_GPU_COUNT":           os.Getenv("RUNPOD_GPU_COUNT"),
		"machineId":                  getPodMachineID(os.Getenv("RUNPOD_POD_ID"), os.Getenv("RUNPOD_API_KEY")),
	}
	for k, v := range envInfo {
		if v == "" {
			envInfo[k] = "Not Available"
		}
	}
	return envInfo
}

func parseNvidiaSMIOutput(output string) map[string]string {
	cudaVersionRegex := regexp.MustCompile(`CUDA Version: (\d+\.\d+)`)
	driverVersionRegex := regexp.MustCompile(`Driver Version: (\d+\.\d+\.\d+)`)
	gpuNameRegex := regexp.MustCompile(`\|\s+\d+\s+([^\|]+?)\s+On\s+\|`)

	cudaVersion := cudaVersionRegex.FindStringSubmatch(output)
	driverVersion := driverVersionRegex.FindStringSubmatch(output)
	gpuName := gpuNameRegex.FindStringSubmatch(output)

	info := map[string]string{
		"CUDA Version":   "Not Available",
		"Driver Version": "Not Available",
		"GPU Name":       "Not Available",
	}

	if len(cudaVersion) > 1 {
		info["CUDA Version"] = cudaVersion[1]
	}
	if len(driverVersion) > 1 {
		info["Driver Version"] = driverVersion[1]
	}
	if len(gpuName) > 1 {
		info["GPU Name"] = strings.TrimSpace(gpuName[1])
	}

	return info
}

func getNvidiaSMIInfo() map[string]string {
	cmd := exec.Command("nvidia-smi")
	output, err := cmd.Output()
	if err != nil {
		return map[string]string{"Error": fmt.Sprintf("Failed to fetch nvidia-smi info: %v", err)}
	}
	return parseNvidiaSMIOutput(string(output))
}

func getSystemInfo() map[string]interface{} {
	systemInfo := map[string]interface{}{
		"Environment Info":  collectEnvInfo(),
		"Host Machine Info": getNvidiaSMIInfo(),
	}
	return systemInfo
}

func runCUDATest() map[string]string {
	fmt.Println("Performing CUDA operation tests on all available GPUs...")
	gpuCount := 0
	if count, err := strconv.Atoi(os.Getenv("RUNPOD_GPU_COUNT")); err == nil {
		gpuCount = count
	}
	results := make(map[string]string)

	if gpuCount == 0 {
		return map[string]string{"Error": "No GPUs found."}
	}

	for gpuID := 0; gpuID < gpuCount; gpuID++ {
		cmd := exec.Command("python", "-c", fmt.Sprintf(`
import torch
device = torch.device('cuda:%d')
torch.cuda.set_device(device)
x = torch.rand(10, 10, device=device)
y = torch.rand(10, 10, device=device)
z = x + y
print("Success: CUDA is working correctly.")
        `, gpuID))
		output, err := cmd.CombinedOutput()
		if err != nil {
			results[fmt.Sprintf("GPU %d", gpuID)] = fmt.Sprintf("Error: %v", err)
		} else {
			results[fmt.Sprintf("GPU %d", gpuID)] = strings.TrimSpace(string(output))
		}
	}

	return results
}

func saveInfoToFile(info map[string]interface{}, filename string) {
	jsonData, _ := json.MarshalIndent(info, "", "    ")
	ioutil.WriteFile(filename, jsonData, 0644)
	fmt.Printf("Diagnostics information saved to %s. Please share this file with RunPod Tech Support for further assistance.\n", filename)
}

// Cobra command
var GpuDiagnosticsCmd = &cobra.Command{
	Use:   "gpu-diagnostics",
	Short: "Run GPU diagnostics for RunPod",
	Long:  `This command performs a series of diagnostics tests on the GPUs available in your system for RunPod.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("RunPod GPU Diagnostics Tool")
		systemInfo := getSystemInfo()
		systemInfo["CUDA Test Result"] = runCUDATest()
		saveInfoToFile(systemInfo, "/workspace/gpu_diagnostics.json")
	},
}