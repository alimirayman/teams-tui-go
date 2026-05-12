package main

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/atotto/clipboard"
)

// ---------------------------------------------------------------------------
// Lipgloss colour palette
// ---------------------------------------------------------------------------

var (
	colCyan     = lipgloss.Color("#00D7D7")
	colYellow   = lipgloss.Color("#FFD700")
	colGreen    = lipgloss.Color("#00D75F")
	colDarkGray = lipgloss.Color("#303030")
	colWhite    = lipgloss.Color("#FFFFFF")
	colRed      = lipgloss.Color("#FF4444")
	colDimGray  = lipgloss.Color("#888888")
)

// Panel border styles.
var (
	normalBorder = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(colGreen)

	bellBorder = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(colRed).
			Foreground(colRed).
			Bold(true).
			Background(colWhite)
)

// ---------------------------------------------------------------------------
// Bubble Tea messages (async events → model)
// ---------------------------------------------------------------------------

// MsgChatsLoaded is sent when the background chat refresh completes.
type MsgChatsLoaded struct {
	Chats           []Chat
	CurrentUserName *string
}

// MsgMessagesLoaded is sent when messages for a specific chat have loaded.
type MsgMessagesLoaded struct {
	ChatIndex int
	Messages  []Message
	NextLink  string
}

// MsgMoreMessagesLoaded is sent when older messages are loaded via pagination.
type MsgMoreMessagesLoaded struct {
	ChatIndex int
	Messages  []Message
	NextLink  string
}

// MsgNewMessage signals that a new message arrived in a non-selected chat.
type MsgNewMessage struct {
	ChatID  string
	Message Message
}

// MsgTick is the heartbeat used for periodic refresh and bell timeout.
type MsgTick struct{}

// MsgSendDone signals that a message send attempt has completed.
type MsgSendDone struct{ Err error }

// ---------------------------------------------------------------------------
// Model — the Bubble Tea application model
// ---------------------------------------------------------------------------

// Model is the central Bubble Tea model. It embeds App state and adds
// all TUI-specific fields.
type Model struct {
	app      *App
	clientID string
	userID   string // authenticated user ID for markChatRead

	// Layout dimensions (set on WindowSizeMsg).
	width  int
	height int

	// Textarea for composing messages.
	textarea textarea.Model

	// Stable ordering of chat IDs.
	stableChatOrder []string

	// Track last-seen message IDs and timestamps per chat.
	lastMsgID   map[string]string
	lastMsgTime map[string]time.Time

	// Track latest chats from the API (before applying stable order).
	latestChats []Chat

	// Timer tracking for periodic refreshes.
	lastChatRefresh    time.Time
	lastMessageRefresh time.Time

	// Which chat index was selected when we last issued a message load.
	lastRefreshIndex int

	// Track last-read message IDs per chat to avoid redundant API calls.
	lastReadMsgID map[string]string

	// Track application focus.
	focused bool
}

// NewModel creates the initial Bubble Tea model.
func NewModel(app *App, clientID, userID string) Model {
	ta := textarea.New()
	ta.Placeholder = "Type your message..."
	ta.ShowLineNumbers = false
	ta.CharLimit = 0

	return Model{
		app:           app,
		clientID:      clientID,
		userID:        userID,
		textarea:      ta,
		lastMsgID:     make(map[string]string),
		lastMsgTime:   make(map[string]time.Time),
		lastReadMsgID: make(map[string]string),
		focused:       true,
	}
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

// Init issues the first tick command to start the event loop.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
		loadChatsCmd(m.clientID),
		func() tea.Msg {
			fmt.Print("\x1b[?1004h") // Enable focus reporting
			return nil
		},
	)
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

