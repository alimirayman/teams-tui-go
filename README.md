# teams-tui-go

A keyboard-driven terminal UI client for Microsoft Teams, written in Go.

Authenticates via **OAuth2 Device Code Flow** (no browser redirect needed), fetches your chats and messages from the **Microsoft Graph API**, and displays them in a fast, minimal TUI.

---

## Features

- 🔐 OAuth2 Device Code Flow — authenticate with your Microsoft account, no browser redirect required
- 💬 List all your Teams chats (1:1, group, meetings) with computed display names
- 📨 View messages in any chat with HTML-to-text rendering (images, attachments, emoji)
- ✏️ Send messages, including multiline via Alt+Enter
- 🔔 Notification modes: None / Console (BEL + visual bell) / System (desktop) / Both
- 🔄 Background polling — chats and messages refresh automatically every ~3 s
- ❤️ Message Interactions — view and add reactions (Heart, Like, Laugh, etc.) to any message
- 🔵 Unread Indicators — chats with new messages are marked with a dot (●) and bold text
- ⬆️ New messages bubble chats to the top of the list
- 📌 Stable chat ordering — order only changes when new messages arrive
- 💾 Token persistence — only authenticate once; tokens refresh automatically

---

## Installation

### Prerequisites

- Go 1.22 or later
- A Microsoft account with access to Microsoft Teams

### Build

```bash
git clone https://github.com/nospor/teams-tui-go
cd teams-tui-go
go build -o teams-tui-go .

# or (builds slower, but binary is smaller)
go build -trimpath -ldflags="-s -w" -o teams-tui-go .

# then run
./teams-tui-go

# you may also want to copy the binary to your PATH (and run it from any place), e.g.:
sudo cp teams-tui-go /usr/local/bin/
```

### Install to PATH

```bash
go install github.com/nospor/teams-tui-go@latest
```

---

## Configuration

### Client ID (optional)

By default the app uses Microsoft's public Teams client ID. To use your own Azure AD app registration:

1. Follow the instructions in [AZURE_SETUP.md](AZURE_SETUP.md).
2. Set your client ID using one of:

   **Option A — environment variable:**
   ```bash
   cp .env.example .env
   # Edit .env and set CLIENT_ID=<your-client-id>
   ```

   **Option B — config file** (`~/.config/teams-tui-go/config.json`):
   ```json
   {
     "client_id": "your-client-id-here"
   }
   ```

### Notifications
- **Toggle Mode**: Cycle through notification modes at runtime by pressing `n`. The chosen mode is automatically saved.
- **Message Previews**: Configure desktop notifications in `~/.config/teams-tui-go/config.json`:

  ```json
  {
    "notification_mode": "System",
    "notification_show_preview": true,
    "notification_preview_len": 80
  }
  ```
  - `notification_show_preview`: Set to `true` to include the message content in the desktop notification.
  - `notification_preview_len`: The maximum number of characters to show in the preview.

### Message Limit
Configure how many messages to fetch when opening a chat in `~/.config/teams-tui-go/config.json`:

  ```json
  {
    "message_limit": 50
  }
  ```
  - `message_limit`: The number of messages to fetch (default: 50).

---

## Usage

```bash
# Run directly
./teams-tui-go

# Or if installed
teams-tui-go
```

On first run (or after token expiry) you will be prompted to visit a URL and enter a short code. Subsequent runs use the cached token (auto-refreshed).

---

## Keyboard Controls

| Key          | Action                                               |
| ------------ | ---------------------------------------------------- |
| `↑` / `k`    | Move up in chat list                                 |
| `↓` / `j`    | Move down in chat list                               |
| `PgUp` / `K` | Scroll messages up                                   |
| `PgDn` / `J` | Scroll messages down                                 |
| `i`          | Enter compose mode                                   |
| `Enter`      | Send message                                         |
| `Alt+Enter`  | New line in message                                  |
| `Esc`        | Cancel compose                                       |
| `n`          | Toggle notification mode                             |
| `m`          | Enter/Exit **Message Mode** (to select/react/delete) |
| `r`          | React to selected message (in Message Mode)          |
| `d`          | Delete selected message (in Message Mode)            |
| `1-6`        | Send reaction (in Reaction Mode)                     |
| `q`          | Quit                                                 |

---

## File Locations

| File                                 | Purpose                        |
| ------------------------------------ | ------------------------------ |
| `~/.config/teams-tui-go/config.json` | Client ID, notification mode   |
| `~/.cache/teams-tui-go/token.json`   | OAuth2 access + refresh tokens |
| `~/.cache/teams-tui-go/profile.json` | Cached user profile            |

---

## Development

```bash
# Run in development
go run .

# Build binary
go build -o teams-tui-go .

# Lint
go vet ./...
```

---

## License

See [LICENSE](LICENSE).

## Thanks For Visiting
Hope you liked it. Wanna **[buy Me a coffee](https://www.buymeacoffee.com/nospor)**?
