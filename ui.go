package main

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

	// Track last-seen message IDs per chat for new-message detection.
	lastMsgID map[string]string

	// Track latest chats from the API (before applying stable order).
	latestChats []Chat

	// Timer tracking for periodic refreshes.
	lastChatRefresh    time.Time
	lastMessageRefresh time.Time

	// Which chat index was selected when we last issued a message load.
	lastRefreshIndex int
}

// NewModel creates the initial Bubble Tea model.
func NewModel(app *App, clientID, userID string) Model {
	ta := textarea.New()
	ta.Placeholder = "Type your message..."
	ta.ShowLineNumbers = false
	ta.CharLimit = 0

	return Model{
		app:      app,
		clientID: clientID,
		userID:   userID,
		textarea: ta,
		lastMsgID: make(map[string]string),
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
			m.app.SetMessages(msg.Messages)
			m.app.SnapToBottom = true

			// If there is a new message, move this chat to the top.
			if len(msg.Messages) > 0 {
				newLastID := msg.Messages[0].ID // API returns newest first
				chat := m.app.GetSelectedChat()
				if chat != nil {
					if old, ok := m.lastMsgID[chat.ID]; !ok || old != newLastID {
						m.lastMsgID[chat.ID] = newLastID
						m.promoteChat(chat.ID)
					}
				}
			}

			// Mark chat as read (fire and forget).
			if chat := m.app.GetSelectedChat(); chat != nil {
				go MarkChatAsRead(func() string {
					t, _ := GetValidTokenSilent(m.clientID)
					return t
				}(), chat.ID, m.userID)
			}
		}
		m.updateScroll()

	// ── New message in non-selected chat ─────────────────────────────────
	case MsgNewMessage:
		prev := m.lastMsgID[msg.ChatID]
		if prev == msg.Message.ID {
			break // not actually new
		}
		m.lastMsgID[msg.ChatID] = msg.Message.ID

		// Trigger notification.
		senderName := ""
		if msg.Message.From != nil && msg.Message.From.User != nil && msg.Message.From.User.DisplayName != nil {
			senderName = *msg.Message.From.User.DisplayName
		}
		m.notify(senderName)

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
			m.app.Status = "Send error: " + msg.Err.Error()
		} else {
			// Immediately reload messages after send.
			if chat := m.app.GetSelectedChat(); chat != nil {
				cmds = append(cmds, loadMessagesCmd(m.clientID, chat.ID, m.app.SelectedIndex))
			}
		}

	// ── Keyboard input ────────────────────────────────────────────────────
	case tea.KeyMsg:
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
	}

	// If chat selection changed, reload messages.
	if m.app.SelectedIndex != prevIdx {
		m.app.Messages = nil
		m.app.SetLoadingMessages(true)
		m.app.SnapToBottom = true
		if chat := m.app.GetSelectedChat(); chat != nil {
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
		m.textarea.Reset()
		return m, nil

	case "enter":
		content := strings.TrimSpace(m.textarea.Value())
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
		return m, sendMessageCmd(m.clientID, chat.ID, content)

	case "alt+enter", "shift+enter", "ctrl+enter":
		m.textarea.InsertString("\n")
		return m, nil
	}

	// All other keys are forwarded to the textarea (handled in Update).
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
		title := "Messages (i to compose, PgUp(K)/PgDn(J) to scroll)"
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
	msgBox := normalBorder.Width(w).Height(msgH).
		Render(lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.NewStyle().Foreground(colDimGray).Render("Messages (ESC to cancel)"),
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

		var label string
		if i == m.app.SelectedIndex {
			label = lipgloss.NewStyle().
				Foreground(colYellow).
				Bold(true).
				Background(colDarkGray).
				Width(w).
				MaxWidth(w).
				Render(labelStr)
		} else {
			typeTag := lipgloss.NewStyle().Foreground(colCyan).Render("[" + chatType + "]")
			label = lipgloss.NewStyle().MaxWidth(w).Render(typeTag + " " + displayName)
		}
		lines = append(lines, label)
	}

	return strings.Join(lines, "\n")
}

// ---------------------------------------------------------------------------
// Messages rendering
// ---------------------------------------------------------------------------

func (m Model) renderMessages(w, h int) string {
	if m.app.LoadingMessages {
		return lipgloss.NewStyle().Foreground(colDimGray).Render("Loading messages...")
	}
	if len(m.app.Messages) == 0 {
		return lipgloss.NewStyle().Foreground(colDimGray).Render("No messages.")
	}

	maxW := w * 9 / 10 // 90% of panel width

	// Messages arrive newest-first from API; render newest at the bottom.
	msgs := m.app.Messages
	start := 0
	if len(msgs) > 100 {
		start = len(msgs) - 100
	}
	msgs = msgs[start:]

	var lines []string
	var prevSender string
	var prevTime time.Time
	isOwn := func(msg Message) bool {
		if m.app.CurrentUserName == nil {
			return false
		}
		if msg.From == nil || msg.From.User == nil || msg.From.User.DisplayName == nil {
			return false
		}
		return *msg.From.User.DisplayName == *m.app.CurrentUserName
	}

	// Iterate in reverse (slice is newest-first) → append → shows newest at bottom.
	for i := len(msgs) - 1; i >= 0; i-- {
		msg := msgs[i]

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
			if isOwn(msg) {
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

		for _, line := range wordWrap(body, maxW) {
			if isOwn(msg) {
				lines = append(lines, padLeft(line, w))
			} else {
				lines = append(lines, line)
			}
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
	if m.app.ScrollOffset < 0 {
		m.app.ScrollOffset = 0
	}
	if m.app.ScrollOffset > m.app.MaxScroll {
		m.app.ScrollOffset = m.app.MaxScroll
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
	text := fmt.Sprintf("%s | Notification (n): %s", m.app.Status, m.app.NotificationMode)
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

// wordWrap breaks s into lines of at most maxW runes.
func wordWrap(s string, maxW int) []string {
	if maxW <= 0 {
		return []string{s}
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := ""
		for _, w := range words {
			if line == "" {
				line = w
			} else if utf8.RuneCountInString(line)+1+utf8.RuneCountInString(w) <= maxW {
				line += " " + w
			} else {
				out = append(out, line)
				line = w
			}
		}
		if line != "" {
			out = append(out, line)
		}
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
	if len(a) == 0 {
		return true
	}
	return a[0].ID == b[0].ID
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
func (m *Model) notify(senderName string) {
	mode := m.app.NotificationMode
	if mode == NotificationNone {
		return
	}
	if mode == NotificationConsole || mode == NotificationBoth {
		fmt.Print("\a") // BEL character
		m.app.TriggerVisualBell()
	}
	if mode == NotificationSystem || mode == NotificationBoth {
		go sendDesktopNotification(senderName)
	}
}
