package podssh

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

var EXCLUDE_PATTERNS = []string{
	"__pycache__/",
	"*.pyc",
	".*.swp",
	".git/",
	"*.tmp",
	"*.log",
}

func GetIgnoreList() ([]string, error) {
	// Reads the .runpodignore file and returns a list of files to ignore.
	ignoreList := make([]string, len(EXCLUDE_PATTERNS))
	copy(ignoreList, EXCLUDE_PATTERNS)

	cwd, _ := os.Getwd()
	ignoreFile := filepath.Join(cwd, ".runpodignore")

	file, err := os.Open(ignoreFile)
	if err != nil {
		if os.IsNotExist(err) {
			return ignoreList, nil
		}
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			ignoreList = append(ignoreList, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return ignoreList, nil
}