// Update is the Bubble Tea update function — processes messages and returns
// the new model plus any commands.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	wasInputMode := m.app.InputMode

	switch msg := msg.(type) {

	// ── Window resize ────────────────────────────────────────────────────
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.textarea.SetWidth(msgPanelWidth(m.width) - 4)

	// ── Heartbeat tick ───────────────────────────────────────────────────
	case MsgTick:
		cmds = append(cmds, tickCmd())

		// Clear expired status messages.
		if m.app.StatusUntil != nil && time.Now().After(*m.app.StatusUntil) {
			m.app.Status = ""
			m.app.StatusUntil = nil
		}

		// Periodic chat refresh every ~3 s.
		if time.Since(m.lastChatRefresh) >= 3*time.Second {
			m.lastChatRefresh = time.Now()
			cmds = append(cmds, loadChatsCmd(m.clientID))
		}

		// Periodic message refresh every ~3 s.
		if m.app.GetSelectedChat() != nil &&
			time.Since(m.lastMessageRefresh) >= 3*time.Second {
			m.lastMessageRefresh = time.Now()
			chat := m.app.GetSelectedChat()
			idx := m.app.SelectedIndex
			cmds = append(cmds, loadMessagesCmd(m.clientID, chat.ID, idx))
		}

	// ── Chat list loaded ─────────────────────────────────────────────────
	case MsgChatsLoaded:
		m.latestChats = msg.Chats
		if msg.CurrentUserName != nil {
			m.app.SetCurrentUser(*msg.CurrentUserName)
		}

		// Preserve current selection by chat ID.
		selectedID := ""
		if chat := m.app.GetSelectedChat(); chat != nil {
			selectedID = chat.ID
		}

		// Build stable order.
		m = m.mergeChats(m.latestChats)

		// Restore selection.
		if selectedID != "" {
			for i, c := range m.app.Chats {
				if c.ID == selectedID {
					m.app.SelectedIndex = i
					break
				}
			}
		}

		// Kick off new-message checks for non-selected chats.
		for i, c := range m.app.Chats {
			if i == m.app.SelectedIndex {
				continue
			}
			cmds = append(cmds, checkNewMessageCmd(m.clientID, c.ID))
		}

		// Refresh messages if selected chat is set.
		if chat := m.app.GetSelectedChat(); chat != nil {
			cmds = append(cmds, loadMessagesCmd(m.clientID, chat.ID, m.app.SelectedIndex))
		}

	// ── Messages loaded ──────────────────────────────────────────────────
	case MsgMessagesLoaded:
		// Discard if the selected chat changed since we issued the load.
		if msg.ChatIndex != m.app.SelectedIndex {
			break
		}
		prev := m.app.Messages
		// Only update if content changed.
		if !messagesEqual(prev, msg.Messages) {
			isNewMessage := len(prev) == 0 || (len(msg.Messages) > 0 && prev[0].ID != msg.Messages[0].ID)
			m.app.SetMessages(msg.Messages, msg.NextLink)
			
			// Only snap to bottom if a new message arrived and the user isn't 
			// currently busy selecting/reacting to an older message.
			if isNewMessage && !m.app.MessageSelectionMode {
				m.app.SnapToBottom = true
			}

			// If there is a new message, move this chat to the top.
			if len(msg.Messages) > 0 {
				newLastID := msg.Messages[0].ID // API returns newest first
				newTime, _ := time.Parse(time.RFC3339Nano, msg.Messages[0].CreatedDateTime)
				chat := m.app.GetSelectedChat()
				if chat != nil {
					if old, ok := m.lastMsgID[chat.ID]; !ok || old != newLastID {
						m.lastMsgID[chat.ID] = newLastID
						m.lastMsgTime[chat.ID] = newTime
						m.promoteChat(chat.ID)

						// If we sent the message, mark it as read immediately.
						if m.isOwn(msg.Messages[0]) {
							m.lastReadMsgID[chat.ID] = newLastID
						}
					}
				}
			}
		}
		m.updateScroll()

	// ── More messages loaded (pagination) ───────────────────────────────
	case MsgMoreMessagesLoaded:
		if msg.ChatIndex != m.app.SelectedIndex {
			break
		}
		// Record the oldest message ID to maintain scroll context.
		if len(m.app.Messages) > 0 {
			m.app.PendingScrollID = m.app.Messages[len(m.app.Messages)-1].ID
		}
		m.app.AppendOlderMessages(msg.Messages, msg.NextLink)
		m.updateScroll()

	// ── Focus / Blur ─────────────────────────────────────────────────────
	case tea.FocusMsg:
		m.focused = true
		m = m.markRead()

	case tea.BlurMsg:
		m.focused = false

	// ── New message in non-selected chat ─────────────────────────────────
	case MsgNewMessage:
		prev := m.lastMsgID[msg.ChatID]
		if prev == msg.Message.ID {
			break // not actually new
		}
		m.lastMsgID[msg.ChatID] = msg.Message.ID
		newTime, _ := time.Parse(time.RFC3339Nano, msg.Message.CreatedDateTime)
		m.lastMsgTime[msg.ChatID] = newTime

		// If we sent the message (e.g. from another client), mark it as read.
		if m.isOwn(msg.Message) {
			m.lastReadMsgID[msg.ChatID] = msg.Message.ID
			m.promoteChat(msg.ChatID)
			m = m.rebuildChatList()
			break // No notification needed for own messages
		}

		// Trigger notification.
		senderName := ""
		if msg.Message.From != nil && msg.Message.From.User != nil && msg.Message.From.User.DisplayName != nil {
			senderName = *msg.Message.From.User.DisplayName
		}
		m.notify(senderName, msg.Message)

		// Move chat to top.
		m.promoteChat(msg.ChatID)

		// Restore selection.
		selectedID := ""
		if chat := m.app.GetSelectedChat(); chat != nil {
			selectedID = chat.ID
		}
		m = m.rebuildChatList()
		if selectedID != "" {
			for i, c := range m.app.Chats {
				if c.ID == selectedID {
					m.app.SelectedIndex = i
					break
				}
			}
		}

	// ── Message send result ───────────────────────────────────────────────
	case MsgSendDone:
		if msg.Err != nil {
			m.app.SetStatus("Send error: "+msg.Err.Error(), 5*time.Second)
		} else {
			// Immediately reload messages after send.
			if chat := m.app.GetSelectedChat(); chat != nil {
				cmds = append(cmds, loadMessagesCmd(m.clientID, chat.ID, m.app.SelectedIndex))
			}
		}

	// ── Keyboard input ────────────────────────────────────────────────────
	case tea.KeyMsg:
		m = m.markRead()
		var cmd tea.Cmd
		m, cmd = m.handleKey(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	// Update textarea if in input mode.
	if m.app.InputMode && wasInputMode {
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		cmds = append(cmds, cmd)
		m.app.InputBuffer = m.textarea.Value()
	}

	return m, tea.Batch(cmds...)
}

// handleKey processes keyboard input and returns the updated model + command.
func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.app.InputMode {
		return m.handleInputModeKey(msg)
	}
	return m.handleNormalModeKey(msg)
}

