# Security Policy

## Supported Versions

Security fixes are applied to the latest published release. Upgrade before reporting an issue that is already fixed on `main`.

## Reporting a Vulnerability

Use GitHub's private vulnerability reporting for this repository. Do not open a public issue for token exposure, arbitrary code execution, path traversal, SSRF, or another unpatched vulnerability.

Include the affected version, operating system, reproduction steps, impact, and any suggested mitigation. Do not include real Microsoft access or refresh tokens.

## Privacy and Network Model

`ms-teams-tui` does not include telemetry, analytics, advertising, crash reporting, remote logging, or an update daemon.

Normal background network traffic is limited to:

- `login.microsoftonline.com` for OAuth device-code authentication and refresh
- `graph.microsoft.com` for Teams, profile, presence, OneDrive, and SharePoint Graph operations
- Microsoft-controlled HTTPS OneDrive/SharePoint transfer hosts returned by Graph
- `teams.microsoft.com` only when the user explicitly launches an audio/video call link

Attachment transfers reject plain HTTP, userinfo URLs, nonstandard ports, loopback/private arbitrary hosts, and lookalike domains. Redirects are revalidated before following them. A Graph bearer token is attached only when the parsed hostname is exactly `graph.microsoft.com`.

When file preview is enabled, the app requests delegated `Files.Read.All` so it can read Teams attachments owned by other participants. This is read-only and cannot exceed the files the signed-in user can already access. Upload remains separately gated by `Files.ReadWrite`.

## Local Data

- Config and cache directories use mode `0700`.
- OAuth tokens, configuration, state, and downloaded attachments use mode `0600` where the platform supports POSIX permissions.
- Existing `teams-tui-go` config/cache directories are migrated to `ms-teams-tui` only when the legacy path is a real directory, never a symlink.
- The executable does not read arbitrary files in the background. File reads happen for app-owned state, an explicit file-picker selection, an explicit pasted image, or an explicit preview request.

## Local Process Execution

The app launches local processes only for user-requested functions:

- The configured editor for `Ctrl+g`
- The configured browser/URL handler for `o`, calls, and downloaded files
- `/usr/bin/qlmanage -p` on macOS after an explicit attachment quick-preview action
- Clipboard helpers after an explicit paste action
- `cmux notify` when running inside cmux with System/Both notifications enabled

These commands are executed directly with argument arrays, not through a shell.

## Release Security

- `VERSION` is the release source of truth.
- Release tags must exactly match `v$(cat VERSION)`.
- CI runs tests, race detection, `go vet`, `govulncheck`, and `gosec` before release builds.
- GitHub Actions are pinned to full commit SHAs.
- Release binaries use `CGO_ENABLED=0`, `-trimpath`, and an embedded version.
- Release assets include SHA-256 checksums, and `scripts/install.sh` verifies the checksum before installation.
- Prebuilt binaries are not committed to the repository.
