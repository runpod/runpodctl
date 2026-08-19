package exec

import (
	"strings"
	"testing"
)

// TestRemotePythonCmd_ReportsFailure pins the exit code. This command used Run,
// which cannot return an error, so a failed run printed to stderr and the process
// still exited 0 — meaning `runpodctl exec python train.py && deploy` deployed on
// a failed run.
func TestRemotePythonCmd_ReportsFailure(t *testing.T) {
	if RemotePythonCmd.Run != nil {
		t.Error("Run must stay nil: a Run handler cannot report failure, so the process exits 0")
	}
	if RemotePythonCmd.RunE == nil {
		t.Fatal("expected RunE, so the error reaches the Execute sink and sets exit 1")
	}

	// a HOME with no ssh key makes PodSSHConnection fail while reading the key,
	// which happens before it starts polling for the pod — so this exercises the
	// real error path with no network and no five-minute wait.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	err := RemotePythonCmd.RunE(RemotePythonCmd, []string{"nonexistent.py"})
	if err == nil {
		t.Fatal("expected an error from a failed run, got nil (the caller would see exit 0)")
	}
	if !strings.Contains(err.Error(), "executing python over ssh") {
		t.Errorf("error should name what failed, got %q", err)
	}
}

func TestRemotePythonCmd_Flags(t *testing.T) {
	flags := RemotePythonCmd.Flags()

	if flags.Lookup("pod_id") == nil {
		t.Error("expected --pod_id flag")
	}

	pythonFlag := flags.Lookup("python")
	if pythonFlag == nil {
		t.Error("expected --python flag")
		return
	}
	if pythonFlag.DefValue != "python3" {
		t.Errorf("expected default python3, got %s", pythonFlag.DefValue)
	}
}
