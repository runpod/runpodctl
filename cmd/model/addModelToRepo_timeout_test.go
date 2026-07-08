package model

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runpod/runpodctl/api"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func TestSetModelGraphQLTimeoutWithoutInheritedFlag(t *testing.T) {
	viper.Reset()

	cmd := &cobra.Command{Use: "add"}
	// CLAUDE.md: informational output must not corrupt stdout for JSON
	// consumers — the "defaulting graphql timeout" notice must land on stderr.
	stdout, stderr := captureStdStreams(t, func() {
		setModelGraphQLTimeout(cmd)
	})

	if got := viper.GetDuration(api.GraphQLTimeoutKey); got != modelGraphQLTimeoutValue {
		t.Fatalf("expected graphql timeout %s, got %s", modelGraphQLTimeoutValue, got)
	}
	if stdout != "" {
		t.Fatalf("stdout must remain empty, got %q", stdout)
	}
	if stderr == "" {
		t.Fatal("expected timeout-default notice on stderr, got empty")
	}
}

func TestSetModelGraphQLTimeoutRespectsExistingConfiguredValue(t *testing.T) {
	viper.Reset()

	existing := 2 * time.Minute
	viper.Set(api.GraphQLTimeoutKey, existing)

	cmd := &cobra.Command{Use: "add"}
	setModelGraphQLTimeout(cmd)

	if got := viper.GetDuration(api.GraphQLTimeoutKey); got != existing {
		t.Fatalf("expected graphql timeout to remain %s, got %s", existing, got)
	}
}

func TestSetModelGraphQLTimeoutSetsInheritedFlagWhenUnchanged(t *testing.T) {
	viper.Reset()

	root := &cobra.Command{Use: "runpodctl"}
	root.PersistentFlags().Duration(graphqlTimeoutFlagName, 10*time.Second, "graphql timeout")
	cmd := &cobra.Command{Use: "add"}
	root.AddCommand(cmd)

	setModelGraphQLTimeout(cmd)

	flag := cmd.InheritedFlags().Lookup(graphqlTimeoutFlagName)
	if flag == nil {
		t.Fatal("expected inherited graphql-timeout flag")
	}
	if got := flag.Value.String(); got != modelGraphQLTimeoutValue.String() {
		t.Fatalf("expected inherited flag value %s, got %s", modelGraphQLTimeoutValue, got)
	}
	if got := viper.GetDuration(api.GraphQLTimeoutKey); got != modelGraphQLTimeoutValue {
		t.Fatalf("expected graphql timeout %s, got %s", modelGraphQLTimeoutValue, got)
	}
}

func TestSetModelGraphQLTimeoutSkipsWhenInheritedFlagChanged(t *testing.T) {
	viper.Reset()

	root := &cobra.Command{Use: "runpodctl"}
	root.PersistentFlags().Duration(graphqlTimeoutFlagName, 10*time.Second, "graphql timeout")
	if err := root.PersistentFlags().Set(graphqlTimeoutFlagName, "45s"); err != nil {
		t.Fatalf("failed to set inherited flag: %v", err)
	}

	cmd := &cobra.Command{Use: "add"}
	root.AddCommand(cmd)

	setModelGraphQLTimeout(cmd)

	if got := viper.GetDuration(api.GraphQLTimeoutKey); got != 0 {
		t.Fatalf("expected graphql timeout to remain unset, got %s", got)
	}
}

