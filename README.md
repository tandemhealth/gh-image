# gh-image

Upload one validated PNG to GitHub and print Markdown for an issue, pull request, or comment.

`gh-image` stores screenshots as assets on a dedicated prerelease in the target repository. It uses GitHub's documented Releases REST API through the existing `gh` CLI login. It does not read browser cookies, accept tokens, store credentials, or add image bytes to Git history.

```console
$ GH_IMAGE_EVIDENCE_ROOT=/absolute/evidence \
    gh image /absolute/evidence/screenshot.png --repo owner/repo
![screenshot.png](https://github.com/owner/repo/releases/download/gh-image-evidence/8f2c…a931.png)
```

The filename is the SHA-256 digest of the validated PNG. Uploading the same bytes again reuses the existing asset. The command never deletes or replaces an asset.

## Install

```bash
gh extension install tandemhealth/gh-image --pin v1.4.0-tandem.1
gh image --version
```

Replace an older installation:

```bash
gh extension remove image
gh extension install tandemhealth/gh-image --pin v1.4.0-tandem.1
gh image --version
```

Expected output: `gh-image v1.4.0-tandem.1`.

## One-time repository initialization

Create the managed prerelease once in each target repository:

```bash
gh image init --repo owner/repo
```

This creates a published prerelease with tag `gh-image-evidence`, title `Automated screenshot evidence`, and `make_latest=false`. Normal uploads and `check-access` are read-only until the final asset upload. They never create a tag or release.

Verify access without changing GitHub:

```bash
gh image check-access --repo owner/repo
```

Both commands use the account already shown by `gh auth status --hostname github.com`. There is no second credential setup.

## Upload

```bash
GH_IMAGE_EVIDENCE_ROOT=/absolute/evidence \
  gh image /absolute/evidence/screenshot.png --repo owner/repo
```

`--repo` is optional inside a Git checkout whose `origin` points to GitHub. `--evidence-root` can replace `GH_IMAGE_EVIDENCE_ROOT`.

The command accepts exactly one absolute `.png` path below the absolute evidence root. Before any GitHub request, it rejects batches, symlinks, non-regular files, invalid PNG data, images over 10,000,000 bytes, and images whose decoded dimensions exceed its safety limit. It snapshots and canonicalizes the image, then uploads those exact bytes.

Write the returned Markdown through a body file:

```bash
GH_IMAGE_EVIDENCE_ROOT=/absolute/evidence \
  gh image /absolute/evidence/screenshot.png --repo owner/repo > /tmp/screenshot.md
gh issue comment 123 --repo owner/repo --body-file /tmp/screenshot.md
```

Private-repository assets remain private. A viewer must be authenticated and authorized for the repository.

## Authentication

`gh-image` delegates every GitHub request to `gh api`. It does not run `gh auth token`, expose a credential in arguments or output, or maintain its own credential store. If a sandbox cannot read the host's existing `gh` login, rerun the exact command through that environment's narrow host-keychain execution path. Do not create or paste another token.

The authenticated account needs push access to the named target repository. `init` needs permission to create a release. Uploads need permission to upload release assets.

## CI

GitHub Actions can use the job's normal `GITHUB_TOKEN`; no second secret is required. Initialize the release once before relying on the job.

```yaml
permissions:
  contents: write

steps:
  - uses: actions/checkout@v4
  - name: Upload screenshot
    env:
      GH_TOKEN: ${{ github.token }}
      GH_IMAGE_EVIDENCE_ROOT: ${{ github.workspace }}/test-results
    run: |
      gh extension install tandemhealth/gh-image --pin v1.4.0-tandem.1
      gh image check-access --repo "${{ github.repository }}"
      gh image "$GH_IMAGE_EVIDENCE_ROOT/screenshot.png" --repo "${{ github.repository }}"
```

## Build and test

Requires Go 1.26 or later.

```bash
go test -race -cover ./...
go vet ./...
```

The Git tree contains source only. GoReleaser publishes checksum-listed executables for supported platforms.

## Design

See [documentation/architecture.md](documentation/architecture.md) for the request sequence, collision handling, and security boundaries. [documentation/github-image-upload-flow.md](documentation/github-image-upload-flow.md) records the removed browser-session protocol for historical comparison; current builds do not contain that code.

## License

[MIT](LICENSE) © 2026 Tandem Health.
