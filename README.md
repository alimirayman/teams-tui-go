# teams-tui-go

A keyboard-driven terminal UI client for Microsoft Teams, written in Go.

Authenticates via **OAuth2 Device Code Flow** (no browser redirect needed), fetches your chats and messages from the **Microsoft Graph API**, and displays them in a fast, minimal TUI.

---

## Features

- 🔐 OAuth2 Device Code Flow — authenticate with your Microsoft account, no browser redirect required
- 💬 List all your Teams chats (1:1, group, meetings) with computed display names
- 📨 View messages in any chat with HTML-to-text rendering (images, attachments, emoji, **bold**, *italic*, ~~strikethrough~~, `code`, lists)
- ❤️ Message Interactions — view and add reactions (Heart, Like, Laugh, etc.) to any message
- 🔗 Clickable & Extractable URLs — links are clickable in supported terminals and can be extracted/copied via the `u` key
- ✏️ Message Management — send, edit, and delete messages (includes multi-line support)
- **✍️ Markdown Formatting** — compose messages with `**bold**`, `*italic*`, ~~`~~strike~~`~~, `` `code` ``, fenced code blocks, and bullet/ordered lists; formatting is sent as rich HTML to all Teams clients and rendered with ANSI styles in the TUI
- 🔔 Notification modes: None / Console (BEL + visual bell) / System (desktop) / Both
- 🔄 Background polling — chats and messages refresh automatically every ~3 s
- 😊 Emoticon Auto-replacement — popular text emoticons (like `:)`, `:D`, `<3`) are automatically converted to Unicode emojis
- 🔍 Search History — search messages in any chat, recursively loading and indexing all conversation history in the background
- 🔍 Chat Search & Open — filter locally loaded chats or open/start a 1:1 chat directly by entering a UPN/email (bypassing directory search)
- ⭐ Favourites — pin any chat to the top of the sidebar with `f`; favourites are sorted alphabetically and stay anchored regardless of activity

- 🔵 Unread Indicators — chats with new messages are marked with a dot (●) and bold text
- 😊 Reaction Indicators — chats with new reactions from other users are marked with their corresponding emoji (e.g. ❤️, 👍, 😆) and bold text
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
  - `message_limit`: The number of messages to fetch (default: 50). For limits greater than 50, the app automatically makes sequential paginated requests. Capped at `200` to prevent excessive API requests.

### Chat Limit
Configure how many chats to load in the sidebar in `~/.config/teams-tui-go/config.json`:

  ```json
  {
    "chat_limit": 50
  }
  ```
  - `chat_limit`: The maximum number of chats to fetch and display (default: 50). Automatically makes paginated requests if needed. Capped at `100` to prevent API rate-limiting during member loading.

### Search Context Limit
Configure how many context messages (before and after each search match) to display in the search history popup in `~/.config/teams-tui-go/config.json`:

  ```json
  {
    "search_context_limit": 3
  }
  ```
  - `search_context_limit`: The number of context messages before and after each match to include (default: 3).

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

## Markdown Formatting

Messages support a subset of markdown that is converted to rich HTML when sent, so recipients on **any Teams client** (Desktop, Web, Mobile) see proper formatting.

### Inline syntax

| Syntax                   | Result     |
| ------------------------ | ---------- |
| `**bold**` or `__bold__` | **Bold**   |
| `*italic*` or `_italic_` | *Italic*   |
| `~~strikethrough~~`      | ~~Strike~~ |
| `` `inline code` ``      | `code`     |

### Block syntax

| Syntax                        | Result                  |
| ----------------------------- | ----------------------- |
| `* item` or `- item`          | Unordered (bullet) list |
| `1. item` or `1) item`        | Ordered (numbered) list |
| ` ``` ` fence on its own line | Multi-line code block   |

**Example:**

````
**Meeting notes** for *Project X*

* Review PR #42
* Deploy to staging

```
fmt.Println("hello")
```
````

> **Note:** Language hints (e.g. ` ```go `) are accepted syntax but have no visible effect — Teams strips the `class` attribute from the stored HTML, so the hint is not preserved or displayed.

### Receive side rendering

Incoming messages from all clients are rendered with matching ANSI styles in the TUI:

- Bold, italic, and strikethrough use terminal text attributes
- Inline `code` is highlighted in amber
- Code blocks are highlighted in green
- Bullet and numbered lists show `•` / `1.` prefixes

### Edit round-trip

When you press `e` to edit an existing message the edit box is pre-filled with the **original markdown source** (e.g. `**bold**` rather than stripped plain text), so formatting is preserved after saving.

---

## Keyboard Controls

| Key          | Action                                                    |
| ------------ | --------------------------------------------------------- |
| `↑` / `k`    | Move up in chat list                                      |
| `↓` / `j`    | Move down in chat list                                    |
| `PgUp` / `K` | Scroll messages up                                        |
| `PgDn` / `J` | Scroll messages down                                      |
| `/`          | Open search input (in Normal Mode)                        |
| `Esc`        | Clear active search (in Normal Mode)                      |
| `c`          | Open chat search / chat creation popup                    |
| `f`          | Toggle ★ favourite on selected chat                       |
| `i`          | Enter compose mode                                        |
| `Enter`      | Send message                                              |
| `Alt+Enter`  | New line in message                                       |
| `Esc`        | Cancel compose                                            |
| `n`          | Toggle notification mode                                  |
| `m`          | Enter/Exit **Message Mode** (to select/react/delete/copy) |
| `v`          | View details/reactions of selected message (in Message Mode) |
| `r`          | React to selected message (in Message Mode)               |
| `y`          | Copy (yank) message text (in Message Mode)                |
| `u`          | Copy (yank) URL from message (in Message Mode)            |
| `d`          | Delete selected message (in Message Mode)                 |
| `e`          | Edit selected message (in Message Mode)                   |
| `a`          | Answer (reply) to selected message (in Message Mode)      |
| `1-6`        | Send reaction (in Reaction Mode)                          |
| `q`          | Quit                                                      |

---

## File Locations

| File                                          | Purpose                             |
| --------------------------------------------- | ----------------------------------- |
| `~/.config/teams-tui-go/config.json`           | Client ID, notification mode, limits |
| `~/.config/teams-tui-go/favourites.json`       | Pinned/favourite chat IDs           |
| `~/.cache/teams-tui-go/token.json`             | OAuth2 access + refresh tokens      |
| `~/.cache/teams-tui-go/profile.json`           | Cached user profile                 |

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
