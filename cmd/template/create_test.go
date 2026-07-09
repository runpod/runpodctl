package template

import (
	"testing"

	"github.com/runpod/runpodctl/internal/api"
)

// TestCreatePortLabelOverridesCarriesStartCommand is the create-side half of the
// NEW-1 guard: the overrides handed to the port-label write must carry the just-set
// start command as the backend's canonical dockerArgs encoding, so a briefly-stale
// post-create read can't blank it.
func TestCreatePortLabelOverridesCarriesStartCommand(t *testing.T) {
	cases := []struct {
		name       string
		entrypoint []string
		startCmd   []string
		want       *string
	}{
		{
			name:     "start command only",
			startCmd: []string{"python -u app.py"},
			want:     strPtr(`{"cmd":["python -u app.py"]}`),
		},
		{
			name:       "entrypoint and start command",
			entrypoint: []string{"/bin/sh", "-c"},
			startCmd:   []string{"python -u app.py"},
			want:       strPtr(`{"cmd":["python -u app.py"],"entrypoint":["/bin/sh","-c"]}`),
		},
		{
			name:       "entrypoint only",
			entrypoint: []string{"/bin/sh"},
			want:       strPtr(`{"entrypoint":["/bin/sh"]}`),
		},
		{
			name: "no command leaves override unset",
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := &api.TemplateCreateRequest{
				DockerEntrypoint: tc.entrypoint,
				DockerStartCmd:   tc.startCmd,
			}
			got := createPortLabelOverrides(req).DockerArgs
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("DockerArgs = %q, want nil", *got)
			case tc.want != nil && got == nil:
				t.Fatalf("DockerArgs = nil, want %q", *tc.want)
			case tc.want != nil && *got != *tc.want:
				t.Fatalf("DockerArgs = %q, want %q", *got, *tc.want)
			}
		})
	}
}

func strPtr(s string) *string { return &s }
