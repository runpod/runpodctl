package pod

import (
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
}

func assertEqualIDs(t *testing.T, want []string, got []*api.Pod) {
	if !reflect.DeepEqual(want, getIDs(got)) {
		t.Errorf("got %v, want %v", getIDs(got), want)
	}
}

func getIDs(pods []*api.Pod) []string {
	out := make([]string, len(pods))
	for i, p := range pods {
		out[i] = p.Id
	}
	return out
}
