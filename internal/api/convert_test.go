package api

import (
	"reflect"
	"strings"
	"testing"

	"github.com/runpod/go-sdk/v2/lib/types"
)

// requireAllFieldsSet fails for every exported field of v, recursively, that
// holds its zero value, except the dotted paths in allowZero. A converter test
// feeds a fully populated SDK value, so a zero field means a mapping was lost.
func requireAllFieldsSet(t *testing.T, v any, allowZero ...string) {
	t.Helper()
	allowed := map[string]bool{}
	for _, p := range allowZero {
		allowed[p] = true
	}
	var walk func(path string, v reflect.Value)
	walk = func(path string, v reflect.Value) {
		switch v.Kind() {
		case reflect.Pointer, reflect.Interface:
			if !v.IsNil() {
				walk(path, v.Elem())
			}
		case reflect.Slice, reflect.Array:
			for i := range v.Len() {
				walk(path, v.Index(i))
			}
		case reflect.Map:
			for _, k := range v.MapKeys() {
				walk(path, v.MapIndex(k))
			}
		case reflect.Struct:
			typ := v.Type()
			for i := range typ.NumField() {
				f := typ.Field(i)
				if !f.IsExported() || f.Tag.Get("json") == "-" {
					continue
				}
				p := strings.TrimPrefix(path+"."+f.Name, ".")
				if v.Field(i).IsZero() {
					if !allowed[p] {
						t.Errorf("%s is zero after conversion", p)
					}
					continue
				}
				walk(p, v.Field(i))
			}
		}
	}
	walk("", reflect.ValueOf(v))
}

func TestNetworkVolumeFromSDK(t *testing.T) {
	got := networkVolumeFromSDK(types.NetworkVolume{
		Id: "vol-abc123", Name: "models", Size: 50, DataCenter: "EU-RO-1", Type: "STANDARD",
	})
	requireAllFieldsSet(t, got)
	want := NetworkVolume{ID: "vol-abc123", Name: "models", Size: 50, DataCenterID: "EU-RO-1"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
