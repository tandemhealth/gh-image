---
name: github-image-upload
description: >-
  Upload one agent-generated PNG screenshot to GitHub and embed it in a pull
  request description, issue, or comment. Use when asked to attach, add, put,
  show, or embed a screenshot in GitHub.
license: MIT
---

# Upload one PNG screenshot to GitHub

Use Tandem's pinned `gh-image` extension. It uploads one validated PNG as a content-addressed asset on the target repository's `gh-image-evidence` prerelease. It uses the existing GitHub CLI login and must never prompt for, read, paste, or store another credential.

## Check setup

1. Confirm the pinned version:

   ```bash
   gh image --version
   ```

   The required version is `v1.4.0-tandem.1`. If it differs, install the exact release:

   ```bash
   gh extension remove image
   gh extension install tandemhealth/gh-image --pin v1.4.0-tandem.1
   ```

2. Run the read-only target check:

   ```bash
   gh image check-access --repo owner/repo
   ```

   If the sandbox cannot read the existing host `gh` login, retry this exact command once through the execution surface's narrow host-keychain permission. Do not run a login flow or ask for a key. If the release is missing, `gh image init --repo owner/repo` is the one-time repository setup; run it only when the task authorizes that target repository write.

3. Set `GH_IMAGE_EVIDENCE_ROOT` to the dedicated absolute screenshot-output directory. Never use `/`, a home directory, or a repository root.

## Upload

Inspect the PNG first. A blank, wallpaper-only, corrupt, or wrong-page capture is not evidence.

Use one absolute path below the evidence root:

```bash
GH_IMAGE_EVIDENCE_ROOT=/absolute/evidence \
  gh image /absolute/evidence/screenshot.png --repo owner/repo
```

Capture the one-line Markdown from stdout. Do not use a glob or batch.

## Embed and verify

Use `--body-file` for the requested issue, pull request, or comment. Preserve existing body text when editing.

```bash
gh issue comment 123 --repo owner/repo --body-file /tmp/screenshot.md
```

Read the target back and confirm that it contains the exact release URL. For a private repository, inspect the rendered target while authenticated as a collaborator and confirm that the image loads. Record anonymous access separately; a private asset can reject anonymous viewers without failing the task.

The accepted generated URL has this shape:

```text
https://github.com/owner/repo/releases/download/gh-image-evidence/<64-lowercase-hex>.png
```

Existing `github.com/user-attachments` URLs remain valid evidence and must not be rewritten.

## Failures

| Error | Action |
|---|---|
| Evidence root missing | Set the dedicated absolute output directory. |
| Path outside root, symlink, invalid PNG, or size error | Regenerate the PNG directly inside the evidence directory. |
| Managed release missing | Run the one-time `init` only when the named repository write is authorized. |
| No push access | Stop. Do not request a broader token or generic permission. |
| Existing digest has the wrong size or never reaches `uploaded` | Stop. Do not delete or replace the asset. Report the exact state and size. |
| Host `gh` login unavailable only inside a sandbox | Retry the exact command through the narrow host-keychain execution path once. |
