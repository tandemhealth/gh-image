package main

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/tandemhealth/gh-image/internal/repo"
	"github.com/tandemhealth/gh-image/internal/upload"
)

// okDeps returns deps whose boundaries all succeed; tests override individual
// fields to exercise specific paths.
func okDeps() deps {
	return deps{
		resolveRepo: func(owner, name string) (*repo.Info, error) {
			return &repo.Info{Owner: "octo", Name: "hello", ID: 1}, nil
		},
		resolveCookie: func(tokenFlag string) (*http.Cookie, error) {
			return &http.Cookie{Name: "user_session", Value: "tok"}, nil
		},
		evidenceRootEnv: "/evidence",
		openEvidence: func(root, path string) (*upload.Evidence, error) {
			return nil, nil
		},
		newUploader: func(cookie *http.Cookie) uploadFunc {
			// The stub returns image-embed markdown for any path; run()'s job is the
			// orchestration spine, not the embed-vs-link decision (that lives in
			// upload.renderMarkdown and is covered by TestRenderMarkdown).
			return func(info *repo.Info, input uploadInput) (string, error) {
				return "![" + input.path + "](url)", nil
			}
		},
		readToken:  func() (string, error) { return "session-to-store", nil },
		storeToken: func(string) error { return nil },
		checkToken: func(tokenFlag string) (string, string, error) { return "octouser", "stub", nil },
	}
}

// runWith executes run() with buffered streams and returns the exit code + output.
func runWith(t *testing.T, args []string, d deps) (code int, stdout, stderr string) {
	t.Helper()
	var so, se bytes.Buffer
	code = run(args, &so, &se, d)
	return code, so.String(), se.String()
}

// TestCookieFromValue_BasicAttributes verifies the cookie has the expected fields.
func TestCookieFromValue_BasicAttributes(t *testing.T) {
	cookie, err := cookieFromValue("mytoken123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cookie.Name != "user_session" {
		t.Errorf("expected Name 'user_session', got %q", cookie.Name)
	}
	if cookie.Value != "mytoken123" {
		t.Errorf("expected Value 'mytoken123', got %q", cookie.Value)
	}
	if cookie.Domain != "github.com" {
		t.Errorf("expected Domain 'github.com', got %q", cookie.Domain)
	}
	if cookie.Path != "/" {
		t.Errorf("expected Path '/', got %q", cookie.Path)
	}
	if !cookie.Secure {
		t.Error("expected Secure to be true")
	}
	if !cookie.HttpOnly {
		t.Error("expected HttpOnly to be true")
	}
}

// TestCookieFromValue_TrimsWhitespace verifies leading/trailing whitespace is stripped.
func TestCookieFromValue_TrimsWhitespace(t *testing.T) {
	cookie, err := cookieFromValue("  token_with_spaces  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cookie.Value != "token_with_spaces" {
		t.Errorf("expected whitespace trimmed, got %q", cookie.Value)
	}
}

// TestCookieFromValue_RejectsEmpty verifies that empty/whitespace-only values error.
func TestCookieFromValue_RejectsEmpty(t *testing.T) {
	tests := []string{"", "   ", "\t\n"}
	for _, v := range tests {
		_, err := cookieFromValue(v)
		if err == nil {
			t.Errorf("expected error for empty token %q, got nil", v)
		}
		if !strings.Contains(err.Error(), "empty") {
			t.Errorf("unexpected error message for %q: %v", v, err)
		}
	}
}

