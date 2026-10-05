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
	// os.MkdirAll leaves an existing directory's mode untouched, so an older
	// install's loose ~/.runpod (0755/0777) would stay wide. Clamp it.
	if err := os.Chmod(dir, configDirPerm); err != nil {
		return err
	}
	file := filepath.Join(dir, ConfigFileName)
	// Clamp the file to 0600 before writing the secret: viper.WriteConfigAs
	// would otherwise create it 0644, leaving a window where the api key sits
	// in a group/other-readable file. ensureSecureFile also tightens a
	// pre-existing loose file in place before the rewrite.
	if err := ensureSecureFile(file); err != nil {
		return err
	}
	if err := viper.WriteConfigAs(file); err != nil {
		return err
	}
	// Re-assert 0600 in case the file was newly created by viper despite the
	// pre-create above (belt-and-suspenders; keeps the invariant explicit).
	return os.Chmod(file, configFilePerm)
}

// ensureSecureFile makes sure file exists with 0600 permissions before any
// content is written to it. A missing file is created owner-only; a
// pre-existing file is clamped to 0600 without truncating its contents.
func ensureSecureFile(file string) error {
	f, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY, configFilePerm)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// O_CREATE leaves a pre-existing file's mode untouched; clamp it.
	return os.Chmod(file, configFilePerm)
}
