package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunpodctlExamples(t *testing.T) {
	skill := `
runpodctl pod list

` + "```bash" + `
# comment
runpodctl pod list --all # inline comment
$ runpodctl serverless create --hub-id abc
curl https://example.com
` + "```" + `
`

	examples := runpodctlExamples(skill)
	expected := []string{
		"runpodctl pod list --all",
		"runpodctl serverless create --hub-id abc",
	}

	if strings.Join(examples, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected examples:\n%v", examples)
	}
}

func TestCheckSkill(t *testing.T) {
	root := testRoot()
	skill := `
live runpodctl --help output is authoritative for exact flags.
use runpodctl <resource> <action> --help before using unfamiliar commands.

## Commands
runpodctl pod
runpodctl serverless
runpodctl help

` + "```bash" + `
runpodctl pod list
runpodctl serverless create --hub-id abc
` + "```" + `
`

	issues := checkSkill(root, skill)
	if len(issues) > 0 {
		t.Fatalf("expected no issues, got %+v", issues)
	}
}

func TestCheckSkillReportsInvalidAndDeprecatedExamples(t *testing.T) {
	root := testRoot()
	skill := `
live runpodctl --help output is authoritative for exact flags.
use runpodctl <resource> <action> --help before using unfamiliar commands.

runpodctl pod
runpodctl serverless
runpodctl help

` + "```bash" + `
runpodctl pod missing
runpodctl old
` + "```" + `
`

	issues := checkSkill(root, skill)
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %+v", issues)
	}
	if issues[0].Kind != "invalid-example" {
		t.Fatalf("expected invalid-example first, got %+v", issues)
	}
	if issues[1].Kind != "deprecated-command" {
		t.Fatalf("expected deprecated-command second, got %+v", issues)
	}
}

func TestCheckSkillReportsHiddenExampleWithPlaceholderArg(t *testing.T) {
	root := testRoot()
	skill := `
live runpodctl --help output is authoritative for exact flags.
use runpodctl <resource> <action> --help before using unfamiliar commands.

runpodctl pod
runpodctl serverless
runpodctl help

` + "```bash" + `
runpodctl hidden <id>
` + "```" + `
`

	issues := checkSkill(root, skill)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %+v", issues)
	}
	if issues[0].Kind != "hidden-command" {
		t.Fatalf("expected hidden-command, got %+v", issues)
	}
}

func testRoot() *cobra.Command {
	root := &cobra.Command{Use: "runpodctl"}
	pod := &cobra.Command{Use: "pod"}
	pod.AddCommand(&cobra.Command{Use: "list"})
	serverless := &cobra.Command{Use: "serverless", Aliases: []string{"sls"}}
	serverless.AddCommand(&cobra.Command{Use: "create"})
	root.AddCommand(pod, serverless)
	root.SetHelpCommand(&cobra.Command{Use: "help"})
	root.AddCommand(&cobra.Command{Use: "old", Deprecated: "use pod instead"})
	root.AddCommand(&cobra.Command{Use: "hidden", Hidden: true})
	return root
}
