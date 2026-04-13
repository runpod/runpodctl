package cmd

import "testing"

func TestSSHInfo_NotDeprecated(t *testing.T) {
	if sshInfoCmd.Deprecated != "" {
		t.Errorf("expected ssh info not to be deprecated")
	}
}

func TestSSHInfo_RequiresPodID(t *testing.T) {
	if err := sshInfoCmd.Args(sshInfoCmd, []string{}); err == nil {
		t.Error("expected ssh info to require a pod id")
	}
	if err := sshInfoCmd.Args(sshInfoCmd, []string{"pod123"}); err != nil {
		t.Errorf("unexpected error for pod id: %v", err)
	}
}

func TestSSHConnect_Deprecated(t *testing.T) {
	if sshConnectCmd.Deprecated == "" {
		t.Errorf("expected ssh connect to be deprecated")
	}
}

func TestSSHConnect_LegacyArgs(t *testing.T) {
	if err := sshConnectCmd.Args(sshConnectCmd, []string{}); err != nil {
		t.Errorf("unexpected error for no args: %v", err)
	}
	if err := sshConnectCmd.Args(sshConnectCmd, []string{"pod123"}); err != nil {
		t.Errorf("unexpected error for pod id: %v", err)
	}
	if err := sshConnectCmd.Args(sshConnectCmd, []string{"a", "b"}); err == nil {
		t.Error("expected error for too many args")
	}
}

func TestSSHCmd_HasInfoCommand(t *testing.T) {
	found := false
	for _, cmd := range sshCmd.Commands() {
		if cmd.Use == "info <pod-id>" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected ssh info command to exist")
	}
}

func TestSSHCmd_HasRemoveKeyCommand(t *testing.T) {
	found := false
	for _, cmd := range sshCmd.Commands() {
		if cmd.Use == "remove-key" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected ssh remove-key command to exist")
	}
}

func TestSSHConnect_Hidden(t *testing.T) {
	if !sshConnectCmd.Hidden {
		t.Error("expected ssh connect to be hidden")
	}
}

func TestSSHRemoveKey_RequiresIdentifier(t *testing.T) {
	origName := sshKeyName
	origFingerprint := sshKeyFingerprint
	t.Cleanup(func() {
		sshKeyName = origName
		sshKeyFingerprint = origFingerprint
	})

	sshKeyName = ""
	sshKeyFingerprint = ""
	if err := sshRemoveKeyCmd.PreRunE(sshRemoveKeyCmd, nil); err == nil {
		t.Error("expected ssh remove-key to require an identifier")
	}

	sshKeyName = "temp-key"
	if err := sshRemoveKeyCmd.PreRunE(sshRemoveKeyCmd, nil); err != nil {
		t.Errorf("unexpected error for name: %v", err)
	}

	sshKeyName = ""
	sshKeyFingerprint = "SHA256:test"
	if err := sshRemoveKeyCmd.PreRunE(sshRemoveKeyCmd, nil); err != nil {
		t.Errorf("unexpected error for fingerprint: %v", err)
	}
}