func (m Model) handleNormalModeKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.app.DeleteConfirmMode {
		return m.handleDeleteConfirmModeKey(msg)
	}
	if m.app.ReactionMode {
		return m.handleReactionModeKey(msg)
	}
	if m.app.MessageSelectionMode {
		return m.handleMessageSelectionModeKey(msg)
	}

	prevIdx := m.app.SelectedIndex

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "j", "down":
		m.app.NextChat()

	case "k", "up":
		m.app.PreviousChat()

	case "n":
		m.app.ToggleNotificationMode()
		nm := m.app.NotificationMode
		cfg := LoadConfig()
		if cfg == nil {
			cfg = &Config{}
		}
		cfg.NotificationMode = &nm
		_ = SaveConfig(cfg)

	case "i":
		m.app.InputMode = true
		m.app.InputBuffer = ""
		m.textarea.Reset()
		return m, m.textarea.Focus()

	case "K", "pgup":
		if m.app.ScrollOffset == 0 && m.app.NextLink != "" && !m.app.LoadingMessages {
			m.app.SetLoadingMessages(true)
			return m, loadMoreMessagesCmd(m.clientID, m.app.NextLink, m.app.SelectedIndex)
		}
		m.app.ScrollOffset -= 10
		if m.app.ScrollOffset < 0 {
			m.app.ScrollOffset = 0
		}
		m.app.SnapToBottom = false

	case "J", "pgdown":
		m.app.ScrollOffset += 10
		if m.app.ScrollOffset >= m.app.MaxScroll {
			m.app.ScrollOffset = m.app.MaxScroll
			m.app.SnapToBottom = true
		}

	case "m":
		if len(m.app.Messages) > 0 {
			m.app.MessageSelectionMode = true
			m.app.MessageSelectedIndex = 0 // start at the newest message (at index 0 in the API list)
		}
	}

	// If chat selection changed, reload messages.
	if m.app.SelectedIndex != prevIdx {
		m.app.Messages = nil
		m.app.NextLink = ""
		m.app.SetLoadingMessages(true)
		m.app.SnapToBottom = true
		if chat := m.app.GetSelectedChat(); chat != nil {
			m = m.markRead()
			return m, loadMessagesCmd(m.clientID, chat.ID, m.app.SelectedIndex)
		}
	}

	return m, nil
}

func (m Model) handleInputModeKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.app.InputMode = false
		m.app.InputBuffer = ""
		m.app.EditingMessageID = nil
		m.textarea.Reset()
		return m, nil

	case "enter":
		content := strings.Trim(m.textarea.Value(), "\n\r")
		if content == "" {
			return m, nil
		}
		m.app.InputMode = false
		m.app.InputBuffer = ""
		m.textarea.Reset()
		chat := m.app.GetSelectedChat()
		if chat == nil {
			return m, nil
		}
		if m.app.EditingMessageID != nil {
			msgID := *m.app.EditingMessageID
			m.app.EditingMessageID = nil
			return m, updateMessageCmd(m.clientID, chat.ID, msgID, content)
		}
		return m, sendMessageCmd(m.clientID, chat.ID, content)

	case "alt+enter", "shift+enter", "ctrl+enter":
		m.textarea.InsertString("\n")
		return m, nil
	}

	// All other keys are forwarded to the textarea (handled in Update).
	return m, nil
}

func (m Model) handleMessageSelectionModeKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "m":
		m.app.MessageSelectionMode = false
		return m, nil

	case "j", "down":
		if m.app.MessageSelectedIndex > 0 {
			m.app.MessageSelectedIndex--
		}

	case "k", "up":
		if m.app.MessageSelectedIndex < len(m.app.Messages)-1 {
			m.app.MessageSelectedIndex++
		}

	case "r":
		m.app.ReactionMode = true
		return m, nil

	case "y":
		if m.app.MessageSelectedIndex < len(m.app.Messages) {
			msgObj := m.app.Messages[m.app.MessageSelectedIndex]
			if msgObj.Body != nil && msgObj.Body.Content != nil {
				text := HTMLToText(*msgObj.Body.Content, msgObj.Attachments)
				if err := clipboard.WriteAll(text); err == nil {
					m.app.SetStatus("Message copied to clipboard", 3*time.Second)
				} else {
					m.app.SetStatus("Clipboard error: "+err.Error(), 5*time.Second)
				}
			}
			m.app.MessageSelectionMode = false
		}
		return m, nil

	case "d":
		if m.app.MessageSelectedIndex < len(m.app.Messages) {
			msgObj := m.app.Messages[m.app.MessageSelectedIndex]
			if m.isOwn(msgObj) {
				m.app.DeleteConfirmMode = true
			} else {
				m.app.SetStatus("Cannot delete messages from others", 3*time.Second)
			}
		}
		return m, nil

	case "e":
		if m.app.MessageSelectedIndex < len(m.app.Messages) {
			msgObj := m.app.Messages[m.app.MessageSelectedIndex]
			if m.isOwn(msgObj) {
				m.app.MessageSelectionMode = false
				m.app.EditingMessageID = &msgObj.ID
				m.app.InputMode = true
				content := ""
				if msgObj.Body != nil && msgObj.Body.Content != nil {
					content = HTMLToText(*msgObj.Body.Content, msgObj.Attachments)
				}
				m.textarea.SetValue(content)
				return m, m.textarea.Focus()
			} else {
				m.app.SetStatus("Cannot edit messages from others", 3*time.Second)
			}
		}
		return m, nil
	}

	return m, nil
}

func (m Model) handleReactionModeKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "r":
		m.app.ReactionMode = false
		return m, nil

	case "1", "2", "3", "4", "5", "6":
		types := []string{"👍", "❤️", "😂", "😮", "😢", "😡"}
		idx := int(msg.String()[0] - '1')
		if idx >= 0 && idx < len(types) {
			reactionType := types[idx]
			chat := m.app.GetSelectedChat()
			if chat != nil && m.app.MessageSelectedIndex < len(m.app.Messages) {
				msgObj := m.app.Messages[m.app.MessageSelectedIndex]

				// Check if current user already has this reaction.
				hasReaction := false
				if m.app.CurrentUserName != nil {
					for _, r := range msgObj.Reactions {
						rType := strings.ToLower(r.ReactionType)
						targetType := strings.ToLower(reactionType)
						// Match either keyword or emoji directly.
						match := rType == targetType ||
							(rType == "like" && targetType == "👍") ||
							(rType == "heart" && targetType == "❤️") ||
							(rType == "laugh" && targetType == "😂") ||
							(rType == "surprised" && targetType == "😮") ||
							(rType == "sad" && targetType == "😢") ||
							(rType == "angry" && targetType == "😡")

						if match && r.User != nil && r.User.User != nil &&
							r.User.User.ID != nil &&
							*r.User.User.ID == m.app.CurrentUserID {
							hasReaction = true
							break
						}
					}
				}

				m.app.ReactionMode = false
				m.app.MessageSelectionMode = false
				if hasReaction {
					return m, unsetReactionCmd(m.clientID, chat.ID, msgObj.ID, reactionType)
				}
				return m, setReactionCmd(m.clientID, chat.ID, msgObj.ID, reactionType)
			}
		}
	}
	return m, nil
}

func (m Model) handleDeleteConfirmModeKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		m.app.DeleteConfirmMode = false
		m.app.MessageSelectionMode = false
		chat := m.app.GetSelectedChat()
		if chat != nil && m.app.MessageSelectedIndex < len(m.app.Messages) {
			msgObj := m.app.Messages[m.app.MessageSelectedIndex]
			return m, deleteMessageCmd(m.clientID, chat.ID, msgObj.ID)
		}
	case "n", "N", "esc":
		m.app.DeleteConfirmMode = false
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

// View renders the complete TUI.
func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	chatW := chatPanelWidth(m.width)
	msgW := msgPanelWidth(m.width)
	// Account for borders on both panels.
	innerH := m.height - 5 // subtract status bar (3) + border rows (2)
	if innerH < 1 {
		innerH = 1
	}

	chatPanel := m.renderChatList(chatW-2, innerH)
	
	right := m.renderRightPanel(msgW-2, innerH)

	left := normalBorder.Width(chatW - 2).Height(innerH).Render(chatPanel)

	top := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	return lipgloss.JoinVertical(lipgloss.Left, top, m.renderStatusBar(m.width))
}

