package exec

import "testing"

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
