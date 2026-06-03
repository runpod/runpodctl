package model

import (
	"errors"
	"strings"

	"github.com/runpod/runpodctl/api"
	"github.com/runpod/runpodctl/internal/output"
)

func handleModelRepoError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, api.ErrModelRepoNotImplemented) {
		output.Error(api.ErrModelRepoNotImplemented)
		return true
	}
	if strings.Contains(err.Error(), "Model Repo feature is not enabled for this user") {
		output.Error(err)
		return true
	}
	return false
}
