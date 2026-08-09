package repo

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// Info identifies a GitHub repository.
type Info struct {
	Owner string
	Name  string
}

// runner runs an external command and returns its stdout. Injected so the
// git/gh subprocess boundary is stubbable in tests.
type runner func(name string, args ...string) ([]byte, error)

// execRun is the production runner; it shells out via os/exec.
func execRun(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

var (
	sshRemoteRe   = regexp.MustCompile(`git@github\.com:([^/]+)/([^/]+?)(?:\.git)?$`)
	httpsRemoteRe = regexp.MustCompile(`https://github\.com/([^/]+)/([^/]+?)(?:\.git)?$`)
)

// fromRemote infers the GitHub owner/repo from the git remote in the current directory.
func fromRemote(run runner) (owner, name string, err error) {
	out, err := run("git", "remote", "get-url", "origin")
	if err != nil {
		return "", "", fmt.Errorf("not a git repository or no 'origin' remote configured")
	}
	remote := strings.TrimSpace(string(out))

	if m := sshRemoteRe.FindStringSubmatch(remote); m != nil {
		return m[1], m[2], nil
	}
	if m := httpsRemoteRe.FindStringSubmatch(remote); m != nil {
		return m[1], m[2], nil
	}

	return "", "", fmt.Errorf("could not parse GitHub owner/repo from remote URL: %s", remote)
}

// resolve returns repository coordinates, inferring them from git when empty.
func resolve(run runner, owner, name string) (*Info, error) {
	if owner == "" || name == "" {
		var err error
		owner, name, err = fromRemote(run)
		if err != nil {
			return nil, err
		}
	}

	return &Info{Owner: owner, Name: name}, nil
}

// Resolve returns full repo info. If owner/name are empty, it infers from the git remote.
func Resolve(owner, name string) (*Info, error) {
	return resolve(execRun, owner, name)
}
