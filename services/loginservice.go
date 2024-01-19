package services

import (
	"fmt"
	"net/http"
	"os"
	"runtime"

	"github.com/pkg/browser"
)

const configFilePath = "./config.txt" // Update this path as needed

func StartLoginProcess() error {
	// Open the browser window to the login page
	err := openBrowser("https://www.runpod.io/console/login")
	if err != nil {
		return err
	}

	// Start a local server to listen for the callback with the auth token
	http.HandleFunc("/callback", handleAuthCallback)
	fmt.Println("Starting local server for authentication callback. Please ensure your browser allows pop-ups.")
	err = http.ListenAndServe(":8080", nil)
	if err != nil {
		return err
	}

	return nil
}

func openBrowser(url string) error {
	// Handling different operating systems
	switch runtime.GOOS {
	case "linux":
		return browser.OpenURL(url)
	case "windows":
		return browser.OpenURL(url)
	case "darwin":
		return browser.OpenURL(url)
	default:
		return fmt.Errorf("unsupported platform")
	}
}

// Callback handler function
func handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	// Extract the token from the request
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "No token found in the request", http.StatusBadRequest)
		return
	}

	// Store it securely in a config file
	err := writeTokenToConfigFile(token)
	if err != nil {
		http.Error(w, "Failed to store token", http.StatusInternalServerError)
		return
	}

	fmt.Println("Received token:", token)

	// Respond to the request
	fmt.Fprintf(w, "Authentication successful. You can close this window.")
}

func writeTokenToConfigFile(token string) error {
	file, err := os.OpenFile(configFilePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.WriteString(token)
	return err
}

func DeleteConfigFile() error {
	return os.Remove(configFilePath)
}
