package cookies

import "testing"

func TestNewSessionCookie(t *testing.T) {
	c := NewSessionCookie("  raw value  ")
	if c.Name != "user_session" || c.Value != "  raw value  " {
		t.Errorf("unexpected cookie identity: Name=%q Value=%q", c.Name, c.Value)
	}
	if c.Domain != "github.com" || c.Path != "/" || !c.Secure || !c.HttpOnly {
		t.Errorf("unexpected shape: Domain=%q Path=%q Secure=%v HttpOnly=%v", c.Domain, c.Path, c.Secure, c.HttpOnly)
	}
}
