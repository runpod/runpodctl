package project

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/gobwas/glob"
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

func ShouldIgnore(filePath string, ignoreList []string) (bool, error) {
	if ignoreList == nil {
		var err error
		ignoreList, err = GetIgnoreList()
		if err != nil {
			return false, err
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		return false, err
	}

	relativePath, err := filepath.Rel(cwd, filePath)
	if err != nil {
		return false, err
	}

	for _, pattern := range ignoreList {
		if strings.HasPrefix(pattern, "/") {
			pattern = pattern[1:]
		}

		if strings.HasSuffix(pattern, "/") {
			pattern += "*"
		}

		glober, err := glob.Compile(pattern)
		if err != nil {
			return false, err
		}

		if glober.Match(relativePath) {
			return true, nil
		}
	}

	return false, nil
}
