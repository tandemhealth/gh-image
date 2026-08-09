package repo

import (
	"fmt"
	"strings"
	"testing"
)

type fakeRunner struct {
	gitOut string
	gitErr error
	calls  int
}

func (f *fakeRunner) run(name string, args ...string) ([]byte, error) {
	if name != "git" {
		return nil, fmt.Errorf("unexpected command %q", name)
	}
	f.calls++
	return []byte(f.gitOut), f.gitErr
}

func TestFromRemote(t *testing.T) {
	tests := []struct {
		remote string
		owner  string
		repo   string
	}{
		{"git@github.com:octocat/hello.git\n", "octocat", "hello"},
		{"https://github.com/octocat/hello", "octocat", "hello"},
	}
	for _, tc := range tests {
		f := &fakeRunner{gitOut: tc.remote}
		owner, name, err := fromRemote(f.run)
		if err != nil || owner != tc.owner || name != tc.repo {
			t.Fatalf("remote=%q got=%s/%s err=%v", tc.remote, owner, name, err)
		}
	}
}

func TestFromRemoteRejectsUnsupportedRemote(t *testing.T) {
	f := &fakeRunner{gitOut: "https://gitlab.com/x/y.git"}
	_, _, err := fromRemote(f.run)
	if err == nil || !strings.Contains(err.Error(), "could not parse GitHub") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveExplicitRepositoryDoesNotCallGitOrGitHub(t *testing.T) {
	f := &fakeRunner{}
	info, err := resolve(f.run, "octocat", "hello")
	if err != nil || *info != (Info{Owner: "octocat", Name: "hello"}) || f.calls != 0 {
		t.Fatalf("info=%#v calls=%d err=%v", info, f.calls, err)
	}
}

func TestResolveInfersRepositoryWithoutGitHubCall(t *testing.T) {
	f := &fakeRunner{gitOut: "git@github.com:octocat/hello.git\n"}
	info, err := resolve(f.run, "", "")
	if err != nil || *info != (Info{Owner: "octocat", Name: "hello"}) || f.calls != 1 {
		t.Fatalf("info=%#v calls=%d err=%v", info, f.calls, err)
	}
}