// renderRightPanel renders the messages panel (with optional input area).
func (m Model) renderRightPanel(w, h int) string {
	if !m.app.InputMode {
		title := "Messages (i:compose, m:select, K/J:scroll)"
		if m.app.MessageSelectionMode {
			title = "MESSAGE MODE (j/k:nav, r:react, y:yank, d:delete, e:edit, ESC/m:exit)"
		}
		msgContent := m.renderMessages(w, h-1)
		return normalBorder.Width(w).Height(h).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(colGreen).
			Render(lipgloss.JoinVertical(lipgloss.Left,
				lipgloss.NewStyle().Foreground(colDimGray).Render(title),
				msgContent,
			))
	}

	// Input mode: split height between messages and textarea.
	inputH := 5
	msgH := h - inputH - 1
	if msgH < 1 {
		msgH = 1
	}

	msgContent := m.renderMessages(w, msgH-1)
	title := "Messages (ESC to cancel)"
	if m.app.EditingMessageID != nil {
		title = "EDITING MESSAGE (ESC to cancel)"
	}
	msgBox := normalBorder.Width(w).Height(msgH).
		Render(lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.NewStyle().Foreground(colDimGray).Render(title),
			msgContent,
		))

	m.textarea.SetWidth(w)
	m.textarea.SetHeight(inputH - 2)
	inputBox := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(colGreen).
		Width(w).Height(inputH - 1).
		Render(lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.NewStyle().Foreground(colDimGray).
				Render("Type your message (Enter to send, Alt+Enter for new line, ESC to cancel)"),
			m.textarea.View(),
		))

	return lipgloss.JoinVertical(lipgloss.Left, msgBox, inputBox)
}

// ---------------------------------------------------------------------------
// Chat list rendering
// ---------------------------------------------------------------------------

func (m Model) renderChatList(w, h int) string {
	title := lipgloss.NewStyle().Foreground(colDimGray).
		Render("Teams Chats (j↑/k↓ to navigate, q to quit)")

	if len(m.app.Chats) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left, title, m.app.Status)
	}

	visibleCount := h - 1
	if visibleCount < 1 {
		visibleCount = 1
	}

	if m.app.SelectedIndex < m.app.ChatScrollOffset {
		m.app.ChatScrollOffset = m.app.SelectedIndex
	} else if m.app.SelectedIndex >= m.app.ChatScrollOffset+visibleCount {
		m.app.ChatScrollOffset = m.app.SelectedIndex - visibleCount + 1
	}

	lines := []string{title}
	
	start := m.app.ChatScrollOffset
	end := start + visibleCount
	if end > len(m.app.Chats) {
		end = len(m.app.Chats)
	}

	for i := start; i < end; i++ {
		c := m.app.Chats[i]
		chatType := c.ChatType
		displayName := ""
		if c.CachedDisplayName != nil {
			displayName = *c.CachedDisplayName
		}

		labelStr := "[" + chatType + "] " + displayName
		unread := m.isUnread(c)
		if unread {
			labelStr = "● " + labelStr
		}

		var label string
		if i == m.app.SelectedIndex {
			label = lipgloss.NewStyle().
				Foreground(colYellow).
				Bold(unread).
				Background(colDarkGray).
				Width(w).
				MaxWidth(w).
				Render(labelStr)
		} else {
			typeTag := lipgloss.NewStyle().Foreground(colCyan).Render("[" + chatType + "]")
			base := typeTag + " " + displayName
			if unread {
				base = lipgloss.NewStyle().Bold(true).Render("● " + base)
			}
			label = lipgloss.NewStyle().MaxWidth(w).Render(base)
		}
		lines = append(lines, label)
	}

	return strings.Join(lines, "\n")
}

// ---------------------------------------------------------------------------
// Messages rendering
// ---------------------------------------------------------------------------

