package model

import (
	"errors"
	"strings"

	"github.com/runpod/runpodctl/api"
)

// modelRepoError normalizes the two shapes a "Model Repo not available" failure
// arrives in and returns the error to return from a RunE handler. It no longer
// prints: printing here plus returning the error would double-print, and
// swallowing the error made the process exit 0 on a real failure.
func modelRepoError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, api.ErrModelRepoNotImplemented) {
		return api.ErrModelRepoNotImplemented
	}
	if strings.Contains(err.Error(), "Model Repo feature is not enabled for this user") {
		return err
	}
	return nil
}
