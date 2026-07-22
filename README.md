# gh-image

Attach agent-generated PNG screenshots to GitHub issues and pull requests.

<p align="center">
  <a href="https://github.com/tandemhealth/gh-image/releases/latest"><img src="https://img.shields.io/github/v/release/tandemhealth/gh-image?color=blue" alt="Latest release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/tandemhealth/gh-image?color=lightgrey" alt="License: MIT"></a>
  <a href="https://github.com/tandemhealth/gh-image/actions/workflows/lint.yml"><img src="https://github.com/tandemhealth/gh-image/actions/workflows/lint.yml/badge.svg" alt="Lint"></a>
</p>

---

GitHub has no public API for the attachment uploads its web UI accepts via drag-and-drop. This Tandem fork uses that internal endpoint for one purpose: upload one agent-generated PNG screenshot from a configured evidence directory and return a repository-scoped `user-attachments` URL. It rejects batches, relative paths, non-PNG files, symlinks, non-regular files, and PNGs over 10,000,000 bytes before reading GitHub credentials or making network requests.

```console
$ GH_IMAGE_EVIDENCE_ROOT=/absolute/evidence \
    gh image /absolute/evidence/screenshot.png
![screenshot.png](https://github.com/user-attachments/assets/88f4599a-…-bc24)
```

## Install

New installation:

```bash
gh extension install tandemhealth/gh-image --pin v1.2.0-tandem.2
gh image --version
```

Replace an existing installation:

```bash
gh extension remove image
gh extension install tandemhealth/gh-image --pin v1.2.0-tandem.2
gh image --version
```

Expected output: `gh-image v1.2.0-tandem.2`.

The Git tree contains no executable binaries, binary assets, or Git LFS objects. Prebuilt executables exist only as the six checksum-published assets on the pinned GitHub Release.

<details>
<summary>Build from source</summary>

```bash
git clone https://github.com/tandemhealth/gh-image
cd gh-image
go build -o gh-image
gh extension install .
```

Requires Go 1.26+.

</details>

## Usage

```bash
# Upload an image (infers repo from the current git workspace)
GH_IMAGE_EVIDENCE_ROOT=/absolute/evidence \
  gh image /absolute/evidence/screenshot.png

# Target a specific repository
GH_IMAGE_EVIDENCE_ROOT=/absolute/evidence \
  gh image /absolute/evidence/screenshot.png --repo owner/repo
```

Each successful upload prints one ready-to-paste image reference:

```
![screenshot.png](https://github.com/user-attachments/assets/…)
```

If validation or upload fails, the error is printed to stderr and the process exits non-zero.

### Pipe directly into an issue, PR, or comment

From inside the repo's working directory, both `gh image` and `gh issue create` infer the target repository automatically:

```bash
gh issue create \
  --title "Login button stuck in loading state" \
  --body "Repro on staging:

$(GH_IMAGE_EVIDENCE_ROOT="$PWD/test-results" gh image "$PWD/test-results/bug.png")

Happens consistently after the third click."
```

## Use with AI agents

