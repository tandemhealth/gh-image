package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/tandemhealth/gh-image/internal/cookies"
	"github.com/tandemhealth/gh-image/internal/credential"
	"github.com/tandemhealth/gh-image/internal/repo"
	"github.com/tandemhealth/gh-image/internal/session"
	"github.com/tandemhealth/gh-image/internal/upload"
	"golang.org/x/term"
)

const usage = `Usage:
  gh image [--repo owner/repo] [--token <value>] [--evidence-root <absolute-dir>] <absolute-png-path>
  gh image auth-store
  gh image check-token [--token <value>]
  gh image --version`

// version is set via -ldflags "-X main.version=..." at release build time.
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, productionDeps()))
}

type uploadInput struct {
	path     string
	evidence *upload.Evidence
}

// uploadFunc uploads one validated evidence snapshot and returns its markdown reference.
type uploadFunc func(info *repo.Info, input uploadInput) (string, error)

// deps are the I/O boundaries run() depends on; productionDeps wires the real ones,
// tests inject stubs so the orchestration spine runs without network/subprocess/exit.
type deps struct {
	resolveRepo     func(owner, name string) (*repo.Info, error)
	resolveCookie   func(tokenFlag string) (*http.Cookie, error)
	evidenceRootEnv string
	openEvidence    func(root, path string) (*upload.Evidence, error)
	// newUploader builds an uploader from a session cookie. It is called once per
	// run so the underlying HTTP client (and its cookie jar) is shared across all
	// files, matching the single-client behavior of the original implementation.
	newUploader func(cookie *http.Cookie) uploadFunc
	readToken   func() (string, error)
	storeToken  func(string) error
	checkToken  func(tokenFlag string) (username, source string, err error)
}

func productionDeps() deps {
	return deps{
		resolveRepo: repo.Resolve,
		resolveCookie: func(tokenFlag string) (*http.Cookie, error) {
			cookie, _, err := resolveSessionCookie(tokenFlag)
			return cookie, err
		},
		evidenceRootEnv: os.Getenv("GH_IMAGE_EVIDENCE_ROOT"),
		openEvidence:    upload.OpenEvidence,
		newUploader: func(cookie *http.Cookie) uploadFunc {
			client := upload.NewClient(cookie)
			return func(info *repo.Info, input uploadInput) (string, error) {
				res, err := client.Upload(info.Owner, info.Name, info.ID, input.evidence)
				if err != nil {
					return "", err
				}
				return res.Markdown, nil
			}
		},
		readToken: func() (string, error) {
			return readSessionToken(os.Stdin, os.Stderr)
		},
		storeToken: credential.Set,
		checkToken: func(tokenFlag string) (string, string, error) {
			return checkToken(tokenFlag, resolveSessionCookie, session.CheckValidity)
		},
	}
}

