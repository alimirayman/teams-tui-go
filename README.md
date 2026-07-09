# teams

A keyboard-first Microsoft Teams client for the terminal, built for fast chat and channel navigation with Go, Bubble Tea, and Microsoft Graph.

This repository is the [`alimirayman/teams-tui-go`](https://github.com/alimirayman/teams-tui-go) fork. It includes a redesigned, Unicode-safe timeline; inline image support for Kitty-compatible terminals and cmux; Adaptive Card rendering; attachment upload/download; cmux notifications; Saved Messages discovery; and Teams call handoff.

## Capabilities

| Area | Support |
| --- | --- |
| Chats | One-to-one, group, meeting, bot, and Saved Messages conversations |
| Channels | Browse, read, post, reply, edit, delete, react, search, and mention users |
| Rendering | Unicode/Bangla-safe wrapping, Markdown, HTML, code blocks, mentions, reactions, Adaptive Cards, links, images, and files |
| Compose | Multiline messages, Markdown, replies, mentions, clipboard images, local files, and external editor |
| Files | Timeline thumbnails, terminal previews, downloads, and uploads up to 50 MB |
| Navigation | Stable activity ordering, favourites, unread markers, history search, long-message collapse, and sleep mode |
| Notifications | Console bell, native desktop notifications, or cmux-native notifications with previews |
| Calls | One-key handoff to official Teams audio or video calls |

### Important Limits

- This is an independent Graph client, not an official Microsoft Teams client.
- Audio and video are handed off to the Teams app or browser. Media does not run inside the terminal.
- Adaptive Cards are rendered as readable terminal content. Open-URL actions work; interactive form inputs and `Action.Submit` execution are not implemented.
- Inline terminal images require Kitty Graphics Protocol support. cmux supports this; unsupported terminals show the attachment name and retain download/open actions.
- Notifications are produced by the running `teams` process. Quit the process and polling stops.
- Graph cannot create a one-to-one chat with the same user twice. The `s` shortcut finds an existing self-chat; create one once in Teams if none exists.

## Requirements

- Go 1.25 or newer
- A Microsoft 365 account with Teams access
- A Microsoft Entra app registration configured for device-code authentication
- A Unicode terminal; cmux or another Kitty-compatible terminal is recommended for inline images

## Install

Clone this fork and build the command as `teams`:

```bash
git clone https://github.com/alimirayman/teams-tui-go.git
cd teams-tui-go
mkdir -p "$(go env GOPATH)/bin"
go build -trimpath -ldflags="-s -w" -o "$(go env GOPATH)/bin/teams" .
```

Ensure the Go binary directory is on `PATH`:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
teams
```

Add that `export` line to `~/.zshrc`, `~/.bashrc`, or the relevant shell profile if needed.

### Upgrade

```bash
cd /path/to/teams-tui-go
git pull --ff-only origin main
go test ./...
go build -trimpath -ldflags="-s -w" -o "$(go env GOPATH)/bin/teams" .
```

## Microsoft Entra Setup

Use your own Entra app registration. The built-in upstream Microsoft client ID can fail with `AADSTS65002` because first-party Microsoft applications require preauthorization that a tenant administrator cannot configure independently.

1. Create an app registration in the Microsoft Entra admin center.
2. Select accounts in your organizational directory unless you specifically need multitenant access.
3. Under **Authentication**, enable **Allow public client flows**.
4. Add the delegated Microsoft Graph permissions required by the features you use.
5. Copy the **Application (client) ID** and **Directory (tenant) ID** into `config.json`.
6. Grant tenant admin consent for permissions that require it.

See [AZURE_SETUP.md](AZURE_SETUP.md) for the portal walkthrough.

### Graph Permissions

Core authentication always requests:

| Permission | Purpose |
| --- | --- |
| `User.Read` | Load the signed-in profile |
| `Chat.ReadWrite` | Read chats, send messages, and update read state |
| `offline_access` | Refresh authentication without signing in every run |

Optional permissions are requested only when their matching config feature is enabled:

| Config key | Delegated permission | Purpose |
| --- | --- | --- |
| `file_preview_enabled` | `Files.Read` | Resolve and download OneDrive/SharePoint attachments |
| `file_upload_enabled` | `Files.ReadWrite` | Upload chat and channel files |
| `presence_enabled` | `Presence.Read.All` | Read user availability and activity |
| `user_profile_enabled` | `User.ReadBasic.All` | Show basic sender profiles |
| `user_profile_extended` | `User.Read.All` | Show job title, department, office, and related profile data |
| `teams_channels_enabled` | `Team.ReadBasic.All`, `Channel.ReadBasic.All`, `ChannelMessage.Read.All`, `ChannelMessage.Send`, `ChannelMessage.ReadWrite` | Enable channel browsing and message operations |
| `channel_mentions_enabled` | `TeamMember.Read.All` | Load channel members for mention autocomplete |

`ChannelMessage.Read.All` and `User.Read.All` commonly require admin consent in organizational tenants.

When permissions or feature flags change, remove the cached `token.json` from the platform cache directory and restart `teams` to perform a fresh device-code login.

## Configuration

The app creates `config.json` with all defaults on first run.

| Platform | Config directory | Cache directory |
| --- | --- | --- |
| macOS | `~/Library/Application Support/teams-tui-go/` | `~/Library/Caches/teams-tui-go/` |
| Linux | `${XDG_CONFIG_HOME:-~/.config}/teams-tui-go/` | `${XDG_CACHE_HOME:-~/.cache}/teams-tui-go/` |

Environment variables `CLIENT_ID` and `TENANT_ID` override `config.json`. A project-local `.env` file is also loaded.

### Complete Example

```json
{
  "client_id": "YOUR-APPLICATION-CLIENT-ID",
  "tenant_id": "YOUR-DIRECTORY-TENANT-ID",
  "notification_mode": "System",
  "notification_show_preview": true,
  "notification_preview_len": 120,
  "message_limit": 50,
  "search_context_limit": 3,
  "chat_limit": 50,
  "chat_icon_theme": "unicode",
  "file_preview_enabled": true,
  "file_preview_in_terminal": true,
  "file_upload_enabled": true,
  "presence_enabled": true,
  "user_profile_enabled": true,
  "user_profile_extended": false,
  "teams_channels_enabled": true,
  "channel_mentions_enabled": false,
  "channel_msg_refresh_min": 2,
  "sqlite_enabled": false,
  "external_editor": "",
  "browser_command": "open"
}
```

Use `"browser_command": "open"` on macOS and `"browser_command": "xdg-open"` on Linux. Optional `youtrack_command` and `gitlab_command` values can route those URLs to dedicated terminal clients; otherwise they use `browser_command`.

### General Settings

| Key | Default | Notes |
| --- | --- | --- |
| `notification_mode` | `None` | `None`, `Console`, `System`, or `Both`; press `n` to cycle and persist it |
| `notification_show_preview` | `false` | Include message text in notifications |
| `notification_preview_len` | `50` | Maximum preview length |
| `message_limit` | `50` | Initial messages per conversation; capped at 200 |
| `chat_limit` | `50` | Sidebar chats; capped at 100 |
| `search_context_limit` | `3` | Messages shown before and after a search hit |
| `channel_msg_refresh_min` | `2` | Background refresh interval for unhidden channels |
| `sqlite_enabled` | `false` | Persist message history and pagination state locally |
| `external_editor` | empty | Falls back to `$EDITOR`, then `$VISUAL`, then `vim` |
| `chat_icon_theme` | `unicode` | `unicode`, `emoji`, or `text` |

Custom sidebar icons can be supplied with `custom_chat_icons`:

```json
{
  "custom_chat_icons": {
    "oneOnOne": "DM",
    "group": "GR",
    "meeting": "MT",
    "channel": "#",
    "default": "*"
  }
}
```

## cmux

When launched inside cmux, `System` and `Both` notification modes use `cmux notify` automatically. The integration detects `CMUX_BUNDLED_CLI_PATH`, or `CMUX_SOCKET_PATH` plus a `cmux` executable on `PATH`. This gives cmux sidebar unread state, workspace navigation, and focused-surface suppression.

Enable it in `config.json`:

```json
{
  "notification_mode": "System",
  "notification_show_preview": true,
  "notification_preview_len": 120
}
```

You can verify cmux independently:

```bash
cmux notify --title "Microsoft Teams" --subtitle "teams" --body "Notification test"
```

Chat metadata refreshes every 15 seconds. The active focused conversation refreshes about every 3 seconds. Unhidden channels refresh using `channel_msg_refresh_min`.

## Keyboard Reference

Press `?` in the app for the contextual help popup.

### Conversation Mode

| Key | Action |
| --- | --- |
| `j` / `k`, arrows | Move through chats or channels |
| `g` / `G` | Jump to first or last item |
| `Enter` | Load the highlighted conversation immediately |
| `Tab` | Switch between chats and channels |
| `K` / `J`, `PgUp` / `PgDn` | Scroll the timeline |
| `Ctrl+u` / `Ctrl+d` | Scroll half a page |
| `m` | Enter message-selection mode |
| `z` | Expand or collapse the message near the viewport |
| `i` | Compose a message |
| `/` | Search message history |
| `c` | Find a local chat or open a user by email/UPN |
| `s` | Open Saved Messages, the chat with yourself |
| `C` / `V` | Open a Teams audio or video call for the selected chat |
| `v` | Preview the newest image in loaded messages |
| `f` | Toggle favourite for a chat |
| `p` | Show participant presence for a chat |
| `h` / `?` | Help; `h` hides/unhides a selected channel |
| `n` | Cycle notification mode |
| `Esc` | Clear highlighting or enter sleep mode |
| `q` / `Ctrl+c` | Quit |

### Message-Selection Mode

| Key | Action |
| --- | --- |
| `j` / `k` | Select newer or older messages |
| `g` / `G` | Jump to oldest or newest loaded message |
| `z` / `Enter` | Expand or collapse the selected message |
| `v` | Open full message and attachment view |
| `a` | Reply to the selected message or channel thread |
| `r`, then `1`-`6` | Add a reaction |
| `y` | Copy rendered message text |
| `u` | Copy a URL; includes Adaptive Card links |
| `o` | Open a URL; includes Adaptive Card links |
| `e` / `d` | Edit or delete your own message |
| `p` | Show sender presence |
| `i` | Show sender profile |
| `Ctrl+g` | Open the message read-only in the external editor |
| `m` / `Esc` | Return to conversation mode |

### Compose Mode

| Key | Action |
| --- | --- |
| `Enter` | Send |
| `Alt+Enter` | Insert a newline |
| `@` | Open mention autocomplete |
| `Ctrl+v` | Read and attach PNG/JPEG clipboard data |
| `Cmd+v` | Attach a cmux/native-terminal pasted image path when supplied by the terminal |
| `Ctrl+f` | Browse and attach a local file |
| `Ctrl+g` | Compose in the external editor |
| `Esc` | Cancel compose |

## Message Rendering

### Long Messages

Messages longer than eight rendered lines collapse automatically to keep navigation fast. Press `z` to expand or collapse them. Adaptive Cards start expanded so their fields are visible immediately.

### Adaptive Cards

Workflow and bot messages using `application/vnd.microsoft.card.adaptive` render directly in the timeline instead of appearing as blank timestamp rows. Supported presentation elements include:

- `TextBlock` and `RichTextBlock`
- `FactSet` with aligned labels and values
- Containers, columns, separators, and images represented by labels
- Markdown links and `Action.OpenUrl`

Select the card with `m`, then press `o` to open its action or `u` to copy the URL.

### Markdown

Outgoing Markdown is converted to Teams-compatible HTML. Incoming HTML is rendered with terminal styles.

| Input | Result |
| --- | --- |
| `**bold**` | Bold |
| `*italic*` | Italic |
| `~~strike~~` | Strikethrough |
| `` `code` `` | Inline code |
| `- item` or `* item` | Bullet list |
| `1. item` | Numbered list |
| Triple-backtick fence | Multiline code block |

Editing your own message restores Markdown-like source rather than flattened terminal text.

## Images and Attachments

### Clipboard Images

Enter compose mode with `i`, then paste a PNG or JPEG. The app inserts an `[Image N]` placeholder and attaches the image when the message is sent. Removing the placeholder removes that pending image.

On cmux/macOS, normal `Cmd+v` works when cmux sends a temporary image filepath. `Ctrl+v` also reads image data directly from the system clipboard. Linux clipboard support uses `wl-paste` or `xclip` when available.

### Timeline Images

With both `file_preview_enabled` and `file_preview_in_terminal` enabled, image attachments download into the cache and render automatically in the timeline on Kitty-compatible terminals. Press `v` for a larger view.

If only a filename appears, verify the feature flags, `Files.Read` permission, fresh authentication, and terminal Kitty Graphics support.

### Files

Press `Ctrl+f` while composing to attach a file up to 50 MB. Chat files upload to the OneDrive `Microsoft Teams Chat Files` folder; channel files upload to the channel SharePoint folder. Files above 4 MB use a resumable upload session automatically.

To download, select a message with `m`, open it with `v`, press `Tab` to focus attachments, and press `Enter`. Downloads are saved to the platform Downloads directory.

## Saved Messages

Press `s` to locate and open the existing one-to-one conversation whose only user sender is the signed-in account. It is labeled `Saved messages` instead of `Unknown`.

If the app reports that Saved Messages was not found, open Microsoft Teams once, start a chat with yourself, send a message, and restart `teams`. Microsoft Graph rejects attempts to programmatically create a one-to-one chat with duplicate members.

## Calls

Press uppercase `C` for audio or uppercase `V` for video while a chat is selected. The app builds an official `teams.microsoft.com/l/call` link from the selected chat members and opens it using `browser_command`. Teams asks for confirmation before starting the call.

Calls are not available for channels or conversations without callable member email addresses.

## Troubleshooting

### `AADSTS65002`

Do not use the upstream Microsoft first-party client ID. Create your own Entra app registration, enable public client flow, set your client and tenant IDs, remove the cached token, and authenticate again.

### Permissions Changed but the Feature Still Fails

Remove `token.json` from the cache directory shown above and restart. Existing access and refresh tokens do not gain newly configured scopes automatically.

### Adaptive Cards Show Only Dates

Update this fork and rebuild `teams`. Older builds removed all card attachments before rendering them.

### No cmux Notification

Set `notification_mode` to `System` or `Both`, leave `teams` running, and verify `cmux notify` works in the same surface. `Console` mode only emits a terminal bell and visual flash.

### Images Show Only Filenames

Enable both preview flags, grant `Files.Read`, re-authenticate, and use a Kitty-compatible terminal. The attachment remains downloadable even when the terminal cannot draw it inline.

### Calls Do Not Open on macOS

Set `"browser_command": "open"`. Linux normally uses `xdg-open`.

### Bangla or Unicode Layout Looks Wrong

Use an up-to-date terminal and a font with Bengali glyph coverage. The TUI uses grapheme-aware cell widths, but font fallback still determines glyph shaping.

## Local Files

| File | Purpose |
| --- | --- |
| `config.json` in the config directory | Account, features, limits, notifications, and commands |
| `favourites.json` in the config directory | Pinned chat IDs |
| `unhidden_channels.json` in the config directory | Channel visibility state |
| `filepicker_settings.json` in the config directory | Last file browser directory and sorting |
| `token.json` in the cache directory | OAuth access and refresh tokens; mode `0600` |
| `profile.json` in the cache directory | Cached signed-in profile |
| `teams-tui-go.db` in the cache directory | Optional SQLite message history |
| `previews/` in the cache directory | Downloaded terminal image previews |

## Development

```bash
go test ./...
go test -race ./...
go vet ./...
go build -o bin/teams .
```

The GitHub Actions test workflow also builds and tests the project. Keep changes formatted with `gofmt` and add focused tests for Graph payload parsing, terminal-width behavior, and key-driven state transitions.

## License

See [LICENSE](LICENSE).
