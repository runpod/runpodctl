package pod

import (
	"os"
	"reflect"
	"testing"

	"github.com/runpod/runpodctl/api"
	"github.com/spf13/cobra"
)

func TestEditPod(t *testing.T) {

	t.Run("port validation", func(t *testing.T) {
		tests := []struct {
			name    string
			input   []string
			wantErr bool
		}{
			{
				name:    "http port",
				input:   []string{"80/http"},
				wantErr: false,
			},
			{
				name:    "tcp port",
				input:   []string{"80/tcp"},
				wantErr: false,
			},
			{
				name:    "invalid port only number",
				input:   []string{"80"},
				wantErr: true,
			},
			{
				name:    "invalid port wrong suffix",
				input:   []string{"80/httpx"},
				wantErr: true,
			},
			{
				name:    "max http ports exactly met",
				input:   repeat("80/http", MaxPorts),
				wantErr: false,
			},
			{
				name:    "max tcp ports exactly met",
				input:   repeat("80/tcp", MaxPorts),
				wantErr: false,
			},
			{
				name:    "max http ports exceeded",
				input:   repeat("80/http", MaxPorts+1),
				wantErr: true,
			},
			{
				name:    "max tcp ports exceeded",
				input:   repeat("80/tcp", MaxPorts+1),
				wantErr: true,
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				err := assertValidPorts(&tc.input)
				if err == nil && tc.wantErr {
					t.Errorf("expected error but got none")
				} else if err != nil && !tc.wantErr {
					t.Errorf("expected no error but one: %v", err)
				}
			})
		}
	})

	t.Run("option binding", func(t *testing.T) {
		type config struct {
			pInt   *int
			pStr   *string
			pSlice *[]string
		}

		tests := []struct {
			name string
			args []string
			want config
			cfg  config
		}{
			{
				name: "no-args",
				args: []string{""},
				want: config{},
			},
			{
				name: "int option",
				args: []string{"--int", "3"},
				want: config{pInt: ptr(3)},
			},
			{
				name: "string option",
				args: []string{"--str", "x"},
				want: config{pStr: ptr("x")},
			},
			{
				name: "slice option",
				args: []string{"--slice", "a,b"},
				want: config{pSlice: ptr([]string{"a", "b"})},
			},
			{
				name: "only touches opts explicitly specified on cmd line",
				args: []string{"--int", "1"},
				want: config{pStr: ptr("don't touch"), pInt: ptr(1)},
				cfg:  config{pStr: ptr("don't touch")},
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				// prepare
				b := make(bindings) // clear the global for each test
				got := tc.cfg

				// act
				cmd := &cobra.Command{Run: func(cmd *cobra.Command, args []string) {
					setUserProviderOptions(cmd, b)
				}}

				bind(b, cmd, &got.pInt, "int", 0, "")
				bind(b, cmd, &got.pStr, "str", "", "")
				bind(b, cmd, &got.pSlice, "slice", nil, "")

				// cobra testing boilerplate
				defer func(orig []string) { // restore in case other tests also depend on os args
					os.Args = orig
				}(os.Args)
				os.Args = append([]string{"program"}, tc.args...)
				cmd.Execute()

				// vertify
				if !reflect.DeepEqual(tc.want, got) {
					t.Errorf("want: %v, got: %v", tc.want, got)
				}
			})
		}
	})

	t.Run("conditional input assignment", func(t *testing.T) {
		tests := []struct {
			name string
			conf EditPodCmdConfig
			want api.PodEditJobInput
			pod  api.Pod
		}{
			{
				name: "no change on nil values",
				conf: EditPodCmdConfig{},
				want: api.PodEditJobInput{ImageName: "alpine"},
				pod:  api.Pod{ImageName: "alpine"},
			},
			{
				name: "change on non-nil values",
				conf: EditPodCmdConfig{imageName: ptr("ubuntu")},
				want: api.PodEditJobInput{ImageName: "ubuntu"},
				pod:  api.Pod{ImageName: "alpine"},
			},
			{
				name: "containerDiskInGb",
				conf: EditPodCmdConfig{containerDiskInGb: ptr(3)},
				want: api.PodEditJobInput{ContainerDiskInGb: 3},
			},
			{
				name: "dockerArgs",
				conf: EditPodCmdConfig{dockerArgs: ptr("sleep 1")},
				want: api.PodEditJobInput{DockerArgs: "sleep 1"},
			},
			{
				name: "env",
				conf: EditPodCmdConfig{env: ptr([]string{"foo=1", "bar=baz"})},
				want: api.PodEditJobInput{Env: []*api.PodEnv{
					{Key: "foo", Value: "1"},
					{Key: "bar", Value: "baz"},
				}},
			},
			{
				name: "image",
				conf: EditPodCmdConfig{imageName: ptr("alpine")},
				want: api.PodEditJobInput{ImageName: "alpine"},
			},
			{
				name: "ports",
				conf: EditPodCmdConfig{ports: ptr([]string{"80/http", "22/tcp"})},
				want: api.PodEditJobInput{Ports: "80/http,22/tcp"},
			},
			{
				name: "volumeInGb",
				conf: EditPodCmdConfig{volumeInGb: ptr(7)},
				want: api.PodEditJobInput{VolumeInGb: 7},
			},
			{
				name: "volumeInGb",
				conf: EditPodCmdConfig{volumeMountPath: ptr("/tmp")},
				want: api.PodEditJobInput{VolumeMountPath: "/tmp"},
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				input := newInputFromPod(&tc.pod)

				populatePodEditJobInput(&tc.pod, input, &tc.conf)

				if !reflect.DeepEqual(tc.want, *input) {
					t.Errorf("want: %v, got: %v", tc.want, input)
				}
			})
		}
	})
}

func repeat(s string, n int) []string {
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = s
	}
	return out
}

func ptr[T any](v T) *T {
	return &v
}