func (m Model) renderMessages(w, h int) string {
	if m.app.LoadingMessages && len(m.app.Messages) == 0 {
		return lipgloss.NewStyle().Foreground(colDimGray).Render("Loading messages...")
	}
	if len(m.app.Messages) == 0 {
		return lipgloss.NewStyle().Foreground(colDimGray).Render("No messages.")
	}

	maxW := w * 9 / 10 // 90% of panel width

	// Messages arrive newest-first from API; render newest at the bottom.
	msgs := m.app.Messages
	start := 0
	msgs = msgs[start:]

	var lines []string
	var prevSender string
	var prevTime time.Time

	var selectedStartLine, selectedEndLine int = -1, -1
	var pendingScrollLine int = -1

	// Iterate in reverse (slice is newest-first) → append → shows newest at bottom.
	for i := len(msgs) - 1; i >= 0; i-- {
		msg := msgs[i]

		if m.app.PendingScrollID != "" && msg.ID == m.app.PendingScrollID {
			pendingScrollLine = len(lines)
		}

		sender := ""
		if msg.From != nil && msg.From.User != nil && msg.From.User.DisplayName != nil {
			sender = *msg.From.User.DisplayName
		}

		msgTime, _ := time.Parse(time.RFC3339Nano, msg.CreatedDateTime)
		msgTime = msgTime.Local()
		senderChanged := sender != prevSender
		timeGap := !msgTime.IsZero() && !prevTime.IsZero() && msgTime.Hour() != prevTime.Hour()

		if senderChanged || timeGap {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			dateStr := ""
			if !msgTime.IsZero() {
				dateStr = msgTime.Format("Jan 02 15:04")
			}
			var header string
			if m.isOwn(msg) {
				h := lipgloss.NewStyle().Foreground(colGreen).Render(dateStr + " Me")
				header = padLeft(h, w)
			} else {
				header = lipgloss.NewStyle().Foreground(colCyan).Render(sender + " " + dateStr)
			}
			lines = append(lines, header)
		}
		prevSender = sender
		prevTime = msgTime

		// Render body.
		body := ""
		if msg.Body != nil && msg.Body.Content != nil {
			body = HTMLToText(*msg.Body.Content, msg.Attachments)
		}

		// Add reactions.
		reactionsStr := renderReactions(msg.Reactions)
		if reactionsStr != "" {
			if body != "" {
				body += "\n" + reactionsStr
			} else {
				body = reactionsStr
			}
		}

		msgLines := wordWrap(body, maxW)
		padding := 0
		if m.isOwn(msg) {
			maxMsgW := 0
			for _, l := range msgLines {
				lw := lipgloss.Width(l)
				if lw > maxMsgW {
					maxMsgW = lw
				}
			}
			padding = w - maxMsgW
			if padding < 0 {
				padding = 0
			}
		}

		padStr := strings.Repeat(" ", padding)
		isSelected := m.app.MessageSelectionMode && (start+i == m.app.MessageSelectedIndex)
		if isSelected {
			selectedStartLine = len(lines)
		}
		for _, line := range msgLines {
			content := padStr + line
			if isSelected {
				content = lipgloss.NewStyle().
					Background(colDarkGray).
					Foreground(colYellow).
					Width(w).
					Render(content)
			}
			lines = append(lines, content)
		}
		if isSelected {
			selectedEndLine = len(lines)
		}
	}

	// Apply scroll.
	total := len(lines)
	m.app.MaxScroll = total - h
	if m.app.MaxScroll < 0 {
		m.app.MaxScroll = 0
	}

	if m.app.SnapToBottom {
		m.app.ScrollOffset = m.app.MaxScroll
	}

	// Auto-scroll to keep selection visible.
	if m.app.MessageSelectionMode && selectedStartLine != -1 {
		if selectedStartLine < m.app.ScrollOffset {
			m.app.ScrollOffset = selectedStartLine
			m.app.SnapToBottom = false
		} else if selectedEndLine > m.app.ScrollOffset+h {
			m.app.ScrollOffset = selectedEndLine - h
			m.app.SnapToBottom = false
		}
	}

	if m.app.ScrollOffset < 0 {
		m.app.ScrollOffset = 0
	}
	if m.app.ScrollOffset > m.app.MaxScroll {
		m.app.ScrollOffset = m.app.MaxScroll
	}

	// Apply pending scroll jump (pagination context).
	// Apply pending scroll jump (pagination context).
	if m.app.PendingScrollID != "" {
		if pendingScrollLine != -1 {
			// Jump to where the old top message moved.
			// "with few line down" -> subtract 3 so the user sees some new context.
			m.app.ScrollOffset = pendingScrollLine - 3
			m.app.SnapToBottom = false
			
			// Clamp again after jump.
			if m.app.ScrollOffset < 0 {
				m.app.ScrollOffset = 0
			}
			if m.app.ScrollOffset > m.app.MaxScroll {
				m.app.ScrollOffset = m.app.MaxScroll
			}
		}
		m.app.PendingScrollID = ""
	}

	// Slice lines for the visible window.
	start2 := m.app.ScrollOffset
	end := start2 + h
	if end > len(lines) {
		end = len(lines)
	}
	if start2 > len(lines) {
		start2 = len(lines)
	}

	return strings.Join(lines[start2:end], "\n")
}

// ---------------------------------------------------------------------------
// Status bar
// ---------------------------------------------------------------------------

func (m Model) renderStatusBar(w int) string {
	if m.app.DeleteConfirmMode {
		return bellBorder.Width(w - 2).Height(1).Render(
			lipgloss.NewStyle().Foreground(colRed).Bold(true).Render(
				"DELETE MESSAGE? (y:yes / n:no)",
			),
		)
	}
	if m.app.ReactionMode {
		return normalBorder.Width(w - 2).Height(1).Render(
			lipgloss.NewStyle().Foreground(colYellow).Render(
				"REACT: 1:👍 2:❤️ 3:😂 4:😮 5:😢 6:😡 (ESC:cancel)",
			),
		)
	}
	text := fmt.Sprintf("%s | Notification (n): %s", m.app.Status, m.app.NotificationMode)
	if m.app.LoadingMessages && len(m.app.Messages) > 0 {
		text = "⏳ Loading older messages... | " + text
	}
	if m.app.VisualBellActive() {
		return bellBorder.Width(w - 2).Height(1).Render(text)
	}
	return normalBorder.Width(w - 2).Height(1).Render(
		lipgloss.NewStyle().Foreground(colGreen).Render(text),
	)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func chatPanelWidth(total int) int {
	return total * 30 / 100
}

func msgPanelWidth(total int) int {
	return total - chatPanelWidth(total)
}

func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return string(r[:maxLen-1]) + "…"
}

