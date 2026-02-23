package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/runpod/runpodctl/api"
)

func handleModelRepoError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, api.ErrModelRepoNotImplemented) {
		fmt.Println(api.ErrModelRepoNotImplemented.Error())
		return true
	}
	if strings.Contains(err.Error(), "Model Repo feature is not enabled for this user") {
		fmt.Println(err.Error())
		return true
	}
	return false
}