`gh-image` is packaged as an [agent skill](https://agentskills.io), so AI coding agents can upload and embed synthetic screenshots without a per-upload browser interaction.

Share this setup block with developers who use Codex, Claude Code, Cursor, or another Agent Skills client:

```bash
npx --yes playwright install chromium
gh auth status --hostname github.com
gh extension install tandemhealth/gh-image --pin v1.2.0-tandem.2
npx --yes skills@1.5.9 add tandemhealth/gh-image --global --skill github-image-upload --agent '*' --yes
```

If `gh auth status` fails, run `gh auth login --hostname github.com`. Restart the agent after the skill is installed so it discovers `github-image-upload`. The Playwright command installs Chromium for screenshot capture; the `gh extension` command installs the uploader; the final command registers the upload workflow globally for every detected agent.

The open [Agent Skills standard](https://agentskills.io/clients) is supported by **Claude Code**, **OpenAI Codex**, **Cursor**, **GitHub Copilot**, and [many more](https://agentskills.io/clients). The skill walks the agent through checking the pinned extension, running the upload, and embedding the resulting `user-attachments` URL into a PR, issue, or comment.

## Authentication

`gh-image` authenticates with your existing GitHub session — **no tokens to provision, no OAuth scopes to configure** for everyday local use. The tool reads the `user_session` cookie from your browser's encrypted cookie store.

**Supported browsers:** Chrome · Brave · Chromium · Edge · Firefox · Opera · Safari

**Supported platforms:** macOS · Linux · Windows · Android (Termux)

On macOS, a Keychain prompt may appear on first use to authorize access to your browser's cookie encryption key. Click **Always Allow** to skip future prompts.

> [!NOTE]
> **When browser cookies aren't available:** Chrome 127+ on Windows is not supported by the current cookie library, and Android (Termux) has no browser cookie store. Supply the token explicitly via `GH_SESSION_TOKEN` (see [Session token override](#session-token-override)); on Windows you can also use another browser.

### Session token override

For CI, headless environments, or shared machines, you can supply the session token explicitly. Resolution order (first match wins):

| Priority | Source | When to use |
|---|---|---|
| 1 | `--token <value>` flag | One-off invocations |
| 2 | `GH_SESSION_TOKEN` env var | CI/CD, shared machines, non-standard browsers |
| 3 | Browser cookie store | Local interactive use (default) |

```bash
# Flag (visible in process listings like `ps aux` — avoid on shared machines)
GH_IMAGE_EVIDENCE_ROOT=/absolute/evidence \
  gh image --token "$MY_TOKEN" /absolute/evidence/screenshot.png --repo owner/repo

# Environment variable (preferred — not visible to `ps aux`)
GH_SESSION_TOKEN="$MY_TOKEN" GH_IMAGE_EVIDENCE_ROOT=/absolute/evidence \
  gh image /absolute/evidence/screenshot.png --repo owner/repo

# Non-standard browser not auto-detected (Firefox forks like Floorp/LibreWolf)?
GH_SESSION_TOKEN="$(sqlite3 ~/path/to/profile/cookies.sqlite "SELECT value FROM moz_cookies WHERE name='user_session' AND host LIKE '%github.com'")" \
  GH_IMAGE_EVIDENCE_ROOT=/absolute/evidence \
  gh image /absolute/evidence/screenshot.png --repo owner/repo
```

> [!WARNING]
> `user_session` cookies grant **full account access** — they are not scoped like personal access tokens. Treat them with the same care as a password. If leaked, **[sign out of GitHub](https://github.com/logout)** on the machine that holds the session; if you are not on that machine, revoke it through [Settings → Sessions](https://github.com/settings/sessions), or [change your password](https://github.com/settings/security) (which kills every session in one action).


## CI / CD

`gh-image` runs unattended in GitHub Actions when given a session token via `GH_SESSION_TOKEN`.

> [!CAUTION]
> **Use a dedicated bot account for CI/CD on shared repos.** GitHub hides secret values in the UI and masks log emissions, but a determined collaborator with write access can craft a workflow that exfiltrates the value through channels masking doesn't cover. Storing your *personal* `user_session` means such a leak compromises your account; a bot account scopes the blast radius to that bot. Decide whose token to extract in step 1 below accordingly.

**Setup**

1. Run `gh image extract-token` locally to capture the token (token → stdout, status → stderr), then run `gh image check-token --token <token>` to confirm it authenticates as the intended user (username → stdout on success, exit code `0` = valid).
2. Create a GitHub environment (Settings → Environments → New environment), e.g. `gh-image`, and restrict deployment branches to a trusted set (e.g. `main` only).
3. Add the token as an **environment secret** named `GH_SESSION_TOKEN` on that environment.

```yaml
jobs:
  upload:
    runs-on: ubuntu-latest
    environment: gh-image                                # binds this job to the scoped environment
    steps:
      - name: Upload screenshots
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}              # for gh CLI auth
          GH_SESSION_TOKEN: ${{ secrets.GH_SESSION_TOKEN }}  # for the upload itself
        run: |
          gh extension install tandemhealth/gh-image --pin v1.2.0-tandem.2
          gh image check-token                                # optional: fail fast if the session expired
          GH_IMAGE_EVIDENCE_ROOT="$GITHUB_WORKSPACE/test-results" \
            gh image "$GITHUB_WORKSPACE/test-results/screenshot.png" --repo ${{ github.repository }}
```

> [!NOTE]
> `user_session` cookies expire when GitHub invalidates the session. A scheduled `check-token` job is the cleanest way to detect expiry before it breaks a real run.

## How it works

1. Validates and snapshots one absolute PNG beneath `GH_IMAGE_EVIDENCE_ROOT` or `--evidence-root`.
2. Resolves a `user_session` cookie from the configured source (flag → env → browser).
3. Fetches the target repository's page to obtain an `uploadToken` from the embedded JS payload.
4. Requests an S3 upload policy from `/upload/policies/assets` using the snapshot's exact size.
5. Uploads the immutable snapshot directly to S3 using the presigned form fields.
6. Calls back to GitHub to finalize the image and prints `![name](url)` to stdout.

The final URL is `https://github.com/user-attachments/assets/<uuid>`. Visibility inherits from the target repository, so a private-repo upload requires authentication to view.

For the full architecture, see **[documentation/architecture.md](documentation/architecture.md)**. For the reverse-engineered upload protocol, see **[documentation/github-image-upload-flow.md](documentation/github-image-upload-flow.md)**.
The Tandem threat model and accepted risks are recorded in **[documentation/security-validation-2026-07-22.md](documentation/security-validation-2026-07-22.md)**. The clean `.2` install, rejection checks, binary hashes, and rendered live-upload evidence are recorded in **[documentation/v1.2.0-tandem.2-validation.md](documentation/v1.2.0-tandem.2-validation.md)**.

## Requirements

- A supported browser with an active GitHub session — or a `GH_SESSION_TOKEN` for CI.
- Write access to the target repository (uploads require it).
- An absolute evidence directory set with `GH_IMAGE_EVIDENCE_ROOT` or `--evidence-root` and one absolute PNG path beneath it.
- A target repository — pass `--repo owner/repo`, or run from a git workspace whose `origin` remote is on GitHub.
- The `gh` CLI must be installed and authenticated (used for repository ID lookup).

## Limitations

- Uses an **undocumented** internal GitHub API that may change without notice.
- `uploadToken` is only issued to users with write access on the target repository.
- Session cookies are not scoped credentials; they expire when GitHub invalidates the session.

## Contributing

Issues and pull requests are welcome. For bug reports, please include:

- Your OS and browser
- The exact `gh image` invocation
- The error output (with any session token values redacted)

Before opening a PR, run `go test ./...` and `go vet ./...`.

## License

[MIT](LICENSE) © 2026 Tandem Health. The original copyright notice is retained in `LICENSE` as required by the MIT license.
