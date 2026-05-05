package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/runpod/runpodctl/cmd"
	"github.com/spf13/cobra"
)

type checkIssue struct {
	Kind    string
	Message string
}

func main() {
	skillPath := flag.String("skill", "../skills/runpodctl/SKILL.md", "path to the runpodctl SKILL.md file")
	flag.Parse()

	content, err := os.ReadFile(*skillPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "skill sync check failed: read skill: %v\n", err)
		os.Exit(1)
	}

	issues := checkSkill(cmd.GetRootCmd(), string(content))
	if len(issues) > 0 {
		fmt.Fprintln(os.Stderr, "skill sync check failed:")
		for _, issue := range issues {
			fmt.Fprintf(os.Stderr, "- [%s] %s\n", issue.Kind, issue.Message)
		}
		os.Exit(1)
	}

	fmt.Println("skill sync check passed")
}

func checkSkill(root *cobra.Command, skill string) []checkIssue {
	var issues []checkIssue

	if !hasLiveHelpGuidance(skill) {
		issues = append(issues, checkIssue{
			Kind:    "live-help",
			Message: "SKILL.md must say live runpodctl --help output is authoritative for exact flags",
		})
	}

	for _, command := range publicTopLevelCommands(root) {
		if !mentionsCommand(skill, command) {
			issues = append(issues, checkIssue{
				Kind:    "missing-command",
				Message: fmt.Sprintf("public top-level command %q is not mentioned", command),
			})
		}
	}

	for _, example := range runpodctlExamples(skill) {
		result := resolveExample(root, example)
		switch {
		case result.Placeholder:
			continue
		case !result.Found:
			issues = append(issues, checkIssue{
				Kind:    "invalid-example",
				Message: fmt.Sprintf("example does not reference a valid command path: %q", example),
			})
		case result.Hidden:
			issues = append(issues, checkIssue{
				Kind:    "hidden-command",
				Message: fmt.Sprintf("example recommends hidden command %q: %q", strings.Join(result.Path, " "), example),
			})
		case result.Deprecated:
			issues = append(issues, checkIssue{
				Kind:    "deprecated-command",
				Message: fmt.Sprintf("example recommends deprecated command %q: %q", strings.Join(result.Path, " "), example),
			})
		}
	}

	return issues
}

func hasLiveHelpGuidance(skill string) bool {
	lower := strings.ToLower(skill)
	return strings.Contains(lower, "authoritative") &&
		strings.Contains(lower, "exact flags") &&
		strings.Contains(lower, "runpodctl --help") &&
		strings.Contains(lower, "runpodctl <resource> <action> --help")
}

func publicTopLevelCommands(root *cobra.Command) []string {
	var commands []string
	for _, child := range root.Commands() {
		if child.Hidden || child.Deprecated != "" {
			continue
		}
		name := commandName(child)
		if name == "" {
			continue
		}
		commands = append(commands, name)
	}
	sort.Strings(commands)
	return commands
}

func mentionsCommand(skill, command string) bool {
	pattern := regexp.MustCompile(`(?m)(^|[^[:alnum:]_-])runpodctl\s+` + regexp.QuoteMeta(command) + `([^[:alnum:]_-]|$)`)
	if pattern.MatchString(skill) {
		return true
	}

	heading := regexp.MustCompile(`(?mi)^#{2,4}\s+` + regexp.QuoteMeta(command) + `(\s|\(|$)`)
	return heading.MatchString(skill)
}

func runpodctlExamples(skill string) []string {
	var examples []string
	inFence := false

	for _, rawLine := range strings.Split(skill, "\n") {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if !inFence {
			continue
		}

		line = strings.TrimPrefix(line, "$ ")
		if !strings.HasPrefix(line, "runpodctl ") {
			continue
		}
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if line != "" {
			examples = append(examples, line)
		}
	}

	return examples
}

type exampleResolution struct {
	Found       bool
	Hidden      bool
	Deprecated  bool
	Placeholder bool
	Path        []string
}

func resolveExample(root *cobra.Command, example string) exampleResolution {
	fields := strings.Fields(example)
	if len(fields) < 2 || fields[0] != "runpodctl" {
		return exampleResolution{Placeholder: true}
	}
	if strings.HasPrefix(fields[1], "-") {
		return exampleResolution{Placeholder: true}
	}

	current := root
	var path []string
	for _, token := range fields[1:] {
		if strings.HasPrefix(token, "-") {
			break
		}
		if strings.ContainsAny(token, "<>[]") {
			if len(path) == 0 {
				return exampleResolution{Placeholder: true}
			}
			break
		}

		child := findChild(current, token)
		if child == nil {
			if hasPublicChildren(current) {
				return exampleResolution{Found: false, Path: path}
			}
			break
		}
		current = child
		path = append(path, commandName(child))
	}

	if len(path) == 0 {
		return exampleResolution{Found: false}
	}

	return exampleResolution{
		Found:      true,
		Hidden:     hasHiddenCommand(root, path),
		Deprecated: hasDeprecatedCommand(root, path),
		Path:       path,
	}
}

func hasPublicChildren(command *cobra.Command) bool {
	for _, child := range command.Commands() {
		if !child.Hidden && child.Deprecated == "" {
			return true
		}
	}
	return false
}

func findChild(parent *cobra.Command, token string) *cobra.Command {
	for _, child := range parent.Commands() {
		if commandName(child) == token {
			return child
		}
		for _, alias := range child.Aliases {
			if alias == token {
				return child
			}
		}
	}
	return nil
}

func hasHiddenCommand(root *cobra.Command, path []string) bool {
	current := root
	for _, token := range path {
		current = findChild(current, token)
		if current == nil {
			return false
		}
		if current.Hidden {
			return true
		}
	}
	return false
}

func hasDeprecatedCommand(root *cobra.Command, path []string) bool {
	current := root
	for _, token := range path {
		current = findChild(current, token)
		if current == nil {
			return false
		}
		if current.Deprecated != "" {
			return true
		}
	}
	return false
}

func commandName(command *cobra.Command) string {
	if command == nil {
		return ""
	}
	fields := strings.Fields(command.Use)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
