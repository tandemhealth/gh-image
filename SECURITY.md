# Security policy

`gh-image` uses `gh api`, so GitHub authentication stays inside the installed GitHub CLI. The extension does not accept, print, read, or store a browser session, personal access token, OAuth token, or browser encryption key.

Every request names one target repository. The authenticated account must have push access. `gh image init` creates only the `gh-image-evidence` prerelease and tag. Uploads use content-addressed names and never delete or replace assets.

Input policy is enforced before a GitHub request. The command accepts one absolute PNG below a configured absolute evidence root, rejects symlinks and non-regular files, validates the PNG structure and decode limits, and uploads an immutable canonical snapshot.

Report vulnerabilities through [GitHub private vulnerability reporting](https://github.com/tandemhealth/gh-image/security/advisories/new). Include the affected version, impact, reproduction steps, and any proof of concept.

Security fixes apply to the latest release only.
