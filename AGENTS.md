# AGENTS.md

Repository instructions for AI coding agents and automated contributors.

## Project Identity

- Repository: `github.com/alimirayman/ms-teams-tui`
- Product name: `ms-teams-tui`
- Installed executable: `teams`
- Go module: `github.com/alimirayman/ms-teams-tui`
- Version source of truth: `VERSION`
- Current config/cache directory name: `ms-teams-tui`
- Legacy `teams-tui-go` data is migrated automatically; do not remove migration support without an explicit breaking release.

This project originated from [`nospor/teams-tui-go`](https://github.com/nospor/teams-tui-go). Preserve upstream attribution in `README.md` and `LICENSE`.

## Git Safety

Do not run git write commands unless the user explicitly requests a commit, push, tag, release, merge, rebase, reset, or repository operation. Read-only commands are allowed.

Never rewrite or move an existing release tag. Never force-push `main` unless the user explicitly requests history rewriting and the consequences have been stated.

## Mandatory Version Policy

Every agent-authored commit containing application code, build logic, installer behavior, configuration behavior, security behavior, or release workflow changes must update `VERSION` in that same commit.

Use semantic versioning:

- Patch: bug fixes, security hardening without API breakage, internal refactors, build fixes, and small behavior corrections.
- Minor: backward-compatible user-facing features, new configuration, new commands, or substantial UI behavior.
- Major: incompatible config/storage/CLI behavior, intentionally breaking changes, or a significant product-level update.

Classify the complete change set by its highest-impact change: fixes increment the patch version, features increment the minor version, and breaking or significant updates increment the major version. Do not use a patch increment for a release that adds a feature.

Use the repository helpers:

```bash
make version
make bump-patch
make bump-minor
make bump-major
make check-version
```

Rules:

1. Read `VERSION` before editing.
2. Decide the required semantic increment from the complete change set.
3. Bump once for the release-ready change set before committing.
4. Update `CHANGELOG.md` for every version bump.
5. Ensure `teams --version` reports `ms-teams-tui vX.Y.Z` after building.
6. Documentation-only commits that do not alter installation, security, compatibility, or release behavior may omit a bump.
7. Do not create a release merely because the version changed; releases require an explicit user request.

## Release Procedure

Only release from a clean, pushed `main` branch. The tag must be exactly `v$(cat VERSION)`.

```bash
make release-check
make release
```

`make release` creates and pushes an annotated tag. The pinned GitHub Actions workflow then:

- Revalidates the tag against `VERSION`
- Runs tests, race detection, vet, govulncheck, and gosec
- Builds static Linux, macOS, and Windows archives
- Embeds the exact version in the binary
- Generates SHA-256 checksums
- Publishes the GitHub release

After tagging, verify all of the following before reporting success:

```bash
gh run list --workflow Release --limit 1
gh release view "v$(cat VERSION)"
```

Confirm that every expected archive and `checksums.txt` exists. Never claim a release succeeded while the workflow is queued or running.

## Required Validation

For code changes run:

```bash
make check-version
go mod verify
go test ./...
go test -race ./...
go vet ./...
make security
make build
./bin/teams --version
```

For documentation-only changes, run at least `git diff --check` and validate commands, paths, links, and version references against the source.

## Security Requirements

- No telemetry, analytics, advertising, crash upload, remote logging, hidden webhooks, or update daemons.
- No committed executables, tokens, config files, caches, downloaded attachments, or generated release archives.
- OAuth tokens must remain mode `0600` in a mode `0700` cache directory.
- Never log authorization headers, access tokens, refresh tokens, upload-session URLs, or message bodies.
- Microsoft transfer URLs must pass `validateMicrosoftTransferURL`; do not weaken this to substring matching.
- Bearer tokens may only be attached when the parsed hostname is exactly `graph.microsoft.com`.
- Any redirect used for file transfer must be revalidated.
- Downloaded attachment names must pass `safeAttachmentFilename` before joining with a local directory.
- User-configurable commands must use `exec.Command` argument arrays, never `sh -c`, `bash -c`, or equivalent.
- New subprocess or filesystem access must be user-triggered or app-owned and covered by a specific test.
- GitHub Actions must be pinned to full commit SHAs.
- The installer must verify release checksums before replacing `teams`.
- Review `SECURITY.md` whenever network, auth, file, subprocess, notification, or release behavior changes.

Do not silence a security scanner globally. A `#nosec` annotation is allowed only on a reviewed intentional capability, must name the exact rule, and must include a concrete justification.

## Architecture

### Authentication

- Device-code OAuth is implemented in `auth.go`.
- Always construct scopes through `BuildScopes()`.
- Background API commands must call `GetValidTokenSilent(clientID)`.
- Config values come from environment/`.env`, then `config.json`, then defaults.

### Graph API

- Graph models and API calls live in `api.go`.
- Use structured JSON and the existing HTTP helpers.
- Display names are computed in the API layer and stored in `Chat.CachedDisplayName`.
- Preserve application senders so bot and Workflow messages remain visible.
- Teams card attachments are terminal-rendered in `adaptive_card.go`; do not filter supported card MIME types merely because they are not Adaptive Cards.

### Bubble Tea

- Runtime state is in `app.go`; event handling and rendering are in `ui.go`; async `tea.Cmd` functions are in `main.go`.
- Network and disk operations must not block `Update` or `View`.
- Read feature flags from `app.Features` inside the event loop.
- The API returns messages newest-first; the timeline renders in reverse.
- Mouse-wheel events always scroll the active message timeline and must never change chat or channel selection.
- Preserve terminal-native partial text selection with `Shift`+drag. When a message is selected and the terminal forwards `Cmd+C`, copy the complete plain-text message; keep `y` as the fallback.
- Modified compose shortcuts rely on the Kitty keyboard disambiguation flag. Push it from `Model.Init` after Bubble Tea enters the alternate screen; the terminal keeps a separate alt-screen keyboard stack and discards it on exit. Keep ordinary text input in legacy form.
- Unicode width must use existing cell/grapheme helpers. Do not use byte length for layout.

### Files and Images

- Clipboard reads happen only after explicit paste actions; clipboard writes happen only after explicit copy actions.
- File uploads happen only after explicit picker selection, paste, or drag/drop input.
- The file picker is type-first: printable keys update its fuzzy filter and arrow keys navigate results.
- Dotfiles and dot-directories are intentionally visible in the picker.
- Image preview cache paths are content-addressed under the app cache.
- Reference-attachment preview uses delegated `Files.Read.All` because the file can be owned by another Teams participant; upload remains separately gated by `Files.ReadWrite`.
- Inline image rendering uses Kitty Graphics Protocol sequences.
- Keep Kitty transmission and placement compatible with cmux's Ghostty renderer: quiet 4096-byte chunks, image IDs in `1..2_000_000_000`, and `ESC 7/8` cursor save/restore around placements.
- Attachment quick preview is explicit and user-triggered; macOS uses `/usr/bin/qlmanage -p` against an app-owned cache file.

### Persistence

- Use `GetAppDir()` and `GetCacheDir()` rather than constructing OS paths directly.
- Chat and channel favourites are separate private ID sets in `favourites.json` and `channel_favourites.json`.
- Keep config/cache migration tests when changing storage.
- SQLite schema and access are in `db.go`.

## Documentation Maintenance

Update documentation in the same change when behavior changes:

- `README.md`: installation, commands, config, platform paths, user-facing behavior
- `AZURE_SETUP.md`: Graph scopes, consent, authentication setup
- `SECURITY.md`: network, local execution, storage, threat boundaries
- `CHANGELOG.md`: every version bump
- `AGENTS.md`: architecture, release, or contributor rules

The user-facing command remains `teams`, even though the repository and product are named `ms-teams-tui`.
