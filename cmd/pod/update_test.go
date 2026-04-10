package pod

import "testing"

func TestMergeEnvMaps(t *testing.T) {
	existing := map[string]string{
		"PUBLIC_KEY": "ssh-ed25519 aaa",
		"OTHER_VAR":  "world",
	}
	updates := map[string]string{
		"MY_VAR":    "updated",
		"OTHER_VAR": "changed",
	}

	merged := mergeEnvMaps(existing, updates)
	if merged["PUBLIC_KEY"] != "ssh-ed25519 aaa" {
		t.Fatalf("expected PUBLIC_KEY to be preserved, got %q", merged["PUBLIC_KEY"])
	}
	if merged["OTHER_VAR"] != "changed" {
		t.Fatalf("expected OTHER_VAR to be updated, got %q", merged["OTHER_VAR"])
	}
	if merged["MY_VAR"] != "updated" {
		t.Fatalf("expected MY_VAR to be added, got %q", merged["MY_VAR"])
	}
}

func TestParseUpdateEnv(t *testing.T) {
	env, err := parseUpdateEnv(`{"MY_VAR":"updated"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env["MY_VAR"] != "updated" {
		t.Fatalf("expected MY_VAR to be parsed, got %q", env["MY_VAR"])
	}
}

func TestMergeEnvMapsNil(t *testing.T) {
	if merged := mergeEnvMaps(nil, nil); merged != nil {
		t.Fatalf("expected nil merge result, got %#v", merged)
	}
}