// padLeft right-aligns text within width w by prepending spaces.
func padLeft(s string, w int) string {
	visLen := lipgloss.Width(s)
	pad := w - visLen
	if pad <= 0 {
		return s
	}
	return strings.Repeat(" ", pad) + s
}

// wordWrap breaks s into lines of at most maxW runes, preserving whitespace.
func wordWrap(s string, maxW int) []string {
	if maxW <= 0 {
		return []string{s}
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line == "" {
			out = append(out, "")
			continue
		}

		// While the line is too long, find a place to break.
		for lipgloss.Width(line) > maxW {
			breakIdx := -1
			currW := 0
			runes := []rune(line)
			for i, r := range runes {
				rw := lipgloss.Width(string(r))
				if currW+rw > maxW {
					break
				}
				if r == ' ' || r == '\t' {
					breakIdx = i
				}
				currW += rw
			}

			if breakIdx == -1 {
				// No space found, force break at maxW.
				fitCount := 0
				fitW := 0
				for _, r := range runes {
					rw := lipgloss.Width(string(r))
					if fitW+rw > maxW {
						break
					}
					fitW += rw
					fitCount++
				}
				if fitCount == 0 && len(runes) > 0 {
					fitCount = 1
				}
				out = append(out, string(runes[:fitCount]))
				line = string(runes[fitCount:])
			} else {
				// Break at space.
				out = append(out, string(runes[:breakIdx]))
				line = string(runes[breakIdx+1:])
			}
		}
		out = append(out, line)
	}
	return out
}

// updateScroll recalculates scroll bounds after messages change.
func (m *Model) updateScroll() {
	if m.app.SnapToBottom {
		m.app.ScrollOffset = m.app.MaxScroll
	}
}

// messagesEqual returns true if the two slices have the same count and last ID.
func messagesEqual(a, b []Message) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			return false
		}
		// Compare body content.
		contentA := ""
		if a[i].Body != nil && a[i].Body.Content != nil {
			contentA = *a[i].Body.Content
		}
		contentB := ""
		if b[i].Body != nil && b[i].Body.Content != nil {
			contentB = *b[i].Body.Content
		}
		if contentA != contentB {
			return false
		}
		if len(a[i].Reactions) != len(b[i].Reactions) {
			return false
		}
		// Deep compare reactions.
		for j := range a[i].Reactions {
			if a[i].Reactions[j].ReactionType != b[i].Reactions[j].ReactionType {
				return false
			}
			// If we really want to be sure, we could compare user IDs,
			// but type changes are the most common visible change.
		}
	}
	return true
}

func (m Model) markRead() Model {
	if !m.focused {
		return m
	}
	chat := m.app.GetSelectedChat()
	if chat == nil {
		return m
	}

	lastID := m.lastMsgID[chat.ID]
	if lastID == "" && len(m.app.Messages) > 0 {
		lastID = m.app.Messages[0].ID
	}

	if lastID != "" && m.lastReadMsgID[chat.ID] != lastID {
		m.lastReadMsgID[chat.ID] = lastID
		go MarkChatAsRead(func() string {
			t, _ := GetValidTokenSilent(m.clientID)
			return t
		}(), chat.ID, m.userID)
	}
	return m
}

func (m Model) isUnread(c Chat) bool {
	// If it's the currently selected chat, we consider it read (or about to be).
	// However, to keep the UI consistent, let's only rely on the IDs.
	
	lastID, hasLast := m.lastMsgID[c.ID]
	if !hasLast {
		return false
	}
	
	// Check local read state first.
	if readID, ok := m.lastReadMsgID[c.ID]; ok && readID == lastID {
		return false
	}

	// If we have a local lastReadMsgID but it's not the lastID, it's definitely unread
	// (unless we were about to fall back to viewpoint).
	if _, ok := m.lastReadMsgID[c.ID]; ok {
		return true
	}

	// Fallback to server-side viewpoint.
	if c.Viewpoint != nil {
		readTime, _ := time.Parse(time.RFC3339Nano, c.Viewpoint.LastMessageReadDateTime)
		lastTime := m.lastMsgTime[c.ID]
		if !lastTime.IsZero() && !readTime.IsZero() {
			// If latest message is newer than last read time, it's unread.
			// Add 1s buffer for safety.
			return lastTime.After(readTime.Add(time.Second))
		}
	}

	return false
}

func (m Model) isOwn(msg Message) bool {
	if m.app.CurrentUserName == nil {
		return false
	}
	if msg.From == nil || msg.From.User == nil || msg.From.User.DisplayName == nil {
		return false
	}
	return *msg.From.User.DisplayName == *m.app.CurrentUserName
}