func TestUploadModelFilesUsesModelVersionUUIDAfterFirstFile(t *testing.T) {
	oldCreateModelRepoUpload := createModelRepoUpload
	oldCompleteModelUploadFile := completeModelUploadFile
	oldCompleteModelRepoUpload := completeModelRepoUpload
	t.Cleanup(func() {
		createModelRepoUpload = oldCreateModelRepoUpload
		completeModelUploadFile = oldCompleteModelUploadFile
		completeModelRepoUpload = oldCompleteModelRepoUpload
	})

	files := []modelFile{
		{AbsolutePath: "/tmp/a.bin", RelativePath: "a.bin", Size: 1},
		{AbsolutePath: "/tmp/b.bin", RelativePath: "b.bin", Size: 2},
		{AbsolutePath: "/tmp/c.bin", RelativePath: "c.bin", Size: 3},
	}

	var calls []api.CreateModelRepoUploadInput
	createModelRepoUpload = func(input *api.CreateModelRepoUploadInput) (*api.ModelRepoMutationResult, error) {
		calls = append(calls, *input)
		return &api.ModelRepoMutationResult{
			Success: true,
			Version: &api.ModelVersion{
				UUID: "version-uuid",
				Hash: "version-hash",
			},
			Upload: &api.ModelRepoUpload{
				SessionID: "session-" + input.FileName,
				Key:       "key-" + input.FileName,
			},
		}, nil
	}
	var events []string
	var uploadedArtifacts []string
	completeModelUploadFile = func(upload *api.ModelRepoUpload, artifactPath string, progress modelUploadProgress) error {
		events = append(events, "upload:"+artifactPath)
		uploadedArtifacts = append(uploadedArtifacts, artifactPath)
		return nil
	}
	var completedSessions []string
	completeModelRepoUpload = func(sessionID string) (*api.CompleteModelRepoUploadResult, error) {
		events = append(events, "complete:"+sessionID)
		completedSessions = append(completedSessions, sessionID)
		return &api.CompleteModelRepoUploadResult{
			SessionID: sessionID,
			Status:    "completed",
		}, nil
	}

	uploadedFiles, err := uploadModelFiles(files, &api.CreateModelRepoUploadInput{Name: "test-model"})
	if err != nil {
		t.Fatalf("uploadModelFiles returned error: %v", err)
	}

	if len(calls) != len(files) {
		t.Fatalf("expected %d createModelRepoUpload calls, got %d", len(files), len(calls))
	}
	if calls[0].ModelVersionUUID != "" {
		t.Fatalf("expected first upload call to omit modelVersionUuid, got %q", calls[0].ModelVersionUUID)
	}
	for i := 1; i < len(calls); i++ {
		if calls[i].ModelVersionUUID != "version-uuid" {
			t.Fatalf("expected call %d to use modelVersionUuid %q, got %q", i, "version-uuid", calls[i].ModelVersionUUID)
		}
	}
	if len(uploadedArtifacts) != len(files) {
		t.Fatalf("expected %d uploaded artifacts, got %d", len(files), len(uploadedArtifacts))
	}
	for i, file := range files {
		if uploadedArtifacts[i] != file.AbsolutePath {
			t.Fatalf("expected uploaded artifact %d to be %q, got %q", i, file.AbsolutePath, uploadedArtifacts[i])
		}
	}
	expectedCompletedSessions := []string{"session-a.bin", "session-b.bin", "session-c.bin"}
	if len(completedSessions) != len(expectedCompletedSessions) {
		t.Fatalf("expected %d completed sessions, got %d", len(expectedCompletedSessions), len(completedSessions))
	}
	for i, expected := range expectedCompletedSessions {
		if completedSessions[i] != expected {
			t.Fatalf("expected completed session %d to be %q, got %q", i, expected, completedSessions[i])
		}
	}
	if len(uploadedFiles) != len(expectedCompletedSessions) {
		t.Fatalf("expected %d uploaded files, got %d", len(expectedCompletedSessions), len(uploadedFiles))
	}
	for i, expected := range expectedCompletedSessions {
		if uploadedFiles[i].SessionID != expected {
			t.Fatalf("expected uploaded file session %d to be %q, got %q", i, expected, uploadedFiles[i].SessionID)
		}
		if uploadedFiles[i].Status != "completed" {
			t.Fatalf("expected uploaded file status %d to be completed, got %q", i, uploadedFiles[i].Status)
		}
	}
	expectedEvents := []string{
		"upload:/tmp/a.bin",
		"upload:/tmp/b.bin",
		"upload:/tmp/c.bin",
		"complete:session-a.bin",
		"complete:session-b.bin",
		"complete:session-c.bin",
	}
	if len(events) != len(expectedEvents) {
		t.Fatalf("expected %d upload/completion events, got %d", len(expectedEvents), len(events))
	}
	for i, expected := range expectedEvents {
		if events[i] != expected {
			t.Fatalf("expected event %d to be %q, got %q", i, expected, events[i])
		}
	}
}

type recordingModelUploadProgress struct {
	bytes    int64
	finished bool
	cleared  bool
}

func (p *recordingModelUploadProgress) Add64(n int64) error {
	p.bytes += n
	return nil
}

func (p *recordingModelUploadProgress) Finish() error {
	p.finished = true
	return nil
}

func (p *recordingModelUploadProgress) Clear() error {
	p.cleared = true
	return nil
}

func TestProgressReaderTracksBytes(t *testing.T) {
	progress := &recordingModelUploadProgress{}
	reader := progressReader{
		reader:   strings.NewReader("abcdef"),
		progress: progress,
	}

	if _, err := io.Copy(io.Discard, reader); err != nil {
		t.Fatalf("copy progress reader: %v", err)
	}
	if progress.bytes != 6 {
		t.Fatalf("expected 6 progress bytes, got %d", progress.bytes)
	}
}

func TestPrintCompletedModelUploadSizeWritesToStderr(t *testing.T) {
	stdout, stderr := captureStdStreams(t, func() {
		printCompletedModelUploadSize(123)
	})

	if stdout != "" {
		t.Fatalf("stdout must remain empty, got %q", stdout)
	}
	if stderr != "model size: 123 bytes\n" {
		t.Fatalf("expected model size on stderr, got %q", stderr)
	}
}

func TestCompleteModelUploadWithProgressTracksMultipartBytes(t *testing.T) {
	artifactPath := filepath.Join(t.TempDir(), "model.bin")
	if err := os.WriteFile(artifactPath, []byte("abcdef"), 0600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	var uploadedBytes int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read upload body: %v", err)
			}
			uploadedBytes += int64(len(body))
			w.Header().Set("ETag", `"etag"`)
			w.WriteHeader(http.StatusOK)
		case http.MethodPost:
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	t.Cleanup(server.Close)

	progress := &recordingModelUploadProgress{}
	err := completeModelUploadWithProgress(&api.ModelRepoUpload{
		PartSizeBytes: 3,
		CompleteURL:   server.URL,
		Parts: []*api.ModelRepoUploadPart{
			{PartNumber: 2, URL: server.URL},
			{PartNumber: 1, URL: server.URL},
		},
	}, artifactPath, progress)
	if err != nil {
		t.Fatalf("complete upload: %v", err)
	}
	if uploadedBytes != 6 {
		t.Fatalf("expected server to receive 6 bytes, got %d", uploadedBytes)
	}
	if progress.bytes != 6 {
		t.Fatalf("expected progress to receive 6 bytes, got %d", progress.bytes)
	}
}

func TestCompleteModelUploadAllowsZeroByteFileWithNoParts(t *testing.T) {
	artifactPath := filepath.Join(t.TempDir(), "empty.bin")
	if err := os.WriteFile(artifactPath, nil, 0600); err != nil {
		t.Fatalf("write empty artifact: %v", err)
	}

	err := completeModelUpload(&api.ModelRepoUpload{}, artifactPath)
	if err != nil {
		t.Fatalf("expected zero-byte upload with no parts to succeed, got %v", err)
	}
}
