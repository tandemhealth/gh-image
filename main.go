package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/tandemhealth/gh-image/internal/release"
	"github.com/tandemhealth/gh-image/internal/repo"
	"github.com/tandemhealth/gh-image/internal/upload"
)

const usage = `Usage:
  gh image [--repo owner/repo] [--evidence-root <absolute-dir>] <absolute-png-path>
  gh image init [--repo owner/repo]
  gh image check-access [--repo owner/repo]
  gh image --version`

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, productionDeps()))
}

type deps struct {
	resolveRepo     func(owner, name string) (*repo.Info, error)
	evidenceRootEnv string
	openEvidence    func(root, path string) (*upload.Evidence, error)
	initRelease     func(*repo.Info) (release.InitResult, error)
	checkAccess     func(*repo.Info) (release.Release, error)
	uploadEvidence  func(*repo.Info, *upload.Evidence) (string, error)
}

func productionDeps() deps {
	client := release.NewClient(nil)
	return deps{
		resolveRepo:     repo.Resolve,
		evidenceRootEnv: os.Getenv("GH_IMAGE_EVIDENCE_ROOT"),
		openEvidence:    upload.OpenEvidence,
		initRelease: func(info *repo.Info) (release.InitResult, error) {
			return client.Init(info.Owner, info.Name)
		},
		checkAccess: func(info *repo.Info) (release.Release, error) {
			return client.CheckAccess(info.Owner, info.Name)
		},
		uploadEvidence: func(info *repo.Info, evidence *upload.Evidence) (string, error) {
			hash := sha256.New()
			if _, err := io.Copy(hash, evidence.Reader()); err != nil {
				return "", fmt.Errorf("hashing validated PNG: %w", err)
			}
			digest := fmt.Sprintf("%x", hash.Sum(nil))
			result, err := client.Upload(info.Owner, info.Name, digest, evidence.Size(), evidence.Reader())
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("![%s](%s)", evidence.Name(), result.URL), nil
		},
	}
}

type parsedArgs struct {
	repoFlag        string
	repoSet         bool
	evidenceRoot    string
	evidenceSet     bool
	positionals     []string
	afterDoubleDash bool
}

func parseArgs(args []string, stdout, stderr io.Writer) (parsedArgs, int, bool) {
	var parsed parsedArgs
	flagsDone := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if flagsDone {
			parsed.positionals = append(parsed.positionals, arg)
			continue
		}
		switch {
		case arg == "--":
			flagsDone = true
			parsed.afterDoubleDash = len(parsed.positionals) == 0
		case arg == "--repo":
			if parsed.repoSet {
				fmt.Fprintln(stderr, "Error: --repo specified more than once")
				return parsed, 1, false
			}
			if i+1 >= len(args) {
				fmt.Fprintf(stderr, "Error: --repo requires a value (owner/repo)\n%s\n", usage)
				return parsed, 1, false
			}
			i++
			parsed.repoFlag, parsed.repoSet = strings.TrimSpace(args[i]), true
		case strings.HasPrefix(arg, "--repo="):
			if parsed.repoSet {
				fmt.Fprintln(stderr, "Error: --repo specified more than once")
				return parsed, 1, false
			}
			parsed.repoFlag, parsed.repoSet = strings.TrimSpace(strings.SplitN(arg, "=", 2)[1]), true
		case arg == "--evidence-root":
			if parsed.evidenceSet {
				fmt.Fprintln(stderr, "Error: --evidence-root specified more than once")
				return parsed, 1, false
			}
			if i+1 >= len(args) {
				fmt.Fprintf(stderr, "Error: --evidence-root requires an absolute directory\n%s\n", usage)
				return parsed, 1, false
			}
			i++
			parsed.evidenceRoot, parsed.evidenceSet = strings.TrimSpace(args[i]), true
		case strings.HasPrefix(arg, "--evidence-root="):
			if parsed.evidenceSet {
				fmt.Fprintln(stderr, "Error: --evidence-root specified more than once")
				return parsed, 1, false
			}
			parsed.evidenceRoot, parsed.evidenceSet = strings.TrimSpace(strings.SplitN(arg, "=", 2)[1]), true
		case arg == "--version":
			fmt.Fprintf(stdout, "gh-image %s\n", version)
			return parsed, 0, false
		case arg == "--help" || arg == "-h":
			fmt.Fprintf(stdout, "%s\n\n", usage)
			fmt.Fprintln(stdout, "Upload one validated PNG to a repository's managed GitHub prerelease and print Markdown.")
			fmt.Fprintln(stdout, "The extension uses the existing GitHub CLI login. It never asks for or stores a token.")
			fmt.Fprintln(stdout, "Run init once per target repository. Normal uploads and check-access never create remote state.")
			return parsed, 0, false
		case strings.HasPrefix(arg, "-") && arg != "-":
			fmt.Fprintf(stderr, "Error: unknown flag %s\n", arg)
			if !strings.HasPrefix(arg, "--") {
				fmt.Fprintf(stderr, "If this is a filename, use: gh image -- %s\n", arg)
			}
			fmt.Fprintln(stderr, "Run 'gh image --help' for usage.")
			return parsed, 1, false
		default:
			parsed.positionals = append(parsed.positionals, arg)
		}
	}
	return parsed, 0, true
}