func renderReactions(reactions []MessageReaction) string {
	if len(reactions) == 0 {
		return ""
	}

	counts := make(map[string]int)
	for _, r := range reactions {
		counts[strings.ToLower(r.ReactionType)]++
	}

	var parts []string
	// Known reaction types for stable ordering.
	types := []string{"like", "heart", "laugh", "surprised", "sad", "angry"}
	for _, t := range types {
		if count, ok := counts[t]; ok {
			emoji := reactionEmoji(t)
			if count > 1 {
				parts = append(parts, fmt.Sprintf("%s %d", emoji, count))
			} else {
				parts = append(parts, emoji)
			}
		}
	}

	// Any other types?
	for t, count := range counts {
		found := false
		for _, known := range types {
			if t == known {
				found = true
				break
			}
		}
		if !found {
			emoji := reactionEmoji(t)
			if count > 1 {
				parts = append(parts, fmt.Sprintf("%s %d", emoji, count))
			} else {
				parts = append(parts, emoji)
			}
		}
	}

	if len(parts) == 0 {
		return ""
	}

	return lipgloss.NewStyle().Foreground(colDimGray).Render(strings.Join(parts, "  "))
}

func reactionEmoji(t string) string {
	switch strings.ToLower(t) {
	case "like", "👍":
		return "👍"
	case "heart", "❤️":
		return "❤️"
	case "laugh", "😂":
		return "😂"
	case "surprised", "😮":
		return "😮"
	case "sad", "😢":
		return "😢"
	case "angry", "😡":
		return "😡"
	default:
		return t
	}
}

// ---------------------------------------------------------------------------
// Chat ordering helpers
// ---------------------------------------------------------------------------

// promoteChat moves chatID to position 0 in the stable order.
func (m *Model) promoteChat(chatID string) {
	for i, id := range m.stableChatOrder {
		if id == chatID {
			m.stableChatOrder = append([]string{chatID},
				append(m.stableChatOrder[:i], m.stableChatOrder[i+1:]...)...)
			return
		}
	}
	// Not found — prepend.
	m.stableChatOrder = append([]string{chatID}, m.stableChatOrder...)
}

// mergeChats integrates fresh chats from the API into the stable order.
// New chats with messages are prepended; new chats without messages are appended.
func (m Model) mergeChats(fresh []Chat) Model {
	// Build a set of known IDs.
	known := make(map[string]bool, len(m.stableChatOrder))
	for _, id := range m.stableChatOrder {
		known[id] = true
	}

	// Determine which chats are new.
	freshByID := make(map[string]Chat, len(fresh))
	for _, c := range fresh {
		freshByID[c.ID] = c
	}

	// Add new chats.
	var newWithMsg []string
	var newWithout []string
	for _, c := range fresh {
		if !known[c.ID] {
			if _, hasMsg := m.lastMsgID[c.ID]; hasMsg {
				newWithMsg = append(newWithMsg, c.ID)
			} else {
				newWithout = append(newWithout, c.ID)
			}
		}
	}

	m.stableChatOrder = append(newWithMsg, append(m.stableChatOrder, newWithout...)...)
	return m.rebuildChatList()
}

// rebuildChatList reorders app.Chats to match stableChatOrder.
func (m Model) rebuildChatList() Model {
	byID := make(map[string]Chat, len(m.latestChats))
	for _, c := range m.latestChats {
		byID[c.ID] = c
	}

	ordered := make([]Chat, 0, len(m.stableChatOrder))
	for _, id := range m.stableChatOrder {
		if c, ok := byID[id]; ok {
			ordered = append(ordered, c)
		}
	}
	m.app.Chats = ordered
	return m
}

// ---------------------------------------------------------------------------
// Notifications
// ---------------------------------------------------------------------------

// notify triggers the appropriate notification based on the app's mode.
func (m *Model) notify(senderName string, msg Message) {
	body := ""
	if m.app.NotificationShowPreview {
		if msg.Body != nil && msg.Body.Content != nil {
			body = HTMLToText(*msg.Body.Content, msg.Attachments)
			// Remove newlines and collapse spaces for a cleaner notification body.
			body = strings.ReplaceAll(body, "\n", " ")
			body = strings.Join(strings.Fields(body), " ")
			if m.app.NotificationPreviewLen > 0 && utf8.RuneCountInString(body) > m.app.NotificationPreviewLen {
				body = string([]rune(body)[:m.app.NotificationPreviewLen]) + "..."
			}
		}
	}

	switch m.app.NotificationMode {
	case NotificationConsole:
		fmt.Print("\a") // BEL
		m.app.TriggerVisualBell()
	case NotificationSystem:
		go sendDesktopNotification(senderName, body)
	case NotificationBoth:
		fmt.Print("\a")
		m.app.TriggerVisualBell()
		go sendDesktopNotification(senderName, body)
	}
}
