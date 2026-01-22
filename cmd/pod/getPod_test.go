package pod

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/runpod/runpodctl/api"
)

func TestGetPod(t *testing.T) {
	pods := []*api.Pod{
		{Id: "p1"},
		{Id: "p2"},
		{Id: "p3"},
	}

	t.Run("filter pods", func(t *testing.T) {

		t.Run("without argument", func(t *testing.T) {
			got := filter(pods, nil)
			assertEqualIDs(t, getIDs(pods), got)
		})

		t.Run("with argument", func(t *testing.T) {
			got := filter(pods, []string{"p2"})
			assertEqualIDs(t, []string{"p2"}, got)
		})
	})

	t.Run("output json", func(t *testing.T) {
		t.Run("with results", func(t *testing.T) {
			var buf bytes.Buffer

			check(t, toJSON(&buf, pods))

			var got []*api.Pod
			check(t, json.NewDecoder(bytes.NewReader(buf.Bytes())).Decode(&got))

			assertEqualIDs(t, []string{"p1", "p2", "p3"}, got)
		})

		t.Run("empty output", func(t *testing.T) {
			var buf bytes.Buffer

			_ = toJSON(&buf, []*api.Pod{})
			got := string(buf.Bytes())

			if got != "[]" {
				t.Errorf("got %v, want %v", got, "[]")
			}
		})
	})

}

func assertEqualIDs(t *testing.T, want []string, got []*api.Pod) {
	if !reflect.DeepEqual(want, getIDs(got)) {
		t.Errorf("got %v, want %v", getIDs(got), want)
	}
}

func check(t *testing.T, err error) {
	if err != nil {
		t.Fatal(err)
	}
}

func getIDs(pods []*api.Pod) []string {
	out := make([]string, len(pods))
	for i, p := range pods {
		out[i] = p.Id
	}
	return out
}