// TestResolveSessionCookie_FlagPriority verifies --token flag takes highest priority.
func TestResolveSessionCookie_FlagPriority(t *testing.T) {
	cookie, source, err := resolveSessionCookieWithGetter("flag_token", "env_token", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cookie.Value != "flag_token" {
		t.Errorf("expected flag_token to win, got %q", cookie.Value)
	}
	if source != "--token flag" {
		t.Errorf("expected source %q, got %q", "--token flag", source)
	}
}

// TestResolveSessionCookie_EnvFallback verifies GH_SESSION_TOKEN is used when no flag.
func TestResolveSessionCookie_EnvFallback(t *testing.T) {
	cookie, source, err := resolveSessionCookieWithGetter("", "env_token_value", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cookie.Value != "env_token_value" {
		t.Errorf("expected env_token_value, got %q", cookie.Value)
	}
	if source != "GH_SESSION_TOKEN" {
		t.Errorf("expected source %q, got %q", "GH_SESSION_TOKEN", source)
	}
}

// TestResolveSessionCookie_CredentialError verifies credential errors are wrapped.
func TestResolveSessionCookie_CredentialError(t *testing.T) {
	_, _, err := resolveSessionCookieWithGetter("", "", func() (string, error) {
		return "", fmt.Errorf("credential unavailable")
	})
	if err == nil {
		t.Fatal("expected error when credential getter fails, got nil")
	}
	if !strings.Contains(err.Error(), "resolving session cookie") {
		t.Errorf("expected 'resolving session cookie' in error, got: %v", err)
	}
}

func TestResolveSessionCookie_CredentialFallbackSuccess(t *testing.T) {
	cookie, source, err := resolveSessionCookieWithGetter("", "", func() (string, error) {
		return "stored_token", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cookie == nil {
		t.Fatal("expected non-nil cookie")
	}
	if cookie.Value != "stored_token" {
		t.Fatalf("expected stored token, got %q", cookie.Value)
	}
	if source != "gh-image OS credential" {
		t.Errorf("expected dedicated credential source, got %q", source)
	}
}

// TestCookieFromValue_UsableByNewClient verifies the cookie produced by
// cookieFromValue can be passed to upload.NewClient.
func TestCookieFromValue_UsableByNewClient(t *testing.T) {
	cookie, err := cookieFromValue("testtoken")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	client := upload.NewClient(cookie)
	if client == nil {
		t.Fatal("expected upload.NewClient to return a non-nil client")
	}
}

func TestCheckToken_Success(t *testing.T) {
	username, source, err := checkToken("sometoken",
		func(token string) (*http.Cookie, string, error) {
			cookie, err := cookieFromValue(token)
			return cookie, "--token flag", err
		},
		func(c *http.Cookie) (string, error) {
			return "testuser", nil
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if username != "testuser" {
		t.Errorf("expected 'testuser', got %q", username)
	}
	if source != "--token flag" {
		t.Errorf("expected source %q, got %q", "--token flag", source)
	}
}

func TestCheckToken_ResolverError(t *testing.T) {
	_, _, err := checkToken("",
		func(token string) (*http.Cookie, string, error) {
			return nil, "", fmt.Errorf("no token")
		},
		func(c *http.Cookie) (string, error) {
			return "unused", nil
		},
	)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCheckToken_ValidatorError(t *testing.T) {
	_, _, err := checkToken("sometoken",
		func(token string) (*http.Cookie, string, error) {
			cookie, err := cookieFromValue(token)
			return cookie, "--token flag", err
		},
		func(c *http.Cookie) (string, error) {
			return "", fmt.Errorf("token expired")
		},
	)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestResolveSessionCookie_WhitespaceEnvVar(t *testing.T) {
	credentialCalled := false
	_, _, err := resolveSessionCookieWithGetter("", "   ", func() (string, error) {
		credentialCalled = true
		return "stored_token", nil
	})
	if err == nil {
		t.Fatal("expected error for whitespace-only env token, got nil")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected error containing 'empty', got: %v", err)
	}
	if !strings.Contains(err.Error(), "GH_SESSION_TOKEN") {
		t.Errorf("expected error to identify source 'GH_SESSION_TOKEN', got: %v", err)
	}
	if credentialCalled {
		t.Error("whitespace-only env token should not fall through to credential getter")
	}
}

func TestResolveSessionCookie_WhitespaceFlag(t *testing.T) {
	_, _, err := resolveSessionCookieWithGetter("   ", "env_token", nil)
	if err == nil {
		t.Fatal("expected error for whitespace-only flag token, got nil")
	}
	if !strings.Contains(err.Error(), "--token flag") {
		t.Errorf("expected error to identify source '--token flag', got: %v", err)
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected error containing 'empty', got: %v", err)
	}
}

func TestRun_VersionAndHelp(t *testing.T) {
	t.Run("version prints to stdout and exits 0", func(t *testing.T) {
		code, out, _ := runWith(t, []string{"--version"}, okDeps())
		if code != 0 {
			t.Fatalf("code = %d, want 0", code)
		}
		if !strings.Contains(out, "gh-image dev") {
			t.Errorf("stdout = %q, want version string", out)
		}
	})
	for _, flag := range []string{"--help", "-h"} {
		t.Run(flag+" prints usage to stdout and exits 0", func(t *testing.T) {
			code, out, _ := runWith(t, []string{flag}, okDeps())
			if code != 0 {
				t.Fatalf("code = %d, want 0", code)
			}
			if !strings.Contains(out, "Usage:") || !strings.Contains(out, "auth-store") {
				t.Errorf("stdout missing usage content: %q", out)
			}
		})
	}
}

func TestRun_FlagErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string // substring expected on stderr
	}{
		{"unknown long flag", []string{"--bogus"}, "unknown flag --bogus"},
		{"unknown short flag hints --", []string{"-x"}, "use: gh image -- -x"},
		{"repo twice", []string{"--repo", "a/b", "--repo", "c/d"}, "specified more than once"},
		{"repo missing value", []string{"--repo"}, "requires a value"},
		{"repo empty via =", []string{"--repo=", "img.png"}, "--repo value cannot be empty"},
		{"repo bad format", []string{"--repo", "noslash", "img.png"}, "must be in owner/repo format"},
		{"token twice", []string{"--token", "a", "--token", "b"}, "specified more than once"},
		{"token missing value", []string{"--token"}, "requires a value"},
		{"token empty value", []string{"--token", "   "}, "cannot be empty"},
		{"no args shows usage", nil, "Usage:"},
		{"empty file path", []string{""}, "empty PNG path"},
		{"evidence root twice", []string{"--evidence-root", "/a", "--evidence-root", "/b", "a.png"}, "specified more than once"},
		{"evidence root missing value", []string{"--evidence-root"}, "requires an absolute directory"},
		{"evidence root empty", []string{"--evidence-root="}, "value cannot be empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, _, errOut := runWith(t, tc.args, okDeps())
			if code != 1 {
				t.Fatalf("code = %d, want 1", code)
			}
			if !strings.Contains(errOut, tc.want) {
				t.Errorf("stderr = %q, want substring %q", errOut, tc.want)
			}
		})
	}
}

func TestRun_Subcommands(t *testing.T) {
	t.Run("auth-store success", func(t *testing.T) {
		d := okDeps()
		var stored string
		d.storeToken = func(value string) error { stored = value; return nil }
		code, out, errOut := runWith(t, []string{"auth-store"}, d)
		if code != 0 || out != "" || stored != "session-to-store" {
			t.Fatalf("code=%d out=%q stored=%q", code, out, stored)
		}
		if !strings.Contains(errOut, "dedicated gh-image OS credential") {
			t.Errorf("stderr missing status: %q", errOut)
		}
	})
	t.Run("auth-store requires interactive token reader", func(t *testing.T) {
		d := okDeps()
		d.readToken = func() (string, error) { return "", fmt.Errorf("auth-store requires an interactive terminal") }
		code, _, errOut := runWith(t, []string{"auth-store"}, d)
		if code != 1 || !strings.Contains(errOut, "interactive terminal") {
			t.Fatalf("code=%d stderr=%q", code, errOut)
		}
	})
	t.Run("auth-store error", func(t *testing.T) {
		d := okDeps()
		d.storeToken = func(string) error { return fmt.Errorf("keyring locked") }
		code, _, errOut := runWith(t, []string{"auth-store"}, d)
		if code != 1 || !strings.Contains(errOut, "keyring locked") {
			t.Fatalf("code=%d stderr=%q", code, errOut)
		}
	})
	t.Run("check-token success prints username", func(t *testing.T) {
		code, out, errOut := runWith(t, []string{"check-token"}, okDeps())
		if code != 0 || strings.TrimSpace(out) != "octouser" {
			t.Fatalf("code=%d out=%q", code, out)
		}
		if !strings.Contains(errOut, "Token is valid (source: stub)") {
			t.Errorf("stderr = %q", errOut)
		}
	})
	t.Run("check-token empty username prints nothing to stdout", func(t *testing.T) {
		d := okDeps()
		d.checkToken = func(string) (string, string, error) { return "", "stub", nil }
		code, out, _ := runWith(t, []string{"check-token"}, d)
		if code != 0 || strings.TrimSpace(out) != "" {
			t.Fatalf("code=%d out=%q", code, out)
		}
	})
	t.Run("check-token error", func(t *testing.T) {
		d := okDeps()
		d.checkToken = func(string) (string, string, error) { return "", "", fmt.Errorf("expired") }
		code, _, errOut := runWith(t, []string{"check-token"}, d)
		if code != 1 || !strings.Contains(errOut, "expired") {
			t.Fatalf("code=%d stderr=%q", code, errOut)
		}
	})
	t.Run("auth-store with --token is a conflict error", func(t *testing.T) {
		code, _, errOut := runWith(t, []string{"auth-store", "--token", "x"}, okDeps())
		if code != 1 || !strings.Contains(errOut, "--token cannot be combined") {
			t.Fatalf("code=%d stderr=%q", code, errOut)
		}
	})
}

func TestRun_Upload(t *testing.T) {
	t.Run("single file prints markdown, exits 0", func(t *testing.T) {
		code, out, _ := runWith(t, []string{"a.png"}, okDeps())
		if code != 0 || strings.TrimSpace(out) != "![a.png](url)" {
			t.Fatalf("code=%d out=%q", code, out)
		}
	})
	t.Run("multiple files are rejected before dependencies", func(t *testing.T) {
		d := okDeps()
		d.openEvidence = func(string, string) (*upload.Evidence, error) {
			t.Fatal("openEvidence called for a batch upload")
			return nil, nil
		}
		code, _, errOut := runWith(t, []string{"a.png", "b.png"}, d)
		if code != 1 || !strings.Contains(errOut, "exactly one PNG path is required, got 2") {
			t.Fatalf("code=%d stderr=%q", code, errOut)
		}
	})
	t.Run("upload failure exits 1", func(t *testing.T) {
		d := okDeps()
		d.newUploader = func(c *http.Cookie) uploadFunc {
			return func(info *repo.Info, input uploadInput) (string, error) {
				return "", fmt.Errorf("upload failed")
			}
		}
		code, out, errOut := runWith(t, []string{"bad.png"}, d)
		if code != 1 {
			t.Fatalf("code = %d, want 1", code)
		}
		if out != "" {
			t.Errorf("stdout = %q, want empty", out)
		}
		if !strings.Contains(errOut, "Error uploading bad.png") {
			t.Errorf("stderr missing failure: %q", errOut)
		}
	})
	t.Run("missing evidence root fails before dependencies", func(t *testing.T) {
		d := okDeps()
		d.evidenceRootEnv = ""
		d.openEvidence = func(string, string) (*upload.Evidence, error) {
			t.Fatal("openEvidence called without a configured root")
			return nil, nil
		}
		code, _, errOut := runWith(t, []string{"a.png"}, d)
		if code != 1 || !strings.Contains(errOut, "evidence root is required") {
			t.Fatalf("code=%d stderr=%q", code, errOut)
		}
	})
	t.Run("flag evidence root overrides the environment", func(t *testing.T) {
		d := okDeps()
		d.evidenceRootEnv = "/from-env"
		var gotRoot string
		d.openEvidence = func(root, path string) (*upload.Evidence, error) {
			gotRoot = root
			return nil, nil
		}
		code, _, errOut := runWith(t, []string{"--evidence-root", "/from-flag", "a.png"}, d)
		if code != 0 || errOut != "" || gotRoot != "/from-flag" {
			t.Fatalf("code=%d stderr=%q root=%q", code, errOut, gotRoot)
		}
	})
	t.Run("invalid evidence fails before repo and cookie resolution", func(t *testing.T) {
		d := okDeps()
		d.openEvidence = func(string, string) (*upload.Evidence, error) {
			return nil, fmt.Errorf("outside evidence root")
		}
		d.resolveRepo = func(string, string) (*repo.Info, error) {
			t.Fatal("resolveRepo called after evidence validation failed")
			return nil, nil
		}
		d.resolveCookie = func(string) (*http.Cookie, error) {
			t.Fatal("resolveCookie called after evidence validation failed")
			return nil, nil
		}
		code, _, errOut := runWith(t, []string{"a.png"}, d)
		if code != 1 || !strings.Contains(errOut, "outside evidence root") {
			t.Fatalf("code=%d stderr=%q", code, errOut)
		}
	})
	t.Run("resolveRepo error exits 1", func(t *testing.T) {
		d := okDeps()
		d.resolveRepo = func(string, string) (*repo.Info, error) { return nil, fmt.Errorf("no remote") }
		code, _, errOut := runWith(t, []string{"a.png"}, d)
		if code != 1 || !strings.Contains(errOut, "Error resolving repository") {
			t.Fatalf("code=%d stderr=%q", code, errOut)
		}
	})
	t.Run("resolveCookie error exits 1", func(t *testing.T) {
		d := okDeps()
		d.resolveCookie = func(string) (*http.Cookie, error) { return nil, fmt.Errorf("no token") }
		code, _, errOut := runWith(t, []string{"a.png"}, d)
		if code != 1 || !strings.Contains(errOut, "Error:") {
			t.Fatalf("code=%d stderr=%q", code, errOut)
		}
	})
	t.Run("explicit --repo is parsed and passed to resolveRepo", func(t *testing.T) {
		var gotOwner, gotName string
		d := okDeps()
		d.resolveRepo = func(owner, name string) (*repo.Info, error) {
			gotOwner, gotName = owner, name
			return &repo.Info{Owner: owner, Name: name, ID: 9}, nil
		}
		runWith(t, []string{"--repo", "acme/widgets", "a.png"}, d)
		if gotOwner != "acme" || gotName != "widgets" {
			t.Errorf("resolveRepo got %q/%q, want acme/widgets", gotOwner, gotName)
		}
	})
	t.Run("-- terminator treats the following absolute PNG as a path and infers repo", func(t *testing.T) {
		var gotOwner, gotName string
		d := okDeps()
		d.resolveRepo = func(owner, name string) (*repo.Info, error) {
			gotOwner, gotName = owner, name
			return &repo.Info{Owner: "octo", Name: "hello", ID: 1}, nil
		}
		code, out, _ := runWith(t, []string{"--", "/evidence/shot.png"}, d)
		if code != 0 {
			t.Fatalf("code = %d, want 0", code)
		}
		if gotOwner != "" || gotName != "" {
			t.Errorf("expected inference path (empty owner/name), got %q/%q", gotOwner, gotName)
		}
		if !strings.Contains(out, "![/evidence/shot.png](url)") {
			t.Errorf("stdout = %q", out)
		}
	})
}

func TestRun_UsageErrorDispatchShowsUsage(t *testing.T) {
	// A usageError from classifySubcommand prints the usage block alongside the error.
	code, _, errOut := runWith(t, []string{"auth-store", "extra"}, okDeps())
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "does not take positional arguments") || !strings.Contains(errOut, "Usage:") {
		t.Errorf("stderr = %q, want error + usage", errOut)
	}
}

func TestResolveSessionCookie_EnvPath(t *testing.T) {
	// The environment value wins before the dedicated credential is read.
	t.Setenv("GH_SESSION_TOKEN", "env-token-value")
	cookie, source, err := resolveSessionCookie("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cookie.Value != "env-token-value" || source != "GH_SESSION_TOKEN" {
		t.Errorf("got value=%q source=%q", cookie.Value, source)
	}
}

func TestResolveSessionCookie_NilGetter(t *testing.T) {
	_, _, err := resolveSessionCookieWithGetter("", "", nil)
	if err == nil || !strings.Contains(err.Error(), "credential getter is unavailable") {
		t.Fatalf("expected nil-getter error, got %v", err)
	}
}

func TestProductionDeps_WiringComplete(t *testing.T) {
	d := productionDeps()
	if d.resolveRepo == nil || d.resolveCookie == nil || d.openEvidence == nil || d.newUploader == nil || d.readToken == nil || d.storeToken == nil || d.checkToken == nil {
		t.Fatal("productionDeps left a boundary unwired")
	}
}

func TestClassifySubcommand(t *testing.T) {
	tests := []struct {
		name                    string
		paths                   []string
		firstPosAfterDoubleDash bool
		tokenFlag               string
		repoSet                 bool
		wantSubcommand          string
		wantErrContains         string
		wantUsageError          bool
	}{
		{
			name:           "auth-store selected",
			paths:          []string{"auth-store"},
			wantSubcommand: "auth-store",
		},
		{
			name:           "check-token selected",
			paths:          []string{"check-token"},
			wantSubcommand: "check-token",
		},
		{
			name:                    "double-dash treats check-token as filename",
			paths:                   []string{"check-token"},
			firstPosAfterDoubleDash: true,
			wantSubcommand:          "",
		},
		{
			name:                    "double-dash treats auth-store as filename",
			paths:                   []string{"auth-store"},
			firstPosAfterDoubleDash: true,
			wantSubcommand:          "",
		},
		{
			name:            "auth-store with extra args errors",
			paths:           []string{"auth-store", "extra"},
			wantErrContains: "does not take positional arguments",
			wantUsageError:  true,
		},
		{
			name:            "check-token with extra args errors",
			paths:           []string{"check-token", "extra"},
			wantErrContains: "does not take positional arguments",
			wantUsageError:  true,
		},
		{
			name:            "auth-store with token flag errors",
			paths:           []string{"auth-store"},
			tokenFlag:       "abc123",
			wantErrContains: "--token cannot be combined",
		},
		{
			name:           "non-subcommand remains upload mode",
			paths:          []string{"image.png"},
			wantSubcommand: "",
		},
		{
			name:            "auth-store with repo flag errors",
			paths:           []string{"auth-store"},
			repoSet:         true,
			wantErrContains: "--repo cannot be combined with auth-store",
		},
		{
			name:            "check-token with repo flag errors",
			paths:           []string{"check-token"},
			repoSet:         true,
			wantErrContains: "--repo cannot be combined with check-token",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotSubcommand, err := classifySubcommand(tc.paths, tc.firstPosAfterDoubleDash, tc.tokenFlag, tc.repoSet, false)
			if tc.wantErrContains != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErrContains)
				}
				if !strings.Contains(err.Error(), tc.wantErrContains) {
					t.Fatalf("expected error containing %q, got %q", tc.wantErrContains, err.Error())
				}
				var ue *usageError
				if tc.wantUsageError && !errors.As(err, &ue) {
					t.Error("expected usageError, but errors.As did not match")
				}
				if !tc.wantUsageError && errors.As(err, &ue) {
					t.Error("unexpected usageError")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotSubcommand != tc.wantSubcommand {
				t.Fatalf("expected subcommand %q, got %q", tc.wantSubcommand, gotSubcommand)
			}
		})
	}
}
