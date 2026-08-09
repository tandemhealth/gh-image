package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/tandemhealth/gh-image/internal/release"
	"github.com/tandemhealth/gh-image/internal/repo"
	"github.com/tandemhealth/gh-image/internal/upload"
)

func okDeps() deps {
	return deps{
		resolveRepo: func(owner, name string) (*repo.Info, error) {
			if owner == "" {
				owner, name = "octo", "hello"
			}
			return &repo.Info{Owner: owner, Name: name}, nil
		},
		evidenceRootEnv: "/evidence",
		openEvidence: func(root, path string) (*upload.Evidence, error) {
			return nil, nil
		},
		initRelease: func(*repo.Info) (release.InitResult, error) {
			return release.InitResult{Created: true, Release: release.Release{HTMLURL: "https://github.com/octo/hello/releases/tag/gh-image-evidence"}}, nil
		},
		checkAccess: func(*repo.Info) (release.Release, error) { return release.Release{}, nil },
		uploadEvidence: func(*repo.Info, *upload.Evidence) (string, error) {
			return "![shot.png](https://github.com/octo/hello/releases/download/gh-image-evidence/" + strings.Repeat("a", 64) + ".png)", nil
		},
	}
}

func runWith(t *testing.T, args []string, d deps) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr, d)
	return code, stdout.String(), stderr.String()
}

func TestRunHelpHasNoCredentialSetup(t *testing.T) {
	code, stdout, _ := runWith(t, []string{"--help"}, okDeps())
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	for _, prohibited := range []string{"auth-store", "check-token", "--token", "user_session", "GH_SESSION_TOKEN"} {
		if strings.Contains(stdout, prohibited) {
			t.Fatalf("help contains removed credential interface %q: %s", prohibited, stdout)
		}
	}
	if !strings.Contains(stdout, "existing GitHub CLI login") || !strings.Contains(stdout, "gh image init") {
		t.Fatalf("help does not explain automatic authentication and initialization: %s", stdout)
	}
}

func TestRunRejectsRemovedCredentialInterfaces(t *testing.T) {
	for _, args := range [][]string{{"auth-store"}, {"check-token"}, {"--token", "secret", "/evidence/shot.png"}} {
		code, stdout, stderr := runWith(t, args, okDeps())
		if code != 1 || stdout != "" {
			t.Fatalf("args=%q code=%d stdout=%q stderr=%q", args, code, stdout, stderr)
		}
		if !strings.Contains(stderr, "removed") && !strings.Contains(stderr, "unknown flag --token") {
			t.Fatalf("args=%q stderr=%q", args, stderr)
		}
	}
}

func TestRunValidatesEvidenceBeforeRepositoryOrGitHub(t *testing.T) {
	d := okDeps()
	d.openEvidence = func(string, string) (*upload.Evidence, error) {
		return nil, fmt.Errorf("outside evidence root")
	}
	d.resolveRepo = func(string, string) (*repo.Info, error) {
		t.Fatal("repository resolution ran before evidence validation")
		return nil, nil
	}
	d.uploadEvidence = func(*repo.Info, *upload.Evidence) (string, error) {
		t.Fatal("GitHub upload ran before evidence validation")
		return "", nil
	}
	code, _, stderr := runWith(t, []string{"/evidence/shot.png"}, d)
	if code != 1 || !strings.Contains(stderr, "outside evidence root") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestRunUploadUsesExplicitRepository(t *testing.T) {
	d := okDeps()
	var gotOwner, gotRepo string
	d.resolveRepo = func(owner, name string) (*repo.Info, error) {
		gotOwner, gotRepo = owner, name
		return &repo.Info{Owner: owner, Name: name}, nil
	}
	code, stdout, stderr := runWith(t, []string{"--repo", "acme/widgets", "/evidence/shot.png"}, d)
	if code != 0 || stderr != "" || gotOwner != "acme" || gotRepo != "widgets" {
		t.Fatalf("code=%d stdout=%q stderr=%q repo=%s/%s", code, stdout, stderr, gotOwner, gotRepo)
	}
	if !strings.Contains(stdout, "releases/download/gh-image-evidence/") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestRunInitAndCheckAccess(t *testing.T) {
	t.Run("init creates managed release", func(t *testing.T) {
		code, stdout, stderr := runWith(t, []string{"init", "--repo", "acme/widgets"}, okDeps())
		if code != 0 || stderr != "" || !strings.Contains(stdout, "Created acme/widgets evidence release") {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
	})
	t.Run("check-access is read only", func(t *testing.T) {
		d := okDeps()
		d.initRelease = func(*repo.Info) (release.InitResult, error) {
			t.Fatal("check-access created remote state")
			return release.InitResult{}, nil
		}
		code, stdout, stderr := runWith(t, []string{"check-access", "--repo=acme/widgets"}, d)
		if code != 0 || stderr != "" || !strings.Contains(stdout, "access ready for acme/widgets") {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
	})
}

func TestRunArgumentErrors(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{nil, "Usage:"},
		{[]string{"--repo"}, "requires a value"},
		{[]string{"--repo", "invalid", "/evidence/shot.png"}, "owner/repo format"},
		{[]string{"--evidence-root", "/a", "--evidence-root", "/b", "/a/x.png"}, "specified more than once"},
		{[]string{"one.png", "two.png"}, "exactly one PNG path"},
		{[]string{"init", "extra"}, "does not take positional arguments"},
		{[]string{"check-access", "--evidence-root", "/tmp"}, "cannot be combined"},
	}
	for _, tc := range tests {
		code, _, stderr := runWith(t, tc.args, okDeps())
		if code != 1 || !strings.Contains(stderr, tc.want) {
			t.Errorf("args=%q code=%d stderr=%q want=%q", tc.args, code, stderr, tc.want)
		}
	}
}

func TestRunVersion(t *testing.T) {
	code, stdout, stderr := runWith(t, []string{"--version"}, okDeps())
	if code != 0 || stderr != "" || strings.TrimSpace(stdout) != "gh-image dev" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestProductionDepsWiringComplete(t *testing.T) {
	d := productionDeps()
	if d.resolveRepo == nil || d.openEvidence == nil || d.initRelease == nil || d.checkAccess == nil || d.uploadEvidence == nil {
		t.Fatal("productionDeps left a boundary unwired")
	}
}