func run(args []string, stdout, stderr io.Writer, d deps) int {
	var repoFlag string
	var repoSet bool
	var tokenFlag string
	var tokenSet bool
	var evidenceRootFlag string
	var evidenceRootSet bool
	var paths []string
	var firstPosAfterDoubleDash bool

	// Manual arg parsing so flags can appear anywhere (before or after positional args).
	flagsDone := false
	for i := 0; i < len(args); i++ {
		arg := args[i]

		// After "--", everything is a positional arg
		if flagsDone {
			if len(paths) == 0 {
				firstPosAfterDoubleDash = true
			}
			paths = append(paths, arg)
			continue
		}

		switch {
		case arg == "--":
			flagsDone = true
		case arg == "--repo":
			if repoSet {
				fmt.Fprintf(stderr, "Error: --repo specified more than once\n")
				return 1
			}
			if i+1 >= len(args) {
				fmt.Fprintf(stderr, "Error: --repo requires a value (owner/repo)\n%s\n", usage)
				return 1
			}
			i++
			repoFlag = args[i]
			repoSet = true
		case strings.HasPrefix(arg, "--repo="):
			if repoSet {
				fmt.Fprintf(stderr, "Error: --repo specified more than once\n")
				return 1
			}
			repoFlag = strings.SplitN(arg, "=", 2)[1]
			repoSet = true
		case arg == "--token":
			if tokenSet {
				fmt.Fprintf(stderr, "Error: --token specified more than once\n")
				return 1
			}
			if i+1 >= len(args) {
				fmt.Fprintf(stderr, "Error: --token requires a value\n%s\n", usage)
				return 1
			}
			i++
			tokenFlag = strings.TrimSpace(args[i])
			if tokenFlag == "" {
				fmt.Fprintf(stderr, "Error: --token value cannot be empty\n%s\n", usage)
				return 1
			}
			tokenSet = true
		case strings.HasPrefix(arg, "--token="):
			if tokenSet {
				fmt.Fprintf(stderr, "Error: --token specified more than once\n")
				return 1
			}
			tokenFlag = strings.TrimSpace(strings.SplitN(arg, "=", 2)[1])
			if tokenFlag == "" {
				fmt.Fprintf(stderr, "Error: --token value cannot be empty\n%s\n", usage)
				return 1
			}
			tokenSet = true
		case arg == "--evidence-root":
			if evidenceRootSet {
				fmt.Fprintf(stderr, "Error: --evidence-root specified more than once\n")
				return 1
			}
			if i+1 >= len(args) {
				fmt.Fprintf(stderr, "Error: --evidence-root requires an absolute directory\n%s\n", usage)
				return 1
			}
			i++
			evidenceRootFlag = strings.TrimSpace(args[i])
			if evidenceRootFlag == "" {
				fmt.Fprintf(stderr, "Error: --evidence-root value cannot be empty\n%s\n", usage)
				return 1
			}
			evidenceRootSet = true
		case strings.HasPrefix(arg, "--evidence-root="):
			if evidenceRootSet {
				fmt.Fprintf(stderr, "Error: --evidence-root specified more than once\n")
				return 1
			}
			evidenceRootFlag = strings.TrimSpace(strings.SplitN(arg, "=", 2)[1])
			if evidenceRootFlag == "" {
				fmt.Fprintf(stderr, "Error: --evidence-root value cannot be empty\n%s\n", usage)
				return 1
			}
			evidenceRootSet = true
		case arg == "--version":
			fmt.Fprintf(stdout, "gh-image %s\n", version)
			return 0
		case arg == "--help" || arg == "-h":
			fmt.Fprintf(stdout, "%s\n\n", usage)
			fmt.Fprintln(stdout, "Upload one validated PNG screenshot to GitHub and print its markdown reference.")
			fmt.Fprintln(stdout)
			fmt.Fprintln(stdout, "The --repo flag is optional. If omitted, the repository is")
			fmt.Fprintln(stdout, "inferred from the git remote in the current directory.")
			fmt.Fprintln(stdout)
			fmt.Fprintln(stdout, "Flags:")
			fmt.Fprintln(stdout, "  --repo owner/repo   GitHub repository (optional)")
			fmt.Fprintln(stdout, "  --token <value>     GitHub session token (default: dedicated OS credential)")
			fmt.Fprintln(stdout, "                      Can also be set via GH_SESSION_TOKEN environment variable")
			fmt.Fprintln(stdout, "                      WARNING: --token values are visible in process listings.")
			fmt.Fprintln(stdout, "                      Prefer GH_SESSION_TOKEN on shared machines.")
			fmt.Fprintln(stdout, "  --evidence-root     Absolute directory that contains the screenshot")
			fmt.Fprintln(stdout, "                      Defaults to GH_IMAGE_EVIDENCE_ROOT")
			fmt.Fprintln(stdout, "  --version           Print version and exit")
			fmt.Fprintln(stdout)
			fmt.Fprintln(stdout, "Subcommands:")
			fmt.Fprintln(stdout, "  auth-store          Prompt for and store gh-image's dedicated OS credential")
			fmt.Fprintln(stdout, "  check-token         Verify a session token is valid and print username to stdout")
			fmt.Fprintln(stdout)
			fmt.Fprintln(stdout, "The PNG path and evidence root must both be absolute.")
			return 0
		case strings.HasPrefix(arg, "-") && arg != "-":
			fmt.Fprintf(stderr, "Error: unknown flag %s\n", arg)
			if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") {
				fmt.Fprintf(stderr, "If this is a filename, use: gh image -- %s\n", arg)
			}
			fmt.Fprintf(stderr, "Run 'gh image --help' for usage.\n")
			return 1
		default:
			paths = append(paths, arg)
		}
	}

	// Dispatch subcommands before any other validation.
	subcommand, dispatchErr := classifySubcommand(paths, firstPosAfterDoubleDash, tokenFlag, repoSet, evidenceRootSet)
	if dispatchErr != nil {
		fmt.Fprintf(stderr, "Error: %v\n", dispatchErr)
		var ue *usageError
		if errors.As(dispatchErr, &ue) {
			fmt.Fprintf(stderr, "%s\nRun 'gh image --help' for usage.\n", usage)
		}
		return 1
	}
	switch subcommand {
	case "auth-store":
		value, err := d.readToken()
		if err != nil {
			fmt.Fprintf(stderr, "Error: reading session token: %v\n", err)
			return 1
		}
		if err := d.storeToken(value); err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		fmt.Fprintln(stderr, "Stored GitHub session in the dedicated gh-image OS credential")
		return 0
	case "check-token":
		username, source, err := d.checkToken(tokenFlag)
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		fmt.Fprintf(stderr, "Token is valid (source: %s)\n", source)
		if username != "" {
			fmt.Fprintln(stdout, username)
		}
		return 0
	}

	if len(paths) == 0 {
		fmt.Fprintf(stderr, "%s\nRun 'gh image --help' for usage.\n", usage)
		return 1
	}
	if len(paths) != 1 {
		fmt.Fprintf(stderr, "Error: exactly one PNG path is required, got %d\n%s\n", len(paths), usage)
		return 1
	}
	if paths[0] == "" {
		fmt.Fprintf(stderr, "Error: empty PNG path\n")
		return 1
	}

	evidenceRoot := evidenceRootFlag
	if !evidenceRootSet {
		evidenceRoot = strings.TrimSpace(d.evidenceRootEnv)
	}
	if evidenceRoot == "" {
		fmt.Fprintln(stderr, "Error: evidence root is required; set --evidence-root or GH_IMAGE_EVIDENCE_ROOT")
		return 1
	}
	if d.openEvidence == nil {
		fmt.Fprintln(stderr, "Error: evidence validator is unavailable")
		return 1
	}
	evidence, err := d.openEvidence(evidenceRoot, paths[0])
	if err != nil {
		fmt.Fprintf(stderr, "Error validating screenshot: %v\n", err)
		return 1
	}

	// Resolve repository
	var owner, name string
	if repoSet {
		if repoFlag == "" {
			fmt.Fprintf(stderr, "Error: --repo value cannot be empty\n")
			return 1
		}
		parts := strings.SplitN(repoFlag, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			fmt.Fprintf(stderr, "Error: --repo must be in owner/repo format, got: %s\n", repoFlag)
			return 1
		}
		owner, name = parts[0], parts[1]
	}

	repoInfo, err := d.resolveRepo(owner, name)
	if err != nil {
		fmt.Fprintf(stderr, "Error resolving repository: %v\n", err)
		return 1
	}

	// Get session cookie (flag > env var > dedicated OS credential)
	cookie, err := d.resolveCookie(tokenFlag)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	// Build the uploader after evidence validation and session resolution.
	uploadFile := d.newUploader(cookie)

	input := uploadInput{path: paths[0], evidence: evidence}
	markdown, err := uploadFile(repoInfo, input)
	if err != nil {
		fmt.Fprintf(stderr, "Error uploading %s: %v\n", paths[0], err)
		return 1
	}
	fmt.Fprintln(stdout, markdown)
	return 0
}

