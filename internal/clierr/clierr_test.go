package clierr

import (
	"errors"
	"testing"
)

func TestUsagefCarriesCode(t *testing.T) {
	err := Usagef("payload from %s is not valid json", "--input")

	if err.Error() != "payload from --input is not valid json" {
		t.Errorf("message = %q", err.Error())
	}

	// output.Error picks the code up through the same interface assertion.
	var coder interface{ ErrorCode() string }
	if !errors.As(err, &coder) {
		t.Fatal("expected the error to expose ErrorCode()")
	}
	if coder.ErrorCode() != "usage_error" {
		t.Errorf("code = %q, want usage_error", coder.ErrorCode())
	}
}

func TestUsageErrorUnwraps(t *testing.T) {
	sentinel := errors.New("boom")
	err := &UsageError{Err: sentinel}

	if !errors.Is(err, sentinel) {
		t.Error("expected the wrapped cause to stay matchable with errors.Is")
	}
}
