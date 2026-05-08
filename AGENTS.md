# AI Agent Instructions

## Project Overview

Go-based terminal UI application for Microsoft Teams. Authenticates via OAuth2 Device Code Flow and displays chats and messages using the Microsoft Graph API. Built with the Bubble Tea TUI framework (MVU architecture).

---

## Key Architecture

### Authentication (`auth.go`)
- OAuth2 Device Code Flow with Microsoft Graph API
- Tokens stored in `~/.cache/teams-tui-go/token.json`
- Auto-refreshes expired tokens using `GetValidTokenSilent(clientID)`
- Client ID loaded in order: `.env` → `config.json` → built-in default
- **All background API calls must use `GetValidTokenSilent()`**, never the cached `accessToken` from startup

### Configuration (`config.go`)
- App data: `~/.config/teams-tui-go/` (via `GetAppDir()`)
- Cache: `~/.cache/teams-tui-go/` (via `GetCacheDir()`)
- Config struct: `ClientID *string`, `NotificationMode *NotificationMode`, `NotificationShowPreview *bool`, `NotificationPreviewLen *int`
- `ResolveClientID()` implements the full precedence chain

### API Layer (`api.go`)
- **User Detection**: Identifies the current user by counting name frequency across `oneOnOne` chats
- **Display Names**: Pre-computed in `GetChats()` and stored in `Chat.CachedDisplayName` — **never compute display names in the UI layer**
- **Name Abbreviation**: Group chat members shown as "FirstName LastInitial" (`abbreviateName()`)
- **Filtering**: Current user is automatically filtered from all member lists by name match (not by ID — IDs vary per chat)
- **HTMLToText**: Uses `golang.org/x/net/html` tokenizer for robust HTML-to-text conversion, handling `<img>`, `<attachment>`, `<emoji>`, block elements, HTML entities
- **Read State**: `Chat` includes `Viewpoint` containing `LastMessageReadDateTime` from the server
- **Silent errors**: `GetChatMembers()` returns empty slice on error; `MarkChatAsRead()` silently ignores all errors

### Application State (`app.go`)
- `App` struct holds all runtime state: chats, messages, selection, input mode, notification mode, etc.
- `NotificationMode` enum is JSON-serialised as a string ("None", "Console", "System", "Both")
- `CurrentUserName` is used for filtering and message alignment; it is **not displayed in the UI**

### UI (`ui.go`)
- Bubble Tea `Model` struct implementing `Init()`, `Update()`, `View()`
- Layout: 30% chat list (left) | 70% messages (right) | status bar (3 lines, bottom)
- Uses `CachedDisplayName` from `Chat` struct — **do not compute display names here**
- Stable chat ordering maintained in `stableChatOrder []string` (list of chat IDs)
  - Only changes when a new message arrives (chat → position 0) or a brand-new chat is added
  - On every API refresh the display list is **rebuilt** from `stableChatOrder`
- **Read Logic**:
  - `lastMsgID` and `lastMsgTime` track latest content
  - `lastReadMsgID` tracks what was read locally in this session
  - `isUnread(chat)` compares latest message time with server viewpoint and local read state
  - `markRead()` triggers `MarkChatAsRead` API on focus, selection change, or key press
- **Focus Tracking**: Terminal focus reporting enabled via `\x1b[?1004h`; `tea.FocusMsg`/`BlurMsg` update `focused` state
- Background tasks issued as Bubble Tea `Cmd` functions returning typed messages (`MsgChatsLoaded`, `MsgMessagesLoaded`, `MsgNewMessage`, `MsgTick`, `MsgSendDone`)

### Main / Entry Point (`main.go`)
- Startup: banner → auth → profile → chats → concurrent initial message fetch for sort → sort → init model → run
- All Bubble Tea commands (async API calls) are defined here
- Initial chat order computed concurrently in `loadInitialChatOrder()` using `sync.WaitGroup`

---

## Important Patterns

1. **Display Names**: Always pre-compute in `api.go → GetChats()` and read from `CachedDisplayName`. Never compute in `ui.go`.
2. **User Filtering**: Done by name matching, not ID. IDs are per-chat base64 strings that vary.
3. **Background Calls**: All API calls in the event loop use `loadMessagesCmd()`, `loadChatsCmd()`, etc. — they return `tea.Cmd` functions, never block.
4. **Token Refresh**: Use `GetValidTokenSilent(clientID)` in all `tea.Cmd` functions, not the startup access token.
5. **No Debug Output**: No `fmt.Printf` / `log.Printf` in production code paths.
6. **Message Order**: API returns messages newest-first. The UI iterates in reverse to display newest at the bottom.
7. **Stable Order**: `stableChatOrder` is the source of truth for display order. It is only mutated by `promoteChat()` and `mergeChats()`.
8. **Silent Failures**: Background refresh errors return `nil` from `tea.Cmd` functions (Bubble Tea ignores nil messages).

---

## Common Tasks

### Adding a new Graph API call
1. Add the function to `api.go`, using `graphGet()` or `graphPost()` helpers.
2. Create a `tea.Cmd` wrapper in `main.go` (e.g. `loadXxxCmd()`).
3. Define a typed message struct in `ui.go` (e.g. `MsgXxxLoaded`).
4. Handle the message in `Model.Update()` in `ui.go`.

### Adding UI state
1. Add the field to `App` struct in `app.go`.
2. Add a setter method to `App` if needed.
3. Reference it in `Model.View()` in `ui.go`.
4. Update via `Model.Update()` when a relevant message arrives.

### Changing the layout
- Panel widths: `chatPanelWidth()` / `msgPanelWidth()` in `ui.go`.
- Status bar height is hardcoded to 3 lines in `Model.View()`.
- Use `lipgloss` styles for borders and colours; the palette is at the top of `ui.go`.

---

## Documentation Maintenance

When making significant changes:
1. Update `README.md` with new features, usage instructions, or requirements.
2. Update `AZURE_SETUP.md` if API permission requirements change.
3. Update this `AGENTS.md` with architectural changes or new patterns.
