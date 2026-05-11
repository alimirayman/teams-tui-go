package main

import (
	"encoding/json"
	"fmt"
	"time"
)

// NotificationMode controls how the app notifies the user of new messages.
type NotificationMode int

const (
	NotificationNone    NotificationMode = iota // 0 — no notifications
	NotificationConsole                         // 1 — BEL + visual bell
	NotificationSystem                          // 2 — desktop notification
	NotificationBoth                            // 3 — BEL + visual bell + desktop
)

// String returns the human-readable name for a NotificationMode.
func (n NotificationMode) String() string {
	switch n {
	case NotificationConsole:
		return "Console"
	case NotificationSystem:
		return "System"
	case NotificationBoth:
		return "Both"
	default:
		return "None"
	}
}

// MarshalJSON serialises NotificationMode as a string so config.json is readable.
func (n NotificationMode) MarshalJSON() ([]byte, error) {
	return json.Marshal(n.String())
}

// UnmarshalJSON parses a NotificationMode from its string representation.
func (n *NotificationMode) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		// Also accept integer representation for backward compatibility.
		var i int
		if err2 := json.Unmarshal(data, &i); err2 != nil {
			return err
		}
		*n = NotificationMode(i)
		return nil
	}
	switch s {
	case "Console":
		*n = NotificationConsole
	case "System":
		*n = NotificationSystem
	case "Both":
		*n = NotificationBoth
	default:
		*n = NotificationNone
	}
	return nil
}

// ---------------------------------------------------------------------------
// App — central application state
// ---------------------------------------------------------------------------

// App holds all runtime state for the Teams TUI application.
type App struct {
	Chats           []Chat
	Status          string
	SelectedIndex   int
	CurrentUserName *string
	CurrentUserID   string // used for markChatRead
	Messages        []Message
	LoadingMessages bool
	InputMode       bool
	InputBuffer     string
	ScrollOffset    int
	MaxScroll       int
	ChatScrollOffset int
	SnapToBottom    bool
	MessageSelectedIndex int
	MessageSelectionMode bool
	ReactionMode         bool
	DeleteConfirmMode    bool
	NotificationMode NotificationMode
	NotificationShowPreview bool
	NotificationPreviewLen  int
	VisualBellUntil *time.Time
	StatusUntil     *time.Time
}

// NewApp creates an App with sensible initial defaults.
func NewApp() *App {
	return &App{
		Status:           "Loading...",
		SnapToBottom:            true,
		NotificationMode:        NotificationNone,
		NotificationShowPreview: false,
		NotificationPreviewLen:  50,
	}
}

// SetChats replaces the chat list and updates the status line.
func (a *App) SetChats(chats []Chat) {
	a.Chats = chats
	a.SetStatus(fmt.Sprintf("Loaded %d chats", len(chats)), 5*time.Second)
}

// SetCurrentUser records the detected current user's display name.
func (a *App) SetCurrentUser(name string) {
	a.CurrentUserName = &name
}

// SetMessages replaces the current message list and clears the loading flag.
func (a *App) SetMessages(messages []Message) {
	oldMsgs := a.Messages
	a.Messages = messages
	a.LoadingMessages = false

	// Maintain selection by ID if in message selection mode.
	if a.MessageSelectionMode && len(oldMsgs) > 0 && a.MessageSelectedIndex < len(oldMsgs) {
		selectedID := oldMsgs[a.MessageSelectedIndex].ID
		for i, m := range messages {
			if m.ID == selectedID {
				a.MessageSelectedIndex = i
				return
			}
		}
	}

	// Clamp index if message was deleted or we're out of bounds.
	if a.MessageSelectedIndex >= len(messages) {
		if len(messages) > 0 {
			a.MessageSelectedIndex = len(messages) - 1
		} else {
			a.MessageSelectedIndex = 0
		}
	}
}

// SetLoadingMessages toggles the loading indicator.
func (a *App) SetLoadingMessages(loading bool) {
	a.LoadingMessages = loading
}

// GetSelectedChat returns the currently highlighted chat, or nil.
func (a *App) GetSelectedChat() *Chat {
	if len(a.Chats) == 0 || a.SelectedIndex < 0 || a.SelectedIndex >= len(a.Chats) {
		return nil
	}
	return &a.Chats[a.SelectedIndex]
}

// NextChat moves the selection one step down, wrapping around.
func (a *App) NextChat() {
	if len(a.Chats) == 0 {
		return
	}
	a.SelectedIndex = (a.SelectedIndex + 1) % len(a.Chats)
}

// PreviousChat moves the selection one step up, wrapping around.
func (a *App) PreviousChat() {
	if len(a.Chats) == 0 {
		return
	}
	a.SelectedIndex = (a.SelectedIndex - 1 + len(a.Chats)) % len(a.Chats)
}

// ToggleNotificationMode cycles None → Console → System → Both → None.
func (a *App) ToggleNotificationMode() {
	a.NotificationMode = (a.NotificationMode + 1) % 4
}

// TriggerVisualBell sets VisualBellUntil to 200 ms from now.
func (a *App) TriggerVisualBell() {
	t := time.Now().Add(200 * time.Millisecond)
	a.VisualBellUntil = &t
}

// VisualBellActive reports whether the visual bell should be showing.
func (a *App) VisualBellActive() bool {
	return a.VisualBellUntil != nil && time.Now().Before(*a.VisualBellUntil)
}

// SetStatus sets the status line, optionally clearing it after duration.
func (a *App) SetStatus(msg string, duration time.Duration) {
	a.Status = msg
	if duration > 0 {
		t := time.Now().Add(duration)
		a.StatusUntil = &t
	} else {
		a.StatusUntil = nil
	}
}
