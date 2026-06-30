package template

import (
	"reflect"
	"strings"
	"testing"

	"github.com/runpod/runpodctl/internal/api"
)

func TestParsePortLabels(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []api.TemplatePortConfig
	}{
		{
			name: "pairs",
			raw:  "22/tcp=SSH, 8888/http=Jupyter Lab",
			want: []api.TemplatePortConfig{{Port: "22", Name: "SSH"}, {Port: "8888", Name: "Jupyter Lab"}},
		},
		{
			name: "json object",
			raw:  `{"8888":"Jupyter Lab","22/tcp":"SSH"}`,
			want: []api.TemplatePortConfig{{Port: "22", Name: "SSH"}, {Port: "8888", Name: "Jupyter Lab"}},
		},
		{
			name: "json array",
			raw:  `[{"port":"22/tcp","name":"SSH"},{"port":"8888","name":"Jupyter Lab"}]`,
			want: []api.TemplatePortConfig{{Port: "22", Name: "SSH"}, {Port: "8888", Name: "Jupyter Lab"}},
		},
		{
			name: "empty clears labels",
			raw:  "",
			want: []api.TemplatePortConfig{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePortLabels(tt.raw)
			if err != nil {
				t.Fatalf("parsePortLabels() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parsePortLabels() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParsePortLabelsRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		raw       string
		wantError string
	}{
		{raw: "22", wantError: "expected port=name"},
		{raw: "abc=ssh", wantError: "invalid port"},
		{raw: "22/udp=ssh", wantError: "unsupported protocol"},
		{raw: "70000=ssh", wantError: "outside the valid range"},
		{raw: "22=", wantError: "name is required"},
		{raw: "22=ssh,22/tcp=shell", wantError: "duplicate port label"},
	}

	for _, tt := range tests {
		_, err := parsePortLabels(tt.raw)
		if err == nil || !strings.Contains(err.Error(), tt.wantError) {
			t.Fatalf("parsePortLabels(%q) error = %v, want substring %q", tt.raw, err, tt.wantError)
		}
	}
}

func TestValidatePortLabelsAgainstPorts(t *testing.T) {
	labels := []api.TemplatePortConfig{{Port: "22", Name: "SSH"}, {Port: "8888", Name: "Jupyter Lab"}}
	if err := validatePortLabelsAgainstPorts(labels, "22/tcp,8888/http"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := validatePortLabelsAgainstPorts(labels, "22/tcp"); err == nil || !strings.Contains(err.Error(), "8888") {
		t.Fatalf("expected missing port error, got %v", err)
	}

	if err := validatePortLabelsAgainstPorts(labels[:1], "22/udp"); err == nil || !strings.Contains(err.Error(), "unsupported protocol") {
		t.Fatalf("expected invalid protocol error, got %v", err)
	}
}
