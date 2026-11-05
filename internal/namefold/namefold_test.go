package namefold

import "testing"

func TestKeyCollidesFilesystemAliases(t *testing.T) {
	pairs := [][2]string{
		{"Model.bin", "model.bin"},
		{"d/ſ", "d/s"},
		{"é", "é"},
	}
	for _, p := range pairs {
		if Key(p[0]) != Key(p[1]) {
			t.Errorf("Key(%q) != Key(%q)", p[0], p[1])
		}
	}
	if Key("a") == Key("b") {
		t.Error("distinct names collided")
	}
}
