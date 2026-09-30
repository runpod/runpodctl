package configenv

import (
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Permissions for the runpod config directory and file. The config file can
// hold the api key, so it must not be readable by other users on a shared host.
const (
	configDirPerm  os.FileMode = 0o700
	configFilePerm os.FileMode = 0o600
)

// ConfigFileName is the base name of the runpod config file within the config
// directory.
const ConfigFileName = "config.toml"

// WriteSecureConfig persists the current viper config to <dir>/config.toml with
// owner-only permissions: the directory is created 0700 and the file clamped to
// 0600. It is the single writer for the config file so every call site (root
// init, doctor, config) applies the same protection to the api key.
func WriteSecureConfig(dir string) error {
	if err := os.MkdirAll(dir, configDirPerm); err != nil {
		return err
	}
	file := filepath.Join(dir, ConfigFileName)
	if err := viper.WriteConfigAs(file); err != nil {
		return err
	}
	// viper writes 0644 by default (and leaves a pre-existing file's mode
	// untouched); clamp it so the secret is owner-only.
	return os.Chmod(file, configFilePerm)
}
