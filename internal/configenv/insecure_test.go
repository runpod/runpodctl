package configenv

import (
	"errors"
	"strings"
	"testing"
)

func TestCheckAPIURL(t *testing.T) {
	t.Setenv(AllowInsecureAPIEnv, "")
	SetAllowInsecureAPI(false)
	t.Cleanup(func() { SetAllowInsecureAPI(false) })

	allowed := []string{
		"https://api.runpod.io/graphql",
		"HTTPS://api.runpod.io/graphql",
		"http://localhost:4000/graphql",
		"http://LOCALHOST/graphql",
		"http://127.0.0.1:4000/graphql",
		"http://127.1.2.3/graphql",
		"http://[::1]:4000/graphql",
		"",
	}
	for _, u := range allowed {
		if err := CheckAPIURL(u); err != nil {
			t.Errorf("expected %q allowed, got %v", u, err)
		}
	}

	refused := []string{
		"http://api.runpod.io/graphql",
		"http://user:pass@api.example.com/graphql?token=abc",
		"http://10.0.0.5:4000/graphql",
		"http://localhost.evil.example/graphql",
		"http://127.0.0.1.evil.example/graphql",
	}
	for _, u := range refused {
		err := CheckAPIURL(u)
		if !errors.Is(err, ErrInsecureAPIURL) {
			t.Errorf("expected %q refused with ErrInsecureAPIURL, got %v", u, err)
			continue
		}
		for _, leak := range []string{"user:pass", "token=abc"} {
			if strings.Contains(err.Error(), leak) {
				t.Errorf("error for %q leaks %q: %v", u, leak, err)
			}
		}
		if !strings.Contains(err.Error(), "--insecure") || !strings.Contains(err.Error(), AllowInsecureAPIEnv) {
			t.Errorf("error should say how to opt in: %v", err)
		}
	}
}

func TestCheckAPIURL_OptIn(t *testing.T) {
	const insecure = "http://api.example.com/graphql"
	t.Cleanup(func() { SetAllowInsecureAPI(false) })

	for _, tc := range []struct {
		name  string
		env   string
		flag  bool
		allow bool
	}{
		{"neither", "", false, false},
		{"flag", "", true, true},
		{"env 1", "1", false, true},
		{"env true", "true", false, true},
		{"env 0", "0", false, false},
		{"env garbage", "please", false, false},
	} {
		t.Setenv(AllowInsecureAPIEnv, tc.env)
		SetAllowInsecureAPI(tc.flag)
		err := CheckAPIURL(insecure)
		if tc.allow && err != nil {
			t.Errorf("%s: expected allowed, got %v", tc.name, err)
		}
		if !tc.allow && !errors.Is(err, ErrInsecureAPIURL) {
			t.Errorf("%s: expected refusal, got %v", tc.name, err)
		}
	}
}
