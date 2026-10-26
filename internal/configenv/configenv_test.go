package configenv

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func loadTOML(t *testing.T, body string) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.SetConfigType("toml")
	if err := viper.ReadConfig(strings.NewReader(body)); err != nil {
		t.Fatalf("read config: %v", err)
	}
}

func TestAPIKey(t *testing.T) {
	cases := []struct {
		name string
		env  string
		toml string
		want string
	}{
		{"top-level apikey", "", "apikey = \"top\"\n", "top"},
		{"flash login default table", "", "[default]\napi_key = \"flash\"\n", "flash"},
		{"top-level wins over flash", "", "apikey = \"top\"\n[default]\napi_key = \"flash\"\n", "top"},
		{"env wins over config", "env", "[default]\napi_key = \"flash\"\n", "env"},
		{"nothing set", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(APIKeyEnv, tc.env)
			loadTOML(t, tc.toml)
			if got := APIKey(); got != tc.want {
				t.Fatalf("APIKey() = %q, want %q", got, tc.want)
			}
		})
	}
}