// usageError wraps an error to signal that usage text should be shown alongside the message.
type usageError struct{ err error }

func (e *usageError) Error() string { return e.err.Error() }

// classifySubcommand identifies whether the parsed positional args represent a
// supported subcommand invocation and validates subcommand-specific constraints.
func classifySubcommand(paths []string, firstPosAfterDoubleDash bool, tokenFlag string, repoSet, evidenceRootSet bool) (string, error) {
	if len(paths) == 0 || firstPosAfterDoubleDash {
		return "", nil
	}
	switch paths[0] {
	case "auth-store":
		if len(paths) > 1 {
			return "", &usageError{fmt.Errorf("auth-store does not take positional arguments")}
		}
		if tokenFlag != "" {
			return "", fmt.Errorf("--token cannot be combined with auth-store (auth-store reads from the terminal without echo)")
		}
		if repoSet {
			return "", fmt.Errorf("--repo cannot be combined with auth-store")
		}
		if evidenceRootSet {
			return "", fmt.Errorf("--evidence-root cannot be combined with auth-store")
		}
		return "auth-store", nil
	case "check-token":
		if len(paths) > 1 {
			return "", &usageError{fmt.Errorf("check-token does not take positional arguments")}
		}
		if repoSet {
			return "", fmt.Errorf("--repo cannot be combined with check-token")
		}
		if evidenceRootSet {
			return "", fmt.Errorf("--evidence-root cannot be combined with check-token")
		}
		return "check-token", nil
	default:
		return "", nil
	}
}

