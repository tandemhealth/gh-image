# Architecture

`gh-image` has three commands:

```text
gh image [--repo owner/repo] [--evidence-root absolute-dir] absolute-png
gh image init [--repo owner/repo]
gh image check-access [--repo owner/repo]
```

## Authentication boundary

All GitHub requests run as `gh api` subprocesses. The GitHub CLI supplies its existing login. `gh-image` never asks `gh` to print its token and has no credential input, environment variable, keyring, cookie jar, or browser integration.

An explicit `--repo` requires no subprocess before GitHub access. Without it, `internal/repo` reads only `git remote get-url origin` and extracts the GitHub owner and repository.

## Managed release

Each target repository has one published prerelease:

| Field | Required value |
|---|---|
| Tag | `gh-image-evidence` |
| Title | `Automated screenshot evidence` |
| Draft | `false` |
| Prerelease | `true` |
| Latest release | `false` |

`gh image init` first checks repository push permission and reads the release by tag. If the release does not exist, it creates it from the default branch. A concurrent initializer can return HTTP 422; the client then reads and validates the release created by the other process.

`check-access` and normal upload require the exact existing metadata. They do not repair or create it.

## Evidence boundary

`internal/upload.OpenEvidence` runs before repository resolution or GitHub access. It requires one absolute PNG below one absolute evidence root. It rejects symlinks in every path component, path escapes, non-regular files, malformed PNG chunks, unsafe dimensions, and files over 10,000,000 bytes. It decodes and re-encodes the image into an immutable in-memory snapshot.

The uploader hashes that snapshot with SHA-256. The release asset name is `<64-lowercase-hex>.png`, so the same bytes have the same URL.

## Upload sequence

1. Read repository metadata and require `permissions.push=true`.
2. Read and validate the `gh-image-evidence` prerelease.
3. List every release asset with `per_page=100`, `--paginate`, and `--slurp`.
4. Reuse an asset only when its name, `uploaded` state, size, and exact browser URL match.
5. If no match exists, send the PNG to GitHub's documented release-asset endpoint through `gh api --input -`.
6. If another process wins the same upload and GitHub returns HTTP 422, reread the exhaustive asset list with bounded backoff until the exact asset reaches `uploaded` with the expected size.
7. Print Markdown containing `https://github.com/<owner>/<repo>/releases/download/gh-image-evidence/<digest>.png`.

The client has no delete, rename, clobber, or replace request. A matching name with a different size is an error.

## GitHub hosts

Metadata uses `api.github.com` through ordinary `gh api` endpoints. Asset bytes use the documented `uploads.github.com` URL. The returned link is accepted only when it exactly matches the named repository, managed tag, and digest filename on `github.com`.
