---
name: github-image-upload
description: >-
  Upload one agent-generated PNG screenshot to GitHub and embed it in a pull
  request description, an issue, or a comment — producing a canonical
  github.com/user-attachments URL (private-repo uploads stay private). Use when
  asked to "attach a screenshot to the PR", "add an image to the PR description",
  "put this image in the issue", "show test results in the PR", or "embed a
  screenshot". Powered by Tandem's locked-down `gh-image` gh CLI extension.
license: MIT
---

# Upload one PNG screenshot to GitHub (gh-image)

GitHub has **no public API** for attachment uploads — the web UI uses an internal
endpoint that mints `user-attachments` URLs scoped to the repo's visibility.
[`gh-image`](https://github.com/tandemhealth/gh-image) replicates that flow as a
`gh` CLI extension. The Tandem build accepts exactly one absolute PNG beneath a
configured evidence directory and returns an `![name](url)` embed. It rejects
other file types, batches, symlinks, non-regular files, and images over
10,000,000 bytes before accessing GitHub credentials.

This skill drives `gh-image` and then embeds the result into a PR/issue/comment.

## Prerequisites — verify these before uploading

Run these checks; only act on the ones that fail.

1. **`gh` CLI installed & authenticated**

   ```bash
   gh auth status
   ```
   If it fails, tell the user to run `gh auth login` (do not attempt it unattended).

2. **The exact Tandem `gh-image` version installed** (idempotent)

   ```bash
   if ! gh extension list | awk '$1 == "gh" && $2 == "image" && $3 == "tandemhealth/gh-image" && $4 == "v1.3.0-tandem.1" { found=1 } END { exit !found }'; then
     gh extension remove image >/dev/null 2>&1 || true
     gh extension install tandemhealth/gh-image --pin v1.3.0-tandem.1
   fi
   ```

3. **A GitHub session for the upload.** `gh-image` does NOT use the `gh` token for
   the upload (that endpoint rejects tokens); it needs GitHub's `user_session`
   cookie. Resolution order (first match wins):
   - `--token <value>` flag, or
   - `GH_SESSION_TOKEN` env var (CI / headless only), or
   - the dedicated `tandemhealth-gh-image` operating-system credential.

   Run `gh image check-token`. If it reports that no `gh-image` credential exists,
   stop and tell the user to provision it from a trusted terminal as documented in
   the extension README. **Do not** inspect browser cookie databases, request
   Chrome Safe Storage access, run `gh image auth-store`, or ask the user to paste
   a session token into the agent chat. The installed extension has no browser
   cookie reader and no command that prints its stored credential.

   > ⚠️ A `user_session` cookie grants **full account access** (it is not scoped
   > like a PAT). Treat it like a password; in CI use a dedicated bot account.

4. **A dedicated evidence directory.** Set `GH_IMAGE_EVIDENCE_ROOT` to the
   absolute directory where the screenshot harness writes PNGs. Do not set it
   to `/`, a home directory, or the repository root.

## Step 1 — Validate the screenshot path

Use exactly one **absolute `.png` path** beneath `GH_IMAGE_EVIDENCE_ROOT`. Do not
use a glob. The extension snapshots the validated file before network access.

## Step 2 — Upload

```bash
# --repo is optional inside a repository working directory.
GH_IMAGE_EVIDENCE_ROOT=/abs/path/evidence \
  gh image "/abs/path/evidence/screenshot.png" --repo <owner>/<repo>
```

`gh image` prints one image reference to **stdout**:

```
![screenshot.png](https://github.com/user-attachments/assets/<uuid>)
```

Capture that output; it is the embeddable reference.

## Step 3 — Embed into the PR / issue / comment

`gh-image` only prints the markdown; you embed it. Pick the target the user asked for.

**Append to a PR description** (preserves the existing body):

```bash
MD="$(GH_IMAGE_EVIDENCE_ROOT=/abs/path/evidence gh image "/abs/path/evidence/shot.png" --repo owner/repo)"
BODY="$(gh pr view <pr> --repo owner/repo --json body -q .body)"
printf '%s\n\n## Screenshots\n\n%s\n' "$BODY" "$MD" \
  | gh pr edit <pr> --repo owner/repo --body-file -
```

**Post as a new PR comment:**

```bash
MD="$(GH_IMAGE_EVIDENCE_ROOT=/abs/path/evidence gh image "/abs/path/evidence/shot.png" --repo owner/repo)"
printf '## Screenshots\n\n%s\n' "$MD" | gh pr comment <pr> --repo owner/repo --body-file -
```

**Add to an issue body / comment:** same pattern with `gh issue edit <n> --body-file -`
or `gh issue comment <n> --body-file -`.

Always use `--body-file -` (not inline `--body`) so multi-line bodies and special
characters can't break shell quoting.

## Step 4 — Verify

```bash
gh pr view <pr> --repo owner/repo --json body -q .body   # confirm the URL is present
```

The `user-attachments` URL inherits the repo's visibility, so on a **private** repo
it renders only for authorized viewers (an anonymous fetch returns 404/403 — that is
expected, not a failure).

## Sizing (optional)

To control display size, embed an HTML tag instead of the bare markdown:

```html
<img width="800" alt="screenshot" src="https://github.com/user-attachments/assets/<uuid>" />
```

## Troubleshooting

| Symptom | Cause / fix |
|---|---|
| `<org> enforces SAML SSO and your session is not authorized…` | The org requires SSO and your session isn't authorized. Open the `https://github.com/orgs/<org>/sso` URL from the message in a browser, authorize (lasts ~24h), then retry. Write access alone is not enough — this is not a permissions problem. |
| `uploadToken not found … do you have write access?` | The generic no-token case. Confirm you have write access; if the repo's org uses SSO, authorize at `https://github.com/orgs/<org>/sso` (the message includes this hint) and retry. |
| No `gh-image` credential found | The user must provision the dedicated credential from a trusted terminal; do not request or extract the value in the agent session. |
| CI / headless run | Set `GH_SESSION_TOKEN` from a secret store using a dedicated bot account. |
| `evidence root is required` | Set `GH_IMAGE_EVIDENCE_ROOT` to the absolute screenshot output directory. |
| `outside evidence root`, `symlink`, `regular file`, or `invalid PNG` | Regenerate the screenshot directly inside the evidence directory; do not copy through a symlink. |
| `gh: command not found` | Install the GitHub CLI (`brew install gh`, etc.). |