// resolveSessionCookie returns a GitHub session cookie using the first available
// source: --token flag, GH_SESSION_TOKEN environment variable, or the dedicated
// gh-image OS credential. It never reads a browser cookie database or browser
// encryption key.
func resolveSessionCookie(tokenFlag string) (*http.Cookie, string, error) {
	return resolveSessionCookieWithGetter(tokenFlag, os.Getenv("GH_SESSION_TOKEN"), credential.Get)
}

// resolveSessionCookieWithGetter is a testable variant of resolveSessionCookie
// that accepts explicit env value and dedicated credential getter dependencies.
// Returns the cookie, a human-readable source label, and any error.
func resolveSessionCookieWithGetter(tokenFlag, envToken string, getCredential func() (string, error)) (*http.Cookie, string, error) {
	if tokenFlag != "" {
		cookie, err := cookieFromValue(tokenFlag)
		if err != nil {
			return nil, "", fmt.Errorf("--token flag: %w", err)
		}
		return cookie, "--token flag", nil
	}
	if envToken != "" {
		cookie, err := cookieFromValue(envToken)
		if err != nil {
			return nil, "", fmt.Errorf("GH_SESSION_TOKEN: %w", err)
		}
		return cookie, "GH_SESSION_TOKEN", nil
	}
	if getCredential == nil {
		return nil, "", fmt.Errorf("no session token found: gh-image credential getter is unavailable")
	}
	value, err := getCredential()
	if err != nil {
		return nil, "", fmt.Errorf("resolving session cookie: %w", err)
	}
	cookie, err := cookieFromValue(value)
	if err != nil {
		return nil, "", fmt.Errorf("gh-image OS credential: %w", err)
	}
	return cookie, "gh-image OS credential", nil
}

// readSessionToken reads a secret only from an interactive terminal and disables
// echo while the user types it. This keeps the value out of shell history,
// process listings, stdout, and agent command output.
func readSessionToken(in *os.File, prompt io.Writer) (string, error) {
	fd := int(in.Fd())
	if !term.IsTerminal(fd) {
		return "", fmt.Errorf("auth-store requires an interactive terminal")
	}
	fmt.Fprint(prompt, "GitHub user_session: ")
	value, err := term.ReadPassword(fd)
	fmt.Fprintln(prompt)
	if err != nil {
		return "", err
	}
	valueString := strings.TrimSpace(string(value))
	if _, err := cookieFromValue(valueString); err != nil {
		return "", err
	}
	return valueString, nil
}

// cookieFromValue constructs a GitHub user_session cookie from a raw token value.
func cookieFromValue(value string) (*http.Cookie, error) {
	value = strings.TrimSpace(value) // defensive: env vars arrive untrimmed; flag path trims earlier
	if value == "" {
		return nil, fmt.Errorf("session token is empty")
	}
	return cookies.NewSessionCookie(value), nil
}

// checkToken resolves and validates a session token, returning the authenticated username and source.
func checkToken(tokenFlag string, resolver func(string) (*http.Cookie, string, error), validator func(*http.Cookie) (string, error)) (string, string, error) {
	cookie, source, err := resolver(tokenFlag)
	if err != nil {
		return "", "", err
	}
	username, err := validator(cookie)
	if err != nil {
		return "", "", err
	}
	return username, source, nil
}
