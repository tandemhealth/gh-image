package credential

import (
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestSetAndGet(t *testing.T) {
	keyring.MockInit()
	if err := Set("session-value"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "session-value" {
		t.Fatalf("Get = %q, want session-value", got)
	}
}

func TestGetMissingCredential(t *testing.T) {
	keyring.MockInit()
	_, err := Get()
	if err == nil || !strings.Contains(err.Error(), "gh image auth-store") {
		t.Fatalf("Get error = %v, want auth-store guidance", err)
	}
}
