# Tandem security validation — 2026-07-22

The Tandem release candidate is **validated for one agent-generated PNG upload per invocation**. Validation used Go 1.26.5, 196 passing Go test cases, race detection, `go vet`, golangci-lint 2.12.2, govulncheck 1.6.0, two identical candidate builds of six platform binaries, and one live upload to `tandemhealth/gh-image`. The old input path accepted any number of arbitrary readable files; the validated path accepts exactly one absolute PNG beneath a configured evidence directory. No Tandem release existed when this report was written; publication is a separate final step after independent patch approval.

The input changes are directly tied to the audit findings:

- Inputs per invocation: unbounded `<file-path>...` → exactly 1.
- Accepted types: any readable file → fully decoded and canonically re-encoded PNG only; trailing bytes and source metadata are discarded or rejected.
- Path scope: current working directory or any readable path → absolute path beneath `GH_IMAGE_EVIDENCE_ROOT` or `--evidence-root`.
- Filesystem objects: followed symlinks and accepted non-regular files → rejects root, parent-directory, and file symlinks; rejects directories, devices, and other non-regular files.
- Read behavior: `os.Stat(path)` followed by a later `os.Open(path)` → one `os.Root`-scoped open and an immutable byte snapshot before repository lookup, cookie access, or network I/O.
- Size: no client limit → 10,000,000 bytes maximum, tested at 10,000,000 accepted and 10,000,001 rejected.
- Decode allocation: no screenshot-specific bound → at most 20,000 pixels per dimension and 40,000,000 total pixels before full decoding.
- Filename: arbitrary basename → at most 128 bytes, starting with an ASCII letter or digit and containing only ASCII letters, digits, `.`, `-`, and `_`.
- Policy response: any returned attachment content type → exactly `image/png`.

The first 2026-07-22 scan found 1 reachable vulnerability, `GO-2026-5970`, through `golang.org/x/text` 0.34.0 in browser-cookie parsing. `golang.org/x/text` 0.34.0 → 0.39.0 reduced reachable vulnerabilities 1 → 0. The final govulncheck result also reported 0 vulnerabilities in directly imported packages; 23 vulnerabilities existed in required modules but had no reachable call path from this program.

Verification results:

- `go test -race -cover ./...`: 196 test and subtest passes. Statement coverage was 86.0% in `main`, 93.3% in `internal/cookies`, 92.6% in `internal/repo`, 89.3% in `internal/session`, and 84.9% in `internal/upload`.
- `go vet ./...`: 0 diagnostics.
- golangci-lint 2.12.2: 0 issues.
- govulncheck 1.6.0 with Go 1.26.5: 0 reachable vulnerabilities.
- Candidate cross-build matrix: macOS amd64/arm64, Linux amd64/arm64, Windows amd64, and Android arm64. Two candidate builds produced identical SHA-256 values for all 6 binaries.
- Live synthetic upload after complete decode and canonical re-encoding: `validation.png` produced [a repository-scoped GitHub user attachment](https://github.com/user-attachments/assets/c9eb0d6c-71d2-416c-b535-e98f35302854).

The validation keeps four explicitly accepted behaviors unchanged:

- `GH_SESSION_TOKEN` remains inherited by the local `git` and `gh` repository-resolution subprocesses.
- Browser-cookie discovery and `extract-token` remain available; the program can use a developer's logged-in GitHub browser session.
- The S3 upload uses GitHub's server-provided policy URL and the HTTP client's normal redirect behavior.
- The release workflow still refers to tagged GitHub Actions and a moving Go toolchain. The planned `v1.2.0-tandem.1` assets will be built separately with exactly Go 1.26.5 after approval.

`GH_IMAGE_EVIDENCE_ROOT` is a safety rail against accidental file selection. It does not protect against an agent or process allowed to replace its own environment or command-line arguments. Hard links can give the same inode a second name outside the evidence directory. Neither limitation changes the uploaded bytes after validation because the client uploads the immutable in-memory snapshot.

No telemetry or non-GitHub credential destination was found in the reviewed source. GitHub session cookies are sent only to `github.com`; S3 receives the PNG snapshot and presigned form fields, not the GitHub cookie. The undocumented GitHub API may still change without notice.