func resolveTarget(parsed parsedArgs, d deps) (*repo.Info, error) {
	var owner, name string
	if parsed.repoSet {
		parts := strings.Split(parsed.repoFlag, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("--repo must be in owner/repo format, got: %s", parsed.repoFlag)
		}
		owner, name = parts[0], parts[1]
	}
	return d.resolveRepo(owner, name)
}

func run(args []string, stdout, stderr io.Writer, d deps) int {
	parsed, code, proceed := parseArgs(args, stdout, stderr)
	if !proceed {
		return code
	}
	if len(parsed.positionals) == 0 {
		fmt.Fprintf(stderr, "%s\nRun 'gh image --help' for usage.\n", usage)
		return 1
	}

	subcommand := ""
	if !parsed.afterDoubleDash && (parsed.positionals[0] == "init" || parsed.positionals[0] == "check-access") {
		subcommand = parsed.positionals[0]
	}
	if !parsed.afterDoubleDash && (parsed.positionals[0] == "auth-store" || parsed.positionals[0] == "check-token") {
		fmt.Fprintf(stderr, "Error: %s was removed; gh-image uses the existing gh login and does not accept credentials\n", parsed.positionals[0])
		return 1
	}
	if subcommand != "" {
		if len(parsed.positionals) != 1 {
			fmt.Fprintf(stderr, "Error: %s does not take positional arguments\n", subcommand)
			return 1
		}
		if parsed.evidenceSet {
			fmt.Fprintf(stderr, "Error: --evidence-root cannot be combined with %s\n", subcommand)
			return 1
		}
		info, err := resolveTarget(parsed, d)
		if err != nil {
			fmt.Fprintf(stderr, "Error resolving repository: %v\n", err)
			return 1
		}
		if subcommand == "init" {
			result, err := d.initRelease(info)
			if err != nil {
				fmt.Fprintf(stderr, "Error initializing image evidence: %v\n", err)
				return 1
			}
			verb := "Ready"
			if result.Created {
				verb = "Created"
			}
			fmt.Fprintf(stdout, "%s %s/%s evidence release: %s\n", verb, info.Owner, info.Name, result.Release.HTMLURL)
			return 0
		}
		if _, err := d.checkAccess(info); err != nil {
			fmt.Fprintf(stderr, "Error checking image evidence access: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "GitHub image evidence access ready for %s/%s\n", info.Owner, info.Name)
		return 0
	}

	if len(parsed.positionals) != 1 {
		fmt.Fprintf(stderr, "Error: exactly one PNG path is required, got %d\n%s\n", len(parsed.positionals), usage)
		return 1
	}
	path := parsed.positionals[0]
	if path == "" {
		fmt.Fprintln(stderr, "Error: empty PNG path")
		return 1
	}
	root := parsed.evidenceRoot
	if !parsed.evidenceSet {
		root = strings.TrimSpace(d.evidenceRootEnv)
	}
	if root == "" {
		fmt.Fprintln(stderr, "Error: evidence root is required; set --evidence-root or GH_IMAGE_EVIDENCE_ROOT")
		return 1
	}
	evidence, err := d.openEvidence(root, path)
	if err != nil {
		fmt.Fprintf(stderr, "Error validating screenshot: %v\n", err)
		return 1
	}
	info, err := resolveTarget(parsed, d)
	if err != nil {
		fmt.Fprintf(stderr, "Error resolving repository: %v\n", err)
		return 1
	}
	markdown, err := d.uploadEvidence(info, evidence)
	if err != nil {
		fmt.Fprintf(stderr, "Error uploading %s: %v\n", path, err)
		return 1
	}
	fmt.Fprintln(stdout, markdown)
	return 0
}
