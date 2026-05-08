package main

import (
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/gen2brain/beeep"
)

// ---------------------------------------------------------------------------
// Bubble Tea commands (async → messages)
// ---------------------------------------------------------------------------

// tickCmd returns a command that fires MsgTick after 100ms.
func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return MsgTick{}
	})
}

// loadChatsCmd fetches the chat list in the background.
func loadChatsCmd(clientID string) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return nil // silently ignore background refresh errors
		}
		chats, currentUser, err := GetChats(token)
		if err != nil {
			return nil
		}
		return MsgChatsLoaded{Chats: chats, CurrentUserName: currentUser}
	}
}

// loadMessagesCmd fetches messages for a specific chat in the background.
func loadMessagesCmd(clientID, chatID string, chatIndex int) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return nil
		}
		msgs, err := GetMessages(token, chatID)
		if err != nil {
			return nil
		}
		return MsgMessagesLoaded{ChatIndex: chatIndex, Messages: msgs}
	}
}

// checkNewMessageCmd fetches the latest message for a non-selected chat.
func checkNewMessageCmd(clientID, chatID string) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return nil
		}
		msgs, err := GetMessages(token, chatID)
		if err != nil || len(msgs) == 0 {
			return nil
		}
		return MsgNewMessage{ChatID: chatID, Message: msgs[0]}
	}
}

// sendMessageCmd sends a message to a chat in the background.
func sendMessageCmd(clientID, chatID, content string) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return MsgSendDone{Err: err}
		}
		err = SendMessage(token, chatID, content)
		return MsgSendDone{Err: err}
	}
}

// ---------------------------------------------------------------------------
// Desktop notification
// ---------------------------------------------------------------------------

// sendDesktopNotification sends a native desktop notification.
func sendDesktopNotification(senderName string, body string) {
	title := "TeamsTUI: New Message"
	if senderName != "" {
		title = "TeamsTUI: " + senderName
	}
	
	finalBody := "New message received"
	if body != "" {
		finalBody = body
	}
	
	beeep.AppName = "TeamsTUI"
	_ = beeep.Notify(title, finalBody, "")
}

// ---------------------------------------------------------------------------
// Initial chat sort by most recent message
// ---------------------------------------------------------------------------

// chatTimestamp holds a chat together with the most recent message timestamp
// observed during the initial load.
type chatTimestamp struct {
	chat      Chat
	latestMsg time.Time
}

// loadInitialChatOrder concurrently fetches the last message for each chat
// and returns the chats sorted by most recent message timestamp (descending),
// along with the last message IDs and timestamps.
func loadInitialChatOrder(accessToken string, chats []Chat) ([]Chat, map[string]string, map[string]time.Time) {
	type result struct {
		index     int
		latestMsg time.Time
		lastMsgID string
	}

	results := make([]result, len(chats))
	var wg sync.WaitGroup

	for i, c := range chats {
		wg.Add(1)
		go func(i int, c Chat) {
			defer wg.Done()
			msgs, err := GetMessages(accessToken, c.ID)
			if err != nil || len(msgs) == 0 {
				// Fallback: use lastUpdatedDateTime.
				t := time.Time{}
				if c.LastUpdated != nil {
					t, _ = time.Parse(time.RFC3339Nano, *c.LastUpdated)
				}
				results[i] = result{i, t, ""}
				return
			}
			// API returns newest first; use the first element.
			latest, _ := time.Parse(time.RFC3339Nano, msgs[0].CreatedDateTime)
			
			// If LastUpdated is newer, use it for sorting (e.g. chat renamed, members changed)
			t := time.Time{}
			if c.LastUpdated != nil {
				t, _ = time.Parse(time.RFC3339Nano, *c.LastUpdated)
			}
			if t.After(latest) {
				latest = t
			}

			results[i] = result{i, latest, msgs[0].ID}
		}(i, c)
	}
	wg.Wait()

	lastMsgIDs := make(map[string]string)
	lastMsgTimes := make(map[string]time.Time)
	for i, c := range chats {
		if results[i].lastMsgID != "" {
			lastMsgIDs[c.ID] = results[i].lastMsgID
		}
		if !results[i].latestMsg.IsZero() {
			lastMsgTimes[c.ID] = results[i].latestMsg
		}
	}

	type chatWithResult struct {
		chat Chat
		res  result
	}
	combined := make([]chatWithResult, len(chats))
	for i, c := range chats {
		combined[i] = chatWithResult{c, results[i]}
	}

	// Sort by latest message timestamp descending.
	sort.Slice(combined, func(a, b int) bool {
		ta := combined[a].res.latestMsg
		tb := combined[b].res.latestMsg
		if ta.IsZero() && tb.IsZero() {
			return false
		}
		if ta.IsZero() {
			return false
		}
		if tb.IsZero() {
			return true
		}
		return ta.After(tb)
	})

	for i, cw := range combined {
		chats[i] = cw.chat
	}

	return chats, lastMsgIDs, lastMsgTimes
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	// 1. Banner.
	fmt.Println("TeamsTUI")
	fmt.Println("================================")

	// 2. Resolve client ID and authenticate.
	clientID := ResolveClientID()
	accessToken, err := GetAccessToken(clientID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "authentication error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ Authentication successful!")

	// 3. Fetch user profile.
	me, err := GetMe(accessToken)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not fetch profile: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Logged in as: %s\n", me.DisplayName)

	// 4. Fetch initial chat list.
	chats, currentUserName, err := GetChats(accessToken)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not fetch chats: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Loaded %d chats\n", len(chats))

	// 5 & 6. Sort chats by most recent message.
	var lastMsgIDs map[string]string
	var lastMsgTimes map[string]time.Time
	chats, lastMsgIDs, lastMsgTimes = loadInitialChatOrder(accessToken, chats)

	// 7 & 8. Initialise application state.
	app := NewApp()
	app.SetChats(chats)
	if currentUserName != nil {
		app.SetCurrentUser(*currentUserName)
	}
	app.CurrentUserID = me.ID

	// Load persisted notification mode and settings.
	if cfg := LoadConfig(); cfg != nil {
		if cfg.NotificationMode != nil {
			app.NotificationMode = *cfg.NotificationMode
		}
		if cfg.NotificationShowPreview != nil {
			app.NotificationShowPreview = *cfg.NotificationShowPreview
		}
		if cfg.NotificationPreviewLen != nil {
			app.NotificationPreviewLen = *cfg.NotificationPreviewLen
		}
	}

	// Build initial stable chat order.
	model := NewModel(app, clientID, me.ID)
	model.latestChats = chats
	model.lastMsgID = lastMsgIDs
	model.lastMsgTime = lastMsgTimes
	stableOrder := make([]string, len(chats))
	for i, c := range chats {
		stableOrder[i] = c.ID
	}
	model.stableChatOrder = stableOrder

	// 9. Start Bubble Tea program.
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}
}
