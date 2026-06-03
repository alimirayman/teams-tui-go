package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gen2brain/beeep"
	"regexp"
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
	IsSearch  bool
}

// MsgTick is the heartbeat used for periodic refresh and bell timeout.
type MsgTick struct{}

// MsgSendDone signals that a message send attempt has completed.
type MsgSendDone struct{ Err error }

// MsgEditDone signals that a message edit (PATCH) has completed.
type MsgEditDone struct {
	ChatID    string
	MessageID string
	Content   string // the markdown content the user typed (used to derive HTML)
	Err       error
}

// MsgUserSearchDone is sent when the directory search completes.
type MsgUserSearchDone struct {
	Users []User
	Err   error
}

// MsgCreateChatDone is sent when a chat creation/retrieval completes.
type MsgCreateChatDone struct {
	Chat *Chat
	Err  error
}

// MsgBackgroundMessagesLoaded is sent when messages are fetched in the background to inspect reactions.
type MsgBackgroundMessagesLoaded struct {
	ChatID   string
	Messages []Message
}

// MsgPollReactionsLoaded is returned when messages for active chats are fetched to inspect reactions.
type MsgPollReactionsLoaded struct {
	Results map[string][]Message
}

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

	// Textinput for searching.
	searchInput textinput.Model

	// Textinput for searching users.
	userSearchInput textinput.Model

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

	// Track whether a chat list load is in progress.
	loadingChats bool

	// Track last-read reaction keys per chat.
	lastReadReactions map[string]map[string]bool

	// Track whether reactions have been initialized/seen for each chat.
	reactionsInitialized map[string]bool

	// Track which reactions have already triggered a notification.
	notifiedReactions map[string]map[string]bool

	// Timer tracking for reaction polling.
	lastReactionPoll time.Time

	// Track application focus.
	focused bool

	// pendingEdits holds optimistic in-memory edits (messageID → HTML content)
	// that have been PATCHed to the API but may not yet be reflected in GET
	// responses (Graph API has eventual consistency after PATCH). Entries are
	// cleared once the API echoes back the updated content.
	pendingEdits map[string]string

	// favourites holds chat IDs that have been starred by the user.
	// Loaded from and persisted to favourites.json in the app config dir.
	favourites map[string]bool
}

// NewModel creates the initial Bubble Tea model.
func NewModel(app *App, clientID, userID string) Model {
	ta := textarea.New()
	ta.Placeholder = "Type your message..."
	ta.ShowLineNumbers = false
	ta.CharLimit = 0

	ti := textinput.New()
	ti.Placeholder = "Search history..."
	ti.CharLimit = 100
	ti.Width = 40

	tiUser := textinput.New()
	tiUser.Placeholder = "Filter local chats, or enter exact email to open..."
	tiUser.CharLimit = 100
	tiUser.Width = 40

	return Model{
		app:                  app,
		clientID:             clientID,
		userID:               userID,
		textarea:             ta,
		searchInput:          ti,
		userSearchInput:      tiUser,
		lastMsgID:            make(map[string]string),
		lastMsgTime:          make(map[string]time.Time),
		lastReadMsgID:        make(map[string]string),
		lastReadReactions:    make(map[string]map[string]bool),
		reactionsInitialized: make(map[string]bool),
		notifiedReactions:    make(map[string]map[string]bool),
		pendingEdits:         make(map[string]string),
		favourites:           make(map[string]bool),
		focused:              true,
	}
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

// Init issues the first tick command to start the event loop.
// Note: the initial chat list is already loaded synchronously in main.go, so
// we do not fire a redundant loadChatsCmd here. The periodic tick handles all
// subsequent refreshes.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
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
	wasSearchMode := m.app.SearchMode
	wasUserSearchMode := m.app.UserSearchMode

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
		if m.app.SearchStatusUntil != nil && time.Now().After(*m.app.SearchStatusUntil) {
			m.app.SearchStatus = ""
			m.app.SearchStatusUntil = nil
		}

		// Periodic chat refresh every ~15 s.
		if !m.loadingChats && time.Since(m.lastChatRefresh) >= 15*time.Second {
			m.lastChatRefresh = time.Now()
			m.loadingChats = true
			cmds = append(cmds, loadChatsCmd(m.clientID, m.app.Chats, m.app.CurrentUserName))
		}

		// Periodic message refresh every ~3 s.
		if m.app.GetSelectedChat() != nil &&
			time.Since(m.lastMessageRefresh) >= 3*time.Second {
			m.lastMessageRefresh = time.Now()
			chat := m.app.GetSelectedChat()
			idx := m.app.SelectedIndex
			cmds = append(cmds, loadMessagesCmd(m.clientID, chat.ID, idx))
		}

		// Periodic reaction poll every ~10 s for other active chats.
		if time.Since(m.lastReactionPoll) >= 10*time.Second {
			m.lastReactionPoll = time.Now()

			var chatsToPoll []string
			selectedID := ""
			if chat := m.app.GetSelectedChat(); chat != nil {
				selectedID = chat.ID
			}

			count := 0
			for _, id := range m.stableChatOrder {
				if id == selectedID {
					continue
				}
				chatsToPoll = append(chatsToPoll, id)
				count++
				if count >= 5 {
					break
				}
			}
			if len(chatsToPoll) > 0 {
				cmds = append(cmds, pollReactionsCmd(m.clientID, chatsToPoll))
			}
		}

	// ── Chat list loaded ─────────────────────────────────────────────────
	case MsgChatsLoaded:
		m.loadingChats = false
		m.latestChats = msg.Chats
		if msg.CurrentUserName != nil {
			m.app.SetCurrentUser(*msg.CurrentUserName)
		}

		// Preserve current selection by chat ID.
		selectedID := ""
		if chat := m.app.GetSelectedChat(); chat != nil {
			selectedID = chat.ID
		}

		// Detect new messages in any of the loaded chats.
		for _, c := range m.latestChats {
			if c.LastMessagePreview == nil {
				continue
			}
			prevID, ok := m.lastMsgID[c.ID]
			newID := c.LastMessagePreview.ID

			newTime, _ := time.Parse(time.RFC3339Nano, c.LastMessagePreview.CreatedDateTime)
			if c.LastUpdated != nil {
				lut, _ := time.Parse(time.RFC3339Nano, *c.LastUpdated)
				if lut.After(newTime) {
					newTime = lut
				}
			}

			if ok && prevID != newID && !m.lastMsgTime[c.ID].IsZero() && newTime.After(m.lastMsgTime[c.ID].Add(time.Second)) {
				m.lastMsgID[c.ID] = newID
				m.lastMsgTime[c.ID] = newTime

				// Determine if it was sent by us.
				isOwnMsg := false
				if m.app.CurrentUserName != nil && c.LastMessagePreview.From != nil &&
					c.LastMessagePreview.From.User != nil && c.LastMessagePreview.From.User.DisplayName != nil {
					isOwnMsg = *c.LastMessagePreview.From.User.DisplayName == *m.app.CurrentUserName
				}

				isActiveChat := false
				if selChat := m.app.GetSelectedChat(); selChat != nil && selChat.ID == c.ID && m.focused {
					isActiveChat = true
				}

				if isOwnMsg || isActiveChat {
					m.lastReadMsgID[c.ID] = newID
					m.promoteChat(c.ID)
					if isActiveChat {
						go MarkChatAsRead(func() string {
							t, _ := GetValidTokenSilent(m.clientID)
							return t
						}(), c.ID, m.userID)
					}
				} else {
					// Track the unread message ID locally to ensure isUnread returns true
					m.lastReadMsgID[c.ID] = prevID

					// Trigger notification.
					senderName := ""
					if c.LastMessagePreview.From != nil && c.LastMessagePreview.From.User != nil && c.LastMessagePreview.From.User.DisplayName != nil {
						senderName = *c.LastMessagePreview.From.User.DisplayName
					}

					// Build a temporary Message object for notification.
					tempMsg := Message{
						ID:              newID,
						CreatedDateTime: c.LastMessagePreview.CreatedDateTime,
						From:            c.LastMessagePreview.From,
						Body:            c.LastMessagePreview.Body,
					}
					m.notify(senderName, tempMsg)
					m.promoteChat(c.ID)
				}
			} else if !ok || (ok && m.lastMsgTime[c.ID].IsZero()) {
				// Initialize cache for newly seen or uninitialized chat.
				m.lastMsgID[c.ID] = newID
				m.lastMsgTime[c.ID] = newTime
			}

			// Detect new reactions on LastMessagePreview
			if m.lastReadReactions[c.ID] == nil {
				m.lastReadReactions[c.ID] = make(map[string]bool)
			}
			if ok {
				var newReactions []MessageReaction
				for _, rKey := range m.getReactionKeys(c.LastMessagePreview) {
					if !m.lastReadReactions[c.ID][rKey] {
						for _, r := range c.LastMessagePreview.Reactions {
							if getReactionKey(c.LastMessagePreview.ID, r) == rKey {
								newReactions = append(newReactions, r)
								break
							}
						}
					}
				}
				if len(newReactions) > 0 {
					isActiveChat := false
					if selChat := m.app.GetSelectedChat(); selChat != nil && selChat.ID == c.ID && m.focused {
						isActiveChat = true
					}
					if !isActiveChat {
						m.notifyReaction(c, c.LastMessagePreview, newReactions)
						m.promoteChat(c.ID)
					} else {
						for _, r := range newReactions {
							m.lastReadReactions[c.ID][getReactionKey(c.LastMessagePreview.ID, r)] = true
						}
					}
				}
			} else {
				for _, rKey := range m.getReactionKeys(c.LastMessagePreview) {
					m.lastReadReactions[c.ID][rKey] = true
				}
			}
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

		// Refresh messages if selected chat is set.
		if chat := m.app.GetSelectedChat(); chat != nil {
			cmds = append(cmds, loadMessagesCmd(m.clientID, chat.ID, m.app.SelectedIndex))
		}

	case MsgBackgroundMessagesLoaded:
		if m.lastReadReactions[msg.ChatID] == nil {
			m.lastReadReactions[msg.ChatID] = make(map[string]bool)
		}
		if m.notifiedReactions[msg.ChatID] == nil {
			m.notifiedReactions[msg.ChatID] = make(map[string]bool)
		}

		var chatObj *Chat
		for _, c := range m.latestChats {
			if c.ID == msg.ChatID {
				chatObj = &c
				break
			}
		}

		var newReactions []MessageReaction
		isInit := !m.reactionsInitialized[msg.ChatID]

		for _, msgObj := range msg.Messages {
			var msgNewReactions []MessageReaction
			for _, rKey := range m.getReactionKeys(&msgObj) {
				if !m.lastReadReactions[msg.ChatID][rKey] {
					for _, r := range msgObj.Reactions {
						if getReactionKey(msgObj.ID, r) == rKey {
							if isInit && m.isOldReaction(msgObj, r) {
								m.lastReadReactions[msg.ChatID][rKey] = true
							} else {
								if !m.notifiedReactions[msg.ChatID][rKey] {
									msgNewReactions = append(msgNewReactions, r)
									m.notifiedReactions[msg.ChatID][rKey] = true
								}
							}
							break
						}
					}
				}
			}
			if len(msgNewReactions) > 0 && chatObj != nil {
				m.notifyReaction(*chatObj, &msgObj, msgNewReactions)
				newReactions = append(newReactions, msgNewReactions...)
			}
		}

		m.reactionsInitialized[msg.ChatID] = true
		m.app.HistoryMessages[msg.ChatID] = mergeHistoryMessages(m.app.HistoryMessages[msg.ChatID], msg.Messages)

		if len(newReactions) > 0 {
			m.promoteChat(msg.ChatID)
		}

	case MsgPollReactionsLoaded:
		for chatID, msgs := range msg.Results {
			if m.lastReadReactions[chatID] == nil {
				m.lastReadReactions[chatID] = make(map[string]bool)
			}
			if m.notifiedReactions[chatID] == nil {
				m.notifiedReactions[chatID] = make(map[string]bool)
			}

			var chatObj *Chat
			for _, c := range m.latestChats {
				if c.ID == chatID {
					chatObj = &c
					break
				}
			}

			var newReactions []MessageReaction
			isInit := !m.reactionsInitialized[chatID]

			for _, msgObj := range msgs {
				var msgNewReactions []MessageReaction
				for _, rKey := range m.getReactionKeys(&msgObj) {
					if !m.lastReadReactions[chatID][rKey] {
						for _, r := range msgObj.Reactions {
							if getReactionKey(msgObj.ID, r) == rKey {
								if isInit && m.isOldReaction(msgObj, r) {
									m.lastReadReactions[chatID][rKey] = true
								} else {
									if !m.notifiedReactions[chatID][rKey] {
										msgNewReactions = append(msgNewReactions, r)
										m.notifiedReactions[chatID][rKey] = true
									}
								}
								break
							}
						}
					}
				}
				if len(msgNewReactions) > 0 && chatObj != nil {
					m.notifyReaction(*chatObj, &msgObj, msgNewReactions)
					newReactions = append(newReactions, msgNewReactions...)
				}
			}

			m.reactionsInitialized[chatID] = true
			m.app.HistoryMessages[chatID] = mergeHistoryMessages(m.app.HistoryMessages[chatID], msgs)

			if len(newReactions) > 0 {
				m.promoteChat(chatID)
			}
		}

	// ── Messages loaded ──────────────────────────────────────────────────
	case MsgMessagesLoaded:
		// Always update the cache for the chat that was loaded, even if the
		// user has since switched away. This ensures that revisiting the chat
		// later shows fresh data immediately.
		// Retrieve the chat ID from the index at the time the load was issued.
		if msg.ChatIndex >= 0 && msg.ChatIndex < len(m.app.Chats) {
			loadedChatID := m.app.Chats[msg.ChatIndex].ID
			if loadedChatID != "" && len(msg.Messages) > 0 {
				// Merge into the existing cache rather than overwriting it.
				// A blind overwrite would discard older pages that were already
				// loaded via pagination, and would also wipe pending edit patches.
				existing := m.app.CachedMessages[loadedChatID]
				if len(existing) == 0 {
					m.app.CachedMessages[loadedChatID] = msg.Messages
				} else {
					// Update/add only the messages present in the new batch.
					idxMap := make(map[string]int, len(existing))
					for i, em := range existing {
						idxMap[em.ID] = i
					}
					for _, nm := range msg.Messages {
						if idx, ok := idxMap[nm.ID]; ok {
							existing[idx] = nm
						} else {
							existing = append(existing, nm)
						}
					}
					m.app.CachedMessages[loadedChatID] = existing
				}
				m.app.CachedNextLink[loadedChatID] = msg.NextLink
			}
		}
		// Discard UI update if the selected chat changed since we issued the load.
		if msg.ChatIndex != m.app.SelectedIndex {
			break
		}
		m.app.SetLoadingMessages(false)
		prev := m.app.Messages
		// Only update if content changed.
		if !messagesEqual(prev, msg.Messages) {
			isNewMessage := len(prev) == 0 || (len(msg.Messages) > 0 && prev[0].ID != msg.Messages[0].ID)
			m.app.SetMessages(msg.Messages, msg.NextLink)

			// Re-apply any pending optimistic edits that the API may not have
			// reflected yet (Graph API has eventual consistency after PATCH).
			if chat := m.app.GetSelectedChat(); chat != nil {
				m.applyPendingEdits(chat.ID)
			}

			// Detect new reactions in the loaded messages
			chat := m.app.GetSelectedChat()
			if chat != nil {
				if m.lastReadReactions[chat.ID] == nil {
					m.lastReadReactions[chat.ID] = make(map[string]bool)
				}
				if m.notifiedReactions[chat.ID] == nil {
					m.notifiedReactions[chat.ID] = make(map[string]bool)
				}
				isInit := !m.reactionsInitialized[chat.ID]
				for _, msgObj := range msg.Messages {
					var msgNewReactions []MessageReaction
					for _, rKey := range m.getReactionKeys(&msgObj) {
						if !m.lastReadReactions[chat.ID][rKey] {
							for _, r := range msgObj.Reactions {
								if getReactionKey(msgObj.ID, r) == rKey {
									if isInit && m.isOldReaction(msgObj, r) {
										m.lastReadReactions[chat.ID][rKey] = true
									} else {
										if !m.notifiedReactions[chat.ID][rKey] {
											msgNewReactions = append(msgNewReactions, r)
											if m.focused {
												m.lastReadReactions[chat.ID][rKey] = true
											} else {
												m.notifiedReactions[chat.ID][rKey] = true
											}
										}
									}
									break
								}
							}
						}
					}
					if len(msgNewReactions) > 0 && !m.focused {
						m.notifyReaction(*chat, &msgObj, msgNewReactions)
					}
				}
				m.reactionsInitialized[chat.ID] = true
			}

			// Keep history cache in sync with new incoming messages
			if chat != nil {
				if hist, ok := m.app.HistoryMessages[chat.ID]; ok && len(hist) > 0 {
					var toPrepend []Message
					for _, newMsg := range msg.Messages {
						found := false
						for _, oldMsg := range hist {
							if oldMsg.ID == newMsg.ID {
								found = true
								break
							}
						}
						if !found {
							toPrepend = append(toPrepend, newMsg)
						}
					}
					if len(toPrepend) > 0 {
						m.app.HistoryMessages[chat.ID] = append(toPrepend, hist...)
					}
				}
			}

			// Only snap to bottom if a new message arrived and the user isn't
			// currently busy selecting/reacting to an older message.
			if isNewMessage && !m.app.MessageSelectionMode {
				m.app.SnapToBottom = true
			}

			// If there is a new message, move this chat to the top.
			if len(msg.Messages) > 0 {
				if chat != nil {
					newLastID := ""
					var newTime time.Time
					var latestMsg Message
					if len(msg.Messages) > 0 {
						latestMsg = msg.Messages[0]
						newLastID = latestMsg.ID
						newTime, _ = time.Parse(time.RFC3339Nano, latestMsg.CreatedDateTime)
					}

					if newLastID != "" {
						old, ok := m.lastMsgID[chat.ID]
						if !ok || m.lastMsgTime[chat.ID].IsZero() {
							m.lastMsgID[chat.ID] = newLastID
							m.lastMsgTime[chat.ID] = newTime
						} else if old != newLastID && newTime.After(m.lastMsgTime[chat.ID].Add(time.Second)) {
							m.lastMsgID[chat.ID] = newLastID
							m.lastMsgTime[chat.ID] = newTime
							m.promoteChat(chat.ID)

							isOwnMsg := m.isOwn(latestMsg)
							if isOwnMsg || m.focused {
								m.lastReadMsgID[chat.ID] = newLastID
								if m.focused {
									go MarkChatAsRead(func() string {
										t, _ := GetValidTokenSilent(m.clientID)
										return t
									}(), chat.ID, m.userID)
								}
							} else {
								// Trigger notification if blurred, and mark as unread locally
								m.lastReadMsgID[chat.ID] = old
								senderName := ""
								if latestMsg.From != nil && latestMsg.From.User != nil && latestMsg.From.User.DisplayName != nil {
									senderName = *latestMsg.From.User.DisplayName
								}
								m.notify(senderName, latestMsg)
							}
						}
					}
				}
			}
		}
		// Keep the per-chat message cache in sync for the active chat.
		if chat := m.app.GetSelectedChat(); chat != nil {
			if len(m.app.Messages) > 0 {
				m.app.CachedMessages[chat.ID] = m.app.Messages
				m.app.CachedNextLink[chat.ID] = m.app.NextLink
			}
		}
		// Re-apply pending edits to any cache entries that were just updated.
		if chat := m.app.GetSelectedChat(); chat != nil {
			m.applyPendingEdits(chat.ID)
		}
		m.updateScroll()

	// ── More messages loaded (pagination) ───────────────────────────────
	case MsgMoreMessagesLoaded:
		if msg.ChatIndex != m.app.SelectedIndex {
			break
		}

		chat := m.app.GetSelectedChat()
		if chat != nil && msg.IsSearch {
			m.app.SetSearchLoadingMessages(false)

			// Update history messages cache directly
			hist := m.app.HistoryMessages[chat.ID]
			existingIDs := make(map[string]bool)
			for _, mObj := range hist {
				existingIDs[mObj.ID] = true
			}
			for _, newM := range msg.Messages {
				if !existingIDs[newM.ID] {
					hist = append(hist, newM)
				}
			}
			m.app.HistoryMessages[chat.ID] = hist
			m.app.HistoryNextLink[chat.ID] = msg.NextLink

			// Rebuild search popup results dynamically if search popup is still open
			if m.app.SearchPopupMode {
				m.RebuildSearchPopupResults()
				m.saveSearchState()

				if msg.NextLink != "" {
					m.app.SetSearchLoadingMessages(true)
					m.app.SetSearchStatus(fmt.Sprintf("Searching all history for '%s'... Loaded %d messages", m.app.SearchQuery, len(hist)), 0)
					cmds = append(cmds, loadMoreMessagesCmd(m.clientID, msg.NextLink, m.app.SelectedIndex, true))
				} else {
					m.app.SetSearchStatus(fmt.Sprintf("Search finished. Loaded all %d messages in history.", len(hist)), 5*time.Second)
				}
			} else {
				// Silently continue loading next page in background so history is ready
				if msg.NextLink != "" {
					m.app.SetSearchLoadingMessages(true)
					cmds = append(cmds, loadMoreMessagesCmd(m.clientID, msg.NextLink, m.app.SelectedIndex, true))
				}
			}
		} else {
			// Standard main chat scroll pagination
			if len(m.app.Messages) > 0 {
				m.app.PendingScrollID = m.app.Messages[len(m.app.Messages)-1].ID
			}
			m.app.AppendOlderMessages(msg.Messages, msg.NextLink)
			m.updateScroll()
			// Update the per-chat cache with the newly paginated messages.
			if chat != nil {
				m.app.CachedMessages[chat.ID] = m.app.Messages
				m.app.CachedNextLink[chat.ID] = msg.NextLink
			}
		}

	case MsgUserSearchDone:
		m.app.UserSearchLoading = false
		if msg.Err != nil {
			errStr := msg.Err.Error()
			if strings.Contains(errStr, "403") {
				m.app.UserSearchStatus = "⚠️ Directory search not allowed (missing permissions). Enter the exact email/UPN (e.g. user@domain.com) to open/create the chat directly."
			} else {
				m.app.UserSearchStatus = "⚠️ Search error: " + errStr
			}
			m.app.UserSearchDirectoryResults = nil
		} else {
			m.app.UserSearchDirectoryResults = msg.Users
			if len(msg.Users) == 0 {
				m.app.UserSearchStatus = "No directory users found matching query."
			} else {
				m.app.UserSearchStatus = fmt.Sprintf("Found %d directory users.", len(msg.Users))
			}
		}
		m.app.UserSearchSelectedIndex = 0

	case MsgCreateChatDone:
		m.app.UserSearchLoading = false
		if msg.Err != nil {
			m.app.UserSearchStatus = "⚠️ Create chat error: " + msg.Err.Error()
		} else if msg.Chat != nil {
			m.app.UserSearchPopupMode = false
			m.app.UserSearchMode = false
			m.app.UserSearchQuery = ""
			m.app.UserSearchLocalResults = nil
			m.app.UserSearchDirectoryResults = nil

			chat := *msg.Chat
			if m.app.CurrentUserName != nil {
				chat.Members = filterMember(chat.Members, *m.app.CurrentUserName)
			}
			name := computeDisplayName(&chat)
			chat.CachedDisplayName = &name

			existsInLatest := false
			for _, c := range m.latestChats {
				if c.ID == chat.ID {
					existsInLatest = true
					break
				}
			}
			if !existsInLatest {
				m.latestChats = append([]Chat{chat}, m.latestChats...)
			}

			m.promoteChat(chat.ID)
			m = m.mergeChats(m.latestChats)

			for i, c := range m.app.Chats {
				if c.ID == chat.ID {
					m.app.SelectedIndex = i
					break
				}
			}

			m.app.Messages = nil
			m.app.NextLink = ""
			m.app.SetLoadingMessages(true)
			m.app.SnapToBottom = true

			cmds = append(cmds, loadMessagesCmd(m.clientID, chat.ID, m.app.SelectedIndex))
		}

	// ── Focus / Blur ─────────────────────────────────────────────────────
	case tea.FocusMsg:
		m.focused = true
		m = m.markRead()

	case tea.BlurMsg:
		m.focused = false

	// ── Message send result ───────────────────────────────────────────────
	case MsgSendDone:
		if msg.Err != nil {
			m.app.SetStatus("Send error: "+msg.Err.Error(), 5*time.Second)
		} else {
			// Immediately reload messages after send.
			if chat := m.app.GetSelectedChat(); chat != nil {
				m.lastMessageRefresh = time.Now()
				cmds = append(cmds, loadMessagesCmd(m.clientID, chat.ID, m.app.SelectedIndex))
			}
		}

	// ── Message edit result ───────────────────────────────────────────────
	case MsgEditDone:
		if msg.Err != nil {
			m.app.SetStatus("Edit error: "+msg.Err.Error(), 5*time.Second)
		} else {
			// Convert the user's markdown to HTML (same path as formatMessageBody).
			newHTML := markdownToHTML(msg.Content)

			// Record this as a pending edit. Subsequent MsgMessagesLoaded
			// handlers will re-apply it after SetMessages, because Graph API
			// GET may return the old content for several seconds after a PATCH
			// (eventual consistency). The entry is cleared once the API echoes
			// back the updated content.
			m.pendingEdits[msg.MessageID] = newHTML

			// Also patch the live caches immediately so the UI updates now.
			m.applyPendingEdits(msg.ChatID)
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
		oldVal := m.textarea.Value()
		m.textarea, cmd = m.textarea.Update(msg)
		cmds = append(cmds, cmd)

		newVal := m.textarea.Value()
		if oldVal != newVal {
			replaced := replaceEmoticons(newVal)
			if replaced != newVal {
				// In bubbles/textarea v1.0.0, we can get the current column offset.
				// SetValue resets the cursor to the start, so we attempt to restore it.
				// For multi-line messages, we fallback to moving the cursor to the end
				// to avoid jumping to the wrong line.
				col := m.textarea.LineInfo().ColumnOffset
				diff := len([]rune(newVal)) - len([]rune(replaced))
				m.textarea.SetValue(replaced)
				if m.textarea.LineCount() <= 1 {
					m.textarea.SetCursor(col - diff)
				} else {
					m.textarea.CursorEnd()
				}
			}
		}
		m.app.InputBuffer = m.textarea.Value()
	}

	// Update search input if in search mode.
	if m.app.SearchMode && wasSearchMode {
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		cmds = append(cmds, cmd)
	}

	// Update user search input if in user search mode.
	if m.app.UserSearchMode && wasUserSearchMode {
		var cmd tea.Cmd
		oldVal := m.userSearchInput.Value()
		m.userSearchInput, cmd = m.userSearchInput.Update(msg)
		cmds = append(cmds, cmd)

		newVal := m.userSearchInput.Value()
		if oldVal != newVal {
			m.app.UserSearchQuery = newVal
			m.updateUserSearchLocalResults()
			m.app.UserSearchSelectedIndex = 0
		}
	}

	return m, tea.Batch(cmds...)
}

// handleKey processes keyboard input and returns the updated model + command.
func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.app.MessagePopupMode {
		return m.handleMessagePopupKey(msg)
	}
	if m.app.InputMode {
		return m.handleInputModeKey(msg)
	}
	if m.app.UserSearchPopupMode {
		if m.app.UserSearchMode {
			return m.handleUserSearchInputModeKey(msg)
		}
		return m.handleUserSearchNavigationKey(msg)
	}
	if m.app.SearchPopupMode {
		if m.app.SearchMode {
			return m.handleSearchModeKey(msg)
		}
		return m.handleSearchPopupNavigationKey(msg)
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
	if m.app.UrlSelectionMode {
		return m.handleUrlSelectionModeKey(msg)
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

	case "c":
		m.app.UserSearchPopupMode = true
		m.app.UserSearchMode = true
		m.app.UserSearchQuery = ""
		m.app.UserSearchStatus = ""
		m.app.UserSearchLocalResults = nil
		m.app.UserSearchDirectoryResults = nil
		m.app.UserSearchSelectedIndex = 0
		m.app.UserSearchLoading = false
		m.userSearchInput.SetValue("")
		m.userSearchInput.Focus()
		return m, textinput.Blink

	case "/":
		m.app.MainChatScrollOffset = m.app.ScrollOffset
		m.app.MainChatSnapToBottom = m.app.SnapToBottom
		m.app.SearchPopupMode = true
		m.app.SearchMode = true
		chat := m.app.GetSelectedChat()
		if chat != nil {
			hist := m.app.HistoryMessages[chat.ID]
			existingIDs := make(map[string]bool)
			for _, mObj := range hist {
				existingIDs[mObj.ID] = true
			}
			var toAdd []Message
			for _, mainM := range m.app.Messages {
				if !existingIDs[mainM.ID] {
					toAdd = append(toAdd, mainM)
				}
			}
			if len(toAdd) > 0 {
				m.app.HistoryMessages[chat.ID] = append(toAdd, hist...)
			}
			if !m.app.HistoryInitialized[chat.ID] {
				m.app.HistoryNextLink[chat.ID] = m.app.NextLink
				m.app.HistoryInitialized[chat.ID] = true
			}
		}
		m.loadSearchState()
		m.searchInput.SetValue(m.app.SearchQuery)
		m.searchInput.Focus()
		return m, textinput.Blink

	case "esc":
		if m.app.SearchActive {
			m.app.SearchActive = false
			m.app.SearchQuery = ""
			m.app.SetStatus("Highlights cleared.", 3*time.Second)
		}

	case "K", "pgup":
		if m.app.ScrollOffset == 0 && m.app.NextLink != "" && !m.app.LoadingMessages {
			m.app.SetLoadingMessages(true)
			return m, loadMoreMessagesCmd(m.clientID, m.app.NextLink, m.app.SelectedIndex, false)
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
			if m.app.SnapToBottom {
				m.app.MessageSelectedIndex = 0
			} else {
				// Try to start selection at the message currently at the top of the viewport.
				m.app.MessageSelectedIndex = 0
				for i := 0; i < len(m.app.Messages); i++ {
					if i < len(m.app.MessageLineOffsets) && m.app.MessageLineOffsets[i] <= m.app.ScrollOffset {
						m.app.MessageSelectedIndex = i
						break
					}
				}
			}
		}

	case "f":
		// Toggle favourite on the selected chat.
		if chat := m.app.GetSelectedChat(); chat != nil {
			if m.favourites[chat.ID] {
				delete(m.favourites, chat.ID)
				m.app.SetStatus("★ Removed from favourites: "+*chat.CachedDisplayName, 3*time.Second)
			} else {
				m.favourites[chat.ID] = true
				m.app.SetStatus("★ Added to favourites: "+*chat.CachedDisplayName, 3*time.Second)
			}
			// Persist and rebuild the list to reorder immediately.
			_ = SaveFavourites(m.favourites)
			m = m.rebuildChatList()
			// Restore selection to the toggled chat.
			for i, c := range m.app.Chats {
				if c.ID == chat.ID {
					m.app.SelectedIndex = i
					break
				}
			}
		}
	}

	// If chat selection changed, reload messages.
	if m.app.SelectedIndex != prevIdx {
		m.app.SearchMode = false
		m.app.SearchActive = false
		m.app.SearchQuery = ""
		m.app.SnapToBottom = true
		if chat := m.app.GetSelectedChat(); chat != nil {
			m = m.markRead()
			m.lastMessageRefresh = time.Now()
			// If we have a cached copy of this chat's messages, show it
			// immediately so the user doesn't see a blank "loading" screen.
			if cached, ok := m.app.CachedMessages[chat.ID]; ok && len(cached) > 0 {
				m.app.Messages = cached
				m.app.NextLink = m.app.CachedNextLink[chat.ID]
				m.app.SetLoadingMessages(false)
			} else {
				m.app.Messages = nil
				m.app.NextLink = ""
				m.app.SetLoadingMessages(true)
			}
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
		m.app.ReplyToMessage = nil
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
		if m.app.ReplyToMessage != nil {
			ref := m.app.ReplyToMessage
			m.app.ReplyToMessage = nil
			return m, sendMessageWithRefCmd(m.clientID, chat.ID, content, ref)
		}
		return m, sendMessageCmd(m.clientID, chat.ID, content)

	case "alt+enter", "shift+enter", "ctrl+enter":
		m.textarea.InsertString("\n")
		return m, nil
	}

	// All other keys are forwarded to the textarea (handled in Update).
	return m, nil
}

func (m Model) handleSearchModeKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.app.SearchMode = false
		m.searchInput.Blur()
		return m, nil

	case "enter":
		query := strings.TrimSpace(m.searchInput.Value())
		m.app.SearchMode = false
		m.searchInput.Blur()
		if query == "" {
			m.app.SearchActive = false
			m.app.SearchQuery = ""
			m.app.SearchPopupResults = nil
			m.app.SearchPopupSelectedIndex = 0
			m.saveSearchState()
			return m, nil
		}
		m.app.SearchActive = true
		m.app.SearchQuery = query

		chat := m.app.GetSelectedChat()
		if chat != nil {
			m.RebuildSearchPopupResults()

			nextLink := m.app.HistoryNextLink[chat.ID]
			if nextLink != "" {
				m.app.SetSearchLoadingMessages(true)
				m.app.SetSearchStatus(fmt.Sprintf("Searching all history for '%s'... Loaded %d messages", query, len(m.app.HistoryMessages[chat.ID])), 0)
				m.saveSearchState()
				return m, loadMoreMessagesCmd(m.clientID, nextLink, m.app.SelectedIndex, true)
			}
		}

		m.app.SetSearchStatus("Search finished.", 3*time.Second)
		m.saveSearchState()
		return m, nil
	}

	return m, nil
}

func (m Model) handleMessagePopupKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "v", "enter":
		m.app.MessagePopupMode = false
		return m, nil

	case "j", "down":
		if m.app.MessageSelectedIndex > 0 {
			m.app.MessageSelectedIndex--
			m.app.MessagePopupScrollOffset = 0
		}

	case "k", "up":
		if m.app.MessageSelectedIndex < len(m.app.Messages)-1 {
			m.app.MessageSelectedIndex++
			m.app.MessagePopupScrollOffset = 0
		} else if m.app.NextLink != "" && !m.app.LoadingMessages {
			// Already at the oldest loaded message — fetch the next page.
			m.app.SetLoadingMessages(true)
			m.app.MessagePopupScrollOffset = 0
			return m, loadMoreMessagesCmd(m.clientID, m.app.NextLink, m.app.SelectedIndex, false)
		}

	case "J", "shift+down", "pgdown":
		m.app.MessagePopupScrollOffset += 3

	case "K", "shift+up", "pgup":
		m.app.MessagePopupScrollOffset -= 3
		if m.app.MessagePopupScrollOffset < 0 {
			m.app.MessagePopupScrollOffset = 0
		}
	}
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
		} else if m.app.NextLink != "" && !m.app.LoadingMessages {
			// Already at the oldest loaded message — fetch the next page.
			m.app.SetLoadingMessages(true)
			return m, loadMoreMessagesCmd(m.clientID, m.app.NextLink, m.app.SelectedIndex, false)
		}

	case "r":
		m.app.ReactionMode = true
		return m, nil

	case "y":
		if m.app.MessageSelectedIndex < len(m.app.Messages) {
			msgObj := m.app.Messages[m.app.MessageSelectedIndex]
			if msgObj.Body != nil && msgObj.Body.Content != nil {
				text := stripANSI(HTMLToText(*msgObj.Body.Content, msgObj.Attachments))
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
					content = HTMLToMarkdown(*msgObj.Body.Content)
				}
				m.textarea.SetValue(content)
				return m, m.textarea.Focus()
			} else {
				m.app.SetStatus("Cannot edit messages from others", 3*time.Second)
			}
		}
		return m, nil

	case "a":
		if m.app.MessageSelectedIndex < len(m.app.Messages) {
			msgObj := m.app.Messages[m.app.MessageSelectedIndex]
			// Store the referenced message — the actual Teams attachment is built on send.
			ref := msgObj
			m.app.ReplyToMessage = &ref
			m.app.MessageSelectionMode = false
			m.app.InputMode = true
			m.textarea.Reset()
			return m, m.textarea.Focus()
		}
		return m, nil
	case "u":
		if m.app.MessageSelectedIndex < len(m.app.Messages) {
			msgObj := m.app.Messages[m.app.MessageSelectedIndex]
			if msgObj.Body != nil && msgObj.Body.Content != nil {
				urls := ExtractURLs(*msgObj.Body.Content)
				if len(urls) == 0 {
					m.app.SetStatus("No URLs found in message", 3*time.Second)
				} else if len(urls) == 1 {
					if err := clipboard.WriteAll(urls[0]); err == nil {
						m.app.SetStatus("URL copied to clipboard", 3*time.Second)
					}
					m.app.MessageSelectionMode = false
				} else {
					m.app.UrlSelectionMode = true
					m.app.UrlSelectedIndex = 0
					m.app.UrlsInMessage = urls
				}
			}
		}
		return m, nil
	case "v":
		if len(m.app.Messages) > 0 && m.app.MessageSelectedIndex < len(m.app.Messages) {
			m.app.MessagePopupMode = true
			m.app.MessagePopupScrollOffset = 0
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

func (m Model) updateUrlSelection(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.app.UrlSelectionMode = false
		return m, nil

	case "j", "down":
		if m.app.UrlSelectedIndex < len(m.app.UrlsInMessage)-1 {
			m.app.UrlSelectedIndex++
		}

	case "k", "up":
		if m.app.UrlSelectedIndex > 0 {
			m.app.UrlSelectedIndex--
		}

	case "enter", "y":
		url := m.app.UrlsInMessage[m.app.UrlSelectedIndex]
		if err := clipboard.WriteAll(url); err == nil {
			m.app.SetStatus("URL copied to clipboard", 3*time.Second)
		}
		m.app.UrlSelectionMode = false
		m.app.MessageSelectionMode = false
	}
	return m, nil
}

func (m Model) handleUrlSelectionModeKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.app.UrlSelectionMode = false
		return m, nil

	case "j", "down":
		if m.app.UrlSelectedIndex < len(m.app.UrlsInMessage)-1 {
			m.app.UrlSelectedIndex++
		}

	case "k", "up":
		if m.app.UrlSelectedIndex > 0 {
			m.app.UrlSelectedIndex--
		}

	case "enter", "y":
		url := m.app.UrlsInMessage[m.app.UrlSelectedIndex]
		if err := clipboard.WriteAll(url); err == nil {
			m.app.SetStatus("URL copied to clipboard", 3*time.Second)
		}
		m.app.UrlSelectionMode = false
		m.app.MessageSelectionMode = false
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Main View
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
	mainView := lipgloss.JoinVertical(lipgloss.Left, top, m.renderStatusBar(m.width))

	if m.app.UrlSelectionMode {
		modal := m.renderUrlSelection(m.width, m.height)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
	}

	if m.app.UserSearchPopupMode {
		popupW := m.width * 85 / 100
		popupH := m.height * 80 / 100
		if popupW < 40 {
			popupW = 40
		}
		if popupH < 10 {
			popupH = 10
		}
		modal := m.renderUserSearchPopup(popupW, popupH)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
	}

	if m.app.SearchPopupMode {
		popupW := m.width * 85 / 100
		popupH := m.height * 80 / 100
		if popupW < 40 {
			popupW = 40
		}
		if popupH < 10 {
			popupH = 10
		}
		modal := m.renderSearchPopup(popupW, popupH)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
	}

	if m.app.MessagePopupMode {
		popupW := m.width * 85 / 100
		popupH := m.height * 80 / 100
		if popupW < 40 {
			popupW = 40
		}
		if popupH < 10 {
			popupH = 10
		}
		modal := m.renderMessagePopup(popupW, popupH)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
	}

	return mainView
}

// renderRightPanel renders the messages panel (with optional input area).
func (m Model) renderRightPanel(w, h int) string {
	if !m.app.InputMode {
		title := "Messages (i:compose, m:select, K/J:scroll, /:search)"
		if m.app.MessageSelectionMode {
			title = "MESSAGE MODE (j/k:nav, r:react, y:yank, u:url, d:delete, e:edit, a:answer, v:view, ESC/m:exit)"
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
	// When replying, add 2 extra lines for the quote preview.
	inputH := 5
	if m.app.ReplyToMessage != nil {
		inputH = 7
	}
	msgH := h - inputH - 1
	if msgH < 1 {
		msgH = 1
	}

	msgContent := m.renderMessages(w, msgH-1)
	title := "Messages (ESC to cancel)"
	if m.app.EditingMessageID != nil {
		title = "EDITING MESSAGE (ESC to cancel)"
	} else if m.app.ReplyToMessage != nil {
		ref := m.app.ReplyToMessage
		sender := "someone"
		if ref.From != nil && ref.From.User != nil && ref.From.User.DisplayName != nil {
			sender = *ref.From.User.DisplayName
			if m.isOwn(*ref) {
				sender = "yourself"
			}
		}
		title = "REPLYING TO " + sender + " (ESC to cancel)"
	}
	msgBox := normalBorder.Width(w).Height(msgH).
		Render(lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.NewStyle().Foreground(colDimGray).Render(title),
			msgContent,
		))

	m.textarea.SetWidth(w)
	m.textarea.SetHeight(inputH - 2)

	// Build input box contents — add quote preview when replying.
	hintLine := lipgloss.NewStyle().Foreground(colDimGray).
		Render("Type your message (Enter to send, Alt+Enter for new line, ESC to cancel)")
	inputParts := []string{hintLine}

	if m.app.ReplyToMessage != nil {
		ref := m.app.ReplyToMessage
		// Build a one-line preview using the same style as renderMessageReference.
		preview := ""
		if ref.Body != nil && ref.Body.Content != nil {
			preview = stripANSI(HTMLToText(*ref.Body.Content, ref.Attachments))
		}
		preview = strings.ReplaceAll(preview, "\n", " ")
		const maxPrev = 80
		if len([]rune(preview)) > maxPrev {
			preview = string([]rune(preview)[:maxPrev]) + "…"
		}
		sender := ""
		if ref.From != nil && ref.From.User != nil && ref.From.User.DisplayName != nil {
			sender = *ref.From.User.DisplayName
			if m.isOwn(*ref) {
				sender = "Me"
			}
		}
		bar := lipgloss.NewStyle().Foreground(lipgloss.Color("#4A90D9")).Bold(true).Render("▎")
		name := lipgloss.NewStyle().Foreground(lipgloss.Color("#7EC8E3")).Bold(true).Render(sender)
		text := lipgloss.NewStyle().Foreground(lipgloss.Color("#6C7A89")).Render(": " + preview)
		quoteLine := bar + " " + name + text
		inputParts = append(inputParts, quoteLine)
		// Separator between quote and textarea.
		inputParts = append(inputParts, lipgloss.NewStyle().Foreground(colDimGray).Render(strings.Repeat("─", w)))
		m.textarea.SetHeight(inputH - 4) // hint + quote + separator lines
	}

	inputParts = append(inputParts, m.textarea.View())

	inputBox := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(colGreen).
		Width(w).Height(inputH - 1).
		Render(lipgloss.JoinVertical(lipgloss.Left, inputParts...))

	return lipgloss.JoinVertical(lipgloss.Left, msgBox, inputBox)
}

// ---------------------------------------------------------------------------
// Chat list rendering
// ---------------------------------------------------------------------------

func (m Model) renderChatList(w, h int) string {
	title := lipgloss.NewStyle().Foreground(colDimGray).
		Render("Chats (j/k: nav, c: find, f: ★ fav, q: quit)")

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

		unread := m.isUnread(c)
		reactionEmoji := m.getLatestUnreadReactionEmoji(c)
		isFav := m.favourites[c.ID]

		prefix := ""
		if isFav {
			prefix += "★ "
		}
		if unread {
			prefix += "● "
		}
		if reactionEmoji != "" {
			prefix += reactionEmoji + " "
		}

		labelStr := prefix + "[" + chatType + "] " + displayName

		var label string
		if i == m.app.SelectedIndex {
			label = lipgloss.NewStyle().
				Foreground(colYellow).
				Bold(unread || reactionEmoji != "").
				Background(colDarkGray).
				Width(w).
				MaxWidth(w).
				Render(labelStr)
		} else {
			typeTag := lipgloss.NewStyle().Foreground(colCyan).Render("[" + chatType + "]")
			base := typeTag + " " + displayName
			if isFav {
				star := lipgloss.NewStyle().Foreground(colYellow).Render("★ ")
				base = star + base
			}
			if unread || reactionEmoji != "" {
				pfx := ""
				if unread {
					pfx += "● "
				}
				if reactionEmoji != "" {
					pfx += reactionEmoji + " "
				}
				base = lipgloss.NewStyle().Bold(true).Render(pfx) + base
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

	m.app.MessageLineOffsets = make([]int, len(msgs))

	// Iterate in reverse (slice is newest-first) → append → shows newest at bottom.
	for i := len(msgs) - 1; i >= 0; i-- {
		msg := msgs[i]
		m.app.MessageLineOffsets[i] = len(lines)

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
		timeGap := !msgTime.IsZero() && !prevTime.IsZero() && (msgTime.Year() != prevTime.Year() ||
			msgTime.Month() != prevTime.Month() ||
			msgTime.Day() != prevTime.Day() ||
			msgTime.Hour() != prevTime.Hour())

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
				senderName := "Me"
				if m.app.SearchActive && m.app.SearchQuery != "" {
					senderName = highlightQuery(senderName, m.app.SearchQuery)
				}
				h := lipgloss.NewStyle().Foreground(colGreen).Render(dateStr + " " + senderName)
				header = padLeft(h, w)
			} else {
				senderName := sender
				if m.app.SearchActive && m.app.SearchQuery != "" {
					senderName = highlightQuery(senderName, m.app.SearchQuery)
				}
				header = lipgloss.NewStyle().Foreground(colCyan).Render(senderName + " " + dateStr)
			}
			lines = append(lines, header)
		}
		prevSender = sender
		prevTime = msgTime

		// Render body.
		body := msg.GetPlainText()
		if m.app.SearchActive && m.app.SearchQuery != "" {
			body = highlightQuery(body, m.app.SearchQuery)
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
		msgHeight := selectedEndLine - selectedStartLine
		if selectedStartLine < m.app.ScrollOffset {
			// Selection scrolled above the top — bring it back into view.
			m.app.ScrollOffset = selectedStartLine
			m.app.SnapToBottom = false
		} else if selectedEndLine > m.app.ScrollOffset+h {
			if msgHeight >= h {
				// Message is taller than the viewport — anchor to its top so
				// the user sees the beginning rather than jumping to the end.
				m.app.ScrollOffset = selectedStartLine
			} else {
				// Message fits — scroll just enough to expose its bottom.
				m.app.ScrollOffset = selectedEndLine - h
			}
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

func wordWrap(s string, maxW int) []string {
	if maxW <= 0 {
		return []string{s}
	}
	// Use lipgloss to perform ANSI-aware wrapping. lipgloss (via reflow)
	// correctly handles repeating escape sequences (like OSC 8 links) across
	// line breaks.
	res := lipgloss.NewStyle().Width(maxW).Render(s)
	lines := strings.Split(res, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return lines
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

	// Mark reactions in the selected chat as read.
	if m.lastReadReactions[chat.ID] == nil {
		m.lastReadReactions[chat.ID] = make(map[string]bool)
	}
	for _, msgObj := range m.app.Messages {
		for _, rKey := range m.getReactionKeys(&msgObj) {
			m.lastReadReactions[chat.ID][rKey] = true
		}
	}
	if hist, ok := m.app.HistoryMessages[chat.ID]; ok {
		for _, msgObj := range hist {
			for _, rKey := range m.getReactionKeys(&msgObj) {
				m.lastReadReactions[chat.ID][rKey] = true
			}
		}
	}
	for _, c := range m.latestChats {
		if c.ID == chat.ID {
			for _, rKey := range m.getReactionKeys(c.LastMessagePreview) {
				m.lastReadReactions[chat.ID][rKey] = true
			}
			break
		}
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
	var otherTypes []string
	for t := range counts {
		found := false
		for _, known := range types {
			if t == known {
				found = true
				break
			}
		}
		if !found {
			otherTypes = append(otherTypes, t)
		}
	}
	sort.Strings(otherTypes)

	for _, t := range otherTypes {
		count := counts[t]
		emoji := reactionEmoji(t)
		if count > 1 {
			parts = append(parts, fmt.Sprintf("%s %d", emoji, count))
		} else {
			parts = append(parts, emoji)
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
// Favourited chats are anchored at the top by rebuildChatList, so they are
// skipped here to avoid disrupting the alphabetical favourites group.
func (m *Model) promoteChat(chatID string) {
	// Don't promote favourited chats — they stay anchored at the top.
	if m.favourites[chatID] {
		return
	}
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

	// Add new chats. Use LastMessagePreview directly to determine whether the
	// chat has a message — this avoids depending on m.lastMsgID being populated
	// by the loop in MsgChatsLoaded (a fragile side-effect ordering dependency).
	var newWithMsg []string
	var newWithout []string
	for _, c := range fresh {
		if !known[c.ID] {
			if c.LastMessagePreview != nil {
				newWithMsg = append(newWithMsg, c.ID)
			} else {
				newWithout = append(newWithout, c.ID)
			}
		}
	}

	m.stableChatOrder = append(newWithMsg, append(m.stableChatOrder, newWithout...)...)
	return m.rebuildChatList()
}

func (m Model) rebuildChatList() Model {
	byID := make(map[string]Chat)
	// Retain previously loaded chats so they don't disappear from the UI
	for _, c := range m.app.Chats {
		byID[c.ID] = c
	}
	// Overwrite/add with fresh chat list data from the API
	for _, c := range m.latestChats {
		byID[c.ID] = c
	}

	// Split into favourites and non-favourites.
	var favChats []Chat
	var normalChats []Chat

	// Non-favourite chats follow the stable order.
	for _, id := range m.stableChatOrder {
		if c, ok := byID[id]; ok {
			if m.favourites[id] {
				favChats = append(favChats, c)
			} else {
				normalChats = append(normalChats, c)
			}
		}
	}

	// Also include favourited chats that are not in stableChatOrder yet
	// (e.g. chats with old activity not loaded from API this session).
	knownInOrder := make(map[string]bool, len(m.stableChatOrder))
	for _, id := range m.stableChatOrder {
		knownInOrder[id] = true
	}
	for id := range m.favourites {
		if !knownInOrder[id] {
			if c, ok := byID[id]; ok {
				favChats = append(favChats, c)
			}
			// If the chat data isn't loaded yet, it simply won't appear until
			// the API returns it. The favourite status is preserved in the file.
		}
	}

	// Sort favourites alphabetically by display name.
	sort.Slice(favChats, func(i, j int) bool {
		namei := ""
		namej := ""
		if favChats[i].CachedDisplayName != nil {
			namei = *favChats[i].CachedDisplayName
		}
		if favChats[j].CachedDisplayName != nil {
			namej = *favChats[j].CachedDisplayName
		}
		return strings.ToLower(namei) < strings.ToLower(namej)
	})

	m.app.Chats = append(favChats, normalChats...)
	return m
}

func (m Model) renderUrlSelection(w, h int) string {
	if len(m.app.UrlsInMessage) == 0 {
		return ""
	}

	title := lipgloss.NewStyle().Foreground(colYellow).Bold(true).Render("Select URL to yank (Enter/y to copy, Esc/q to cancel):")
	var list strings.Builder
	list.WriteString(title + "\n\n")

	for i, u := range m.app.UrlsInMessage {
		prefix := "  "
		style := lipgloss.NewStyle()
		if i == m.app.UrlSelectedIndex {
			prefix = "> "
			style = style.Foreground(colYellow).Background(colDarkGray)
		}

		displayURL := u
		if len(displayURL) > w-10 {
			displayURL = displayURL[:w-13] + "..."
		}
		list.WriteString(style.Render(prefix+displayURL) + "\n")
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colYellow).
		Padding(1, 2).
		Render(list.String())

	return box
}

// ---------------------------------------------------------------------------
// Notifications
// ---------------------------------------------------------------------------

// stripANSI removes ANSI escape codes (including OSC 8 links) from a string.
func stripANSI(s string) string {
	// Standard ANSI CSI sequences + OSC 8 sequences.
	re := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\]8;.*?\x1b\\`)
	return re.ReplaceAllString(s, "")
}

// notify triggers the appropriate notification based on the app's mode.
func (m *Model) notify(senderName string, msg Message) {
	body := ""
	if m.app.NotificationShowPreview {
		if msg.Body != nil && msg.Body.Content != nil {
			body = stripANSI(HTMLToText(*msg.Body.Content, msg.Attachments))
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

// messageMatches reports whether the given message contains the case-insensitive query in its body or attachment names.
func (m Model) messageMatches(msg *Message, query string) bool {
	if query == "" {
		return true
	}
	normQuery := normalizeString(strings.TrimSpace(strings.ToLower(query)))

	// Check body
	text := msg.GetPlainText()
	if strings.Contains(normalizeString(strings.ToLower(text)), normQuery) {
		return true
	}

	// Check attachments
	for _, att := range msg.Attachments {
		if att.Name != nil && strings.Contains(normalizeString(strings.ToLower(*att.Name)), normQuery) {
			return true
		}
	}

	return false
}

// highlightQuery highlights occurrences of query inside text without breaking ANSI sequences.
func highlightQuery(text, query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return text
	}

	lowerText := normalizeString(strings.ToLower(text))
	lowerQuery := normalizeString(strings.ToLower(query))
	if !strings.Contains(lowerText, lowerQuery) {
		return text
	}

	type runeInfo struct {
		isANSI  bool
		val     rune
		bytePos int
	}

	runes := []rune(text)
	infos := make([]runeInfo, 0, len(runes))

	bytePos := 0
	inANSI := false

	for i := 0; i < len(runes); i++ {
		r := runes[i]
		rLen := utf8.RuneLen(r)

		if r == '\x1b' {
			inANSI = true
		}

		infos = append(infos, runeInfo{
			isANSI:  inANSI,
			val:     r,
			bytePos: bytePos,
		})

		if inANSI {
			if r == 'm' && i > 0 && runes[i-1] != '\x1b' {
				inANSI = false
			}
			if r == '\\' && i > 0 && runes[i-1] == '\x1b' {
				inANSI = false
			}
		}

		bytePos += rLen
	}

	var plainText strings.Builder
	var plainToOriginal []int

	for _, info := range infos {
		if !info.isANSI {
			plainText.WriteRune(info.val)
			plainToOriginal = append(plainToOriginal, info.bytePos)
		}
	}

	plainStr := plainText.String()
	plainRunes := []rune(plainStr)
	var plainNormRunes []rune
	// plainNormToOriginalRuneIdx maps the index in plainNormRunes to the index in plainRunes
	plainNormToOriginalRuneIdx := make([]int, 0, len(plainRunes))

	for origRuneIdx, r := range plainRunes {
		normStr := normalizeString(string(r))
		for _, normRune := range normStr {
			plainNormRunes = append(plainNormRunes, normRune)
			plainNormToOriginalRuneIdx = append(plainNormToOriginalRuneIdx, origRuneIdx)
		}
	}

	plainLowerNormRunes := []rune(strings.ToLower(string(plainNormRunes)))
	queryNormRunes := []rune(normalizeString(strings.ToLower(query)))

	var matches [][]int
	if len(queryNormRunes) > 0 {
		for i := 0; i <= len(plainLowerNormRunes)-len(queryNormRunes); i++ {
			match := true
			for j := 0; j < len(queryNormRunes); j++ {
				if plainLowerNormRunes[i+j] != queryNormRunes[j] {
					match = false
					break
				}
			}
			if match {
				startNormIdx := i
				endNormIdx := i + len(queryNormRunes)

				startRuneIdx := plainNormToOriginalRuneIdx[startNormIdx]
				endRuneIdx := len(plainRunes)
				if endNormIdx < len(plainNormToOriginalRuneIdx) {
					endRuneIdx = plainNormToOriginalRuneIdx[endNormIdx]
				}

				matches = append(matches, []int{startRuneIdx, endRuneIdx})
				// Advance index to avoid overlapping matches
				i += len(queryNormRunes) - 1
			}
		}
	}

	if len(matches) == 0 {
		return text
	}

	var result strings.Builder
	lastOrigByte := 0

	for _, match := range matches {
		startRune := match[0]
		endRune := match[1]

		origStartByte := plainToOriginal[startRune]
		origEndByte := len(text)
		if endRune < len(plainToOriginal) {
			origEndByte = plainToOriginal[endRune]
		}

		result.WriteString(text[lastOrigByte:origStartByte])

		matchText := text[origStartByte:origEndByte]
		highlighted := lipgloss.NewStyle().Foreground(colYellow).Bold(true).Render(matchText)
		result.WriteString(highlighted)

		lastOrigByte = origEndByte
	}

	result.WriteString(text[lastOrigByte:])
	return result.String()
}

// RebuildSearchPopupResults filters the history messages by search query, merges context, and updates state.
func (m *Model) RebuildSearchPopupResults() {
	query := strings.TrimSpace(m.searchInput.Value())
	if query == "" {
		m.app.SearchPopupResults = nil
		m.app.SearchPopupSelectedIndex = 0
		return
	}

	chat := m.app.GetSelectedChat()
	if chat == nil {
		return
	}

	// Retrieve all loaded messages for this chat so far
	history, ok := m.app.HistoryMessages[chat.ID]
	if !ok || len(history) == 0 {
		m.app.SearchPopupResults = nil
		m.app.SearchPopupSelectedIndex = 0
		return
	}

	// Save the currently selected message ID to maintain selection focus.
	var prevSelectedID string
	if len(m.app.SearchPopupResults) > 0 && m.app.SearchPopupSelectedIndex < len(m.app.SearchPopupResults) {
		prevSelectedID = m.app.SearchPopupResults[m.app.SearchPopupSelectedIndex].Message.ID
	}
	// Save the first visible message ID to freeze screen position.
	var prevFirstVisibleID string
	if len(m.app.SearchPopupResults) > 0 && m.app.SearchPopupScrollOffset < len(m.app.SearchPopupResults) {
		prevFirstVisibleID = m.app.SearchPopupResults[m.app.SearchPopupScrollOffset].Message.ID
	}

	// First, find all matching indices in the history list.
	matches := make(map[int]bool)
	for i := range history {
		if m.messageMatches(&history[i], query) {
			matches[i] = true
		}
	}

	if len(matches) == 0 {
		m.app.SearchPopupResults = nil
		m.app.SearchPopupSelectedIndex = 0
		return
	}

	// We want to include context messages: x before and x after each match.
	x := ResolveSearchContextLimit()

	// Gather all indices we should include. Use a map to automatically avoid duplicates.
	includedIndices := make(map[int]bool)
	for matchIdx := range matches {
		includedIndices[matchIdx] = true
		for j := 1; j <= x; j++ {
			// Older context
			if matchIdx+j < len(history) {
				includedIndices[matchIdx+j] = true
			}
			// Newer context
			if matchIdx-j >= 0 {
				includedIndices[matchIdx-j] = true
			}
		}
	}

	// Merge expanded indices for this chat
	if state, ok := m.app.SearchStates[chat.ID]; ok && state.ExpandedIndices != nil {
		for idx := range state.ExpandedIndices {
			if idx >= 0 && idx < len(history) {
				includedIndices[idx] = true
			}
		}
	}

	// Sort the included indices. Since history is newest-first (index 0 is newest, len-1 is oldest),
	// if we sort included indices in descending order (e.g. from largest index to smallest),
	// we will render the popup list in chronological order (oldest at the top, newest at the bottom)!
	var sortedIndices []int
	for i := len(history) - 1; i >= 0; i-- {
		if includedIndices[i] {
			sortedIndices = append(sortedIndices, i)
		}
	}

	// Build the items list.
	var items []SearchPopupItem
	for _, idx := range sortedIndices {
		items = append(items, SearchPopupItem{
			Message:      history[idx],
			IsMatch:      matches[idx],
			HistoryIndex: idx,
		})
	}

	m.app.SearchPopupResults = items

	// Restore selection focus by message ID to prevent scrolling/jumping when older history loads
	if prevSelectedID != "" {
		found := false
		for i, item := range items {
			if item.Message.ID == prevSelectedID {
				m.app.SearchPopupSelectedIndex = i
				found = true
				break
			}
		}
		if !found {
			if m.app.SearchPopupSelectedIndex >= len(items) {
				m.app.SearchPopupSelectedIndex = len(items) - 1
			}
		}
	} else {
		if m.app.SearchPopupSelectedIndex >= len(items) {
			m.app.SearchPopupSelectedIndex = len(items) - 1
		}
	}
	if m.app.SearchPopupSelectedIndex < 0 {
		m.app.SearchPopupSelectedIndex = 0
	}

	// Restore scroll offset by message ID to freeze screen position
	if prevFirstVisibleID != "" {
		for i, item := range items {
			if item.Message.ID == prevFirstVisibleID {
				m.app.SearchPopupScrollOffset = i
				break
			}
		}
	}
}

// renderSearchPopup draws the beautiful search interface modal on top of screen.
func (m Model) renderSearchPopup(w, h int) string {
	chat := m.app.GetSelectedChat()
	if chat == nil {
		return ""
	}

	titleStyle := lipgloss.NewStyle().Foreground(colYellow).Bold(true)
	titleText := "Search History (Enter to search)"
	if m.app.SearchQuery != "" {
		titleText = fmt.Sprintf("Search History: %s | Results for '%s'", *chat.CachedDisplayName, m.app.SearchQuery)
	}
	title := titleStyle.Render(titleText)

	instructions := lipgloss.NewStyle().Foreground(colDimGray).Render(
		" j/k: Nav | y: Yank | u: URL | o/Enter: Expand context | /: Edit | Esc: Close",
	)

	var list strings.Builder
	list.WriteString(title + "\n")
	list.WriteString(instructions + "\n\n")

	results := m.app.SearchPopupResults
	msgH := h - 10
	if msgH < 3 {
		msgH = 3
	}

	if len(results) == 0 {
		if m.app.SearchQuery == "" {
			list.WriteString(lipgloss.NewStyle().Foreground(colDimGray).Render("Type a query and press Enter to search.") + "\n")
		} else {
			list.WriteString(lipgloss.NewStyle().Foreground(colDimGray).Render("No matching messages found.") + "\n")
		}
		// Fill remaining lines to keep height stable
		for l := 1; l < msgH; l++ {
			list.WriteString("\n")
		}
	} else {
		// Keep selected index visible
		if m.app.SearchPopupSelectedIndex < m.app.SearchPopupScrollOffset {
			m.app.SearchPopupScrollOffset = m.app.SearchPopupSelectedIndex
		}
		if m.app.SearchPopupScrollOffset < 0 {
			m.app.SearchPopupScrollOffset = 0
		}

		// Adjust scroll offset to keep selected item visible at the bottom:
		for {
			hSum := 0
			for i := m.app.SearchPopupScrollOffset; i <= m.app.SearchPopupSelectedIndex && i < len(results); i++ {
				item := results[i]
				body := item.Message.GetPlainText()
				if item.IsMatch {
					body = highlightQuery(body, m.app.SearchQuery)
				}
				bodyW := w - 8
				if bodyW < 10 {
					bodyW = 10
				}
				bodyLines := wordWrap(body, bodyW)
				hSum += len(bodyLines) + 1 // body lines + 1 header line

				// Add gap indicator height if applicable
				if i > 0 {
					prevItem := results[i-1]
					if prevItem.HistoryIndex-item.HistoryIndex > 1 {
						hSum += 3 // 3 blank/separator lines
					}
				}
			}
			if hSum <= msgH || m.app.SearchPopupScrollOffset >= m.app.SearchPopupSelectedIndex {
				break
			}
			m.app.SearchPopupScrollOffset++
		}

		// Clamp scroll offset to valid bounds
		if m.app.SearchPopupScrollOffset >= len(results) {
			m.app.SearchPopupScrollOffset = len(results) - 1
		}
		if m.app.SearchPopupScrollOffset < 0 {
			m.app.SearchPopupScrollOffset = 0
		}

		// Now render ONLY the visible items starting from SearchPopupScrollOffset
		var visibleLines []string
		linesRendered := 0

		for idx := m.app.SearchPopupScrollOffset; idx < len(results); idx++ {
			item := results[idx]

			// Render gap indicator if applicable
			var gapLines []string
			if idx > 0 {
				prevItem := results[idx-1]
				if prevItem.HistoryIndex-item.HistoryIndex > 1 {
					gapLines = append(gapLines, "")
					gapLines = append(gapLines, lipgloss.NewStyle().Foreground(colDimGray).Render("  ─── [gap in history] ───"))
					gapLines = append(gapLines, "")
				}
			}

			// Render sender + date
			sender := "Unknown"
			if item.Message.From != nil && item.Message.From.User != nil && item.Message.From.User.DisplayName != nil {
				sender = *item.Message.From.User.DisplayName
			}
			msgTime, _ := time.Parse(time.RFC3339Nano, item.Message.CreatedDateTime)
			msgTime = msgTime.Local()
			dateStr := ""
			if !msgTime.IsZero() {
				dateStr = msgTime.Format("2006 Jan 02 15:04")
			}

			isSelected := idx == m.app.SearchPopupSelectedIndex
			prefix := "  "
			if isSelected {
				prefix = "> "
			}

			var header string
			if m.isOwn(item.Message) {
				senderName := "Me"
				if item.IsMatch {
					senderName = highlightQuery(senderName, m.app.SearchQuery)
				}
				header = lipgloss.NewStyle().Foreground(colGreen).Render(prefix + dateStr + " " + senderName)
			} else {
				senderName := sender
				if item.IsMatch {
					senderName = highlightQuery(senderName, m.app.SearchQuery)
				}
				header = lipgloss.NewStyle().Foreground(colCyan).Render(prefix + senderName + " " + dateStr)
			}

			// Render body
			body := item.Message.GetPlainText()
			if item.IsMatch {
				body = highlightQuery(body, m.app.SearchQuery)
			}

			// Wrap body lines
			bodyW := w - 8
			if bodyW < 10 {
				bodyW = 10
			}
			bodyLines := wordWrap(body, bodyW)

			var itemLines []string
			itemLines = append(itemLines, header)
			for _, bl := range bodyLines {
				lineStyle := lipgloss.NewStyle()
				if isSelected {
					lineStyle = lineStyle.Background(colDarkGray)
				}
				itemLines = append(itemLines, lineStyle.Render("    "+bl))
			}

			// Check if we have space to draw this item (or if it's the very first item we must draw it)
			totalNewLines := len(gapLines) + len(itemLines)
			if linesRendered+totalNewLines <= msgH || idx == m.app.SearchPopupScrollOffset {
				visibleLines = append(visibleLines, gapLines...)
				visibleLines = append(visibleLines, itemLines...)
				linesRendered += totalNewLines
			} else {
				// No more room in viewport, stop rendering!
				break
			}
		}

		// Write viewport lines to buffer
		for _, line := range visibleLines {
			list.WriteString(line + "\n")
		}

		// Fill remaining height with blank lines to keep popup height perfectly stable
		for l := linesRendered; l < msgH; l++ {
			list.WriteString("\n")
		}
	}

	m.searchInput.Width = w - 10
	tiView := m.searchInput.View()

	borderCol := colYellow
	if !m.app.SearchMode {
		borderCol = colDimGray
	}

	inputBox := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(borderCol).
		Width(w - 6).Height(3).
		Render(lipgloss.JoinHorizontal(lipgloss.Left,
			lipgloss.NewStyle().Foreground(borderCol).Bold(true).Render("🔍 "),
			tiView,
		))

	// Render status/loader row above the input box
	statusText := ""
	if m.app.SearchStatus != "" {
		statusText = "  " + lipgloss.NewStyle().Foreground(colYellow).Italic(true).Render(m.app.SearchStatus)
	} else if m.app.SearchLoadingMessages {
		statusText = "  " + lipgloss.NewStyle().Foreground(colYellow).Italic(true).Render("⏳ Searching history in background...")
	}

	list.WriteString(statusText + "\n" + inputBox)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colYellow).
		Padding(1, 2).
		Width(w).Height(h).
		Render(list.String())

	return box
}

// handleSearchPopupNavigationKey handles keystrokes inside results list navigation mode.
func (m Model) handleSearchPopupNavigationKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.app.SearchPopupMode = false
		m.app.SearchMode = false
		m.saveSearchState()
		m.app.ScrollOffset = m.app.MainChatScrollOffset
		m.app.SnapToBottom = m.app.MainChatSnapToBottom
		return m, nil

	case "j", "down":
		if m.app.SearchPopupSelectedIndex < len(m.app.SearchPopupResults)-1 {
			m.app.SearchPopupSelectedIndex++
		}
		m.saveSearchState()
		return m, nil

	case "k", "up":
		if m.app.SearchPopupSelectedIndex > 0 {
			m.app.SearchPopupSelectedIndex--
		}
		m.saveSearchState()
		return m, nil

	case "/":
		m.app.SearchMode = true
		m.searchInput.Focus()
		m.saveSearchState()
		return m, textinput.Blink

	case "y":
		if len(m.app.SearchPopupResults) > 0 && m.app.SearchPopupSelectedIndex < len(m.app.SearchPopupResults) {
			msgObj := m.app.SearchPopupResults[m.app.SearchPopupSelectedIndex].Message
			if msgObj.Body != nil && msgObj.Body.Content != nil {
				text := stripANSI(HTMLToText(*msgObj.Body.Content, msgObj.Attachments))
				if err := clipboard.WriteAll(text); err == nil {
					m.app.SetSearchStatus("Message copied to clipboard", 3*time.Second)
				} else {
					m.app.SetSearchStatus("Clipboard error: "+err.Error(), 5*time.Second)
				}
			}
		}
	case "o", "enter":
		if len(m.app.SearchPopupResults) > 0 && m.app.SearchPopupSelectedIndex < len(m.app.SearchPopupResults) {
			item := m.app.SearchPopupResults[m.app.SearchPopupSelectedIndex]
			chat := m.app.GetSelectedChat()
			if chat != nil {
				state, ok := m.app.SearchStates[chat.ID]
				if !ok {
					state = &ChatSearchState{
						ExpandedIndices: make(map[int]bool),
					}
					m.app.SearchStates[chat.ID] = state
				}
				if state.ExpandedIndices == nil {
					state.ExpandedIndices = make(map[int]bool)
				}

				idx := item.HistoryIndex
				history := m.app.HistoryMessages[chat.ID]
				for j := 1; j <= 5; j++ {
					if idx+j < len(history) {
						state.ExpandedIndices[idx+j] = true
					}
					if idx-j >= 0 {
						state.ExpandedIndices[idx-j] = true
					}
				}

				m.RebuildSearchPopupResults()
				m.app.SetSearchStatus("Expanded context by 5 messages before/after", 3*time.Second)
				m.saveSearchState()
			}
		}
		return m, nil

	case "u":
		if len(m.app.SearchPopupResults) > 0 && m.app.SearchPopupSelectedIndex < len(m.app.SearchPopupResults) {
			msgObj := m.app.SearchPopupResults[m.app.SearchPopupSelectedIndex].Message
			if msgObj.Body != nil && msgObj.Body.Content != nil {
				urls := ExtractURLs(*msgObj.Body.Content)
				if len(urls) == 0 {
					m.app.SetSearchStatus("No URLs found in message", 3*time.Second)
				} else if len(urls) == 1 {
					if err := clipboard.WriteAll(urls[0]); err == nil {
						m.app.SetSearchStatus("URL copied to clipboard", 3*time.Second)
					}
				} else {
					m.app.UrlSelectionMode = true
					m.app.UrlSelectedIndex = 0
					m.app.UrlsInMessage = urls
				}
			}
		}
	}

	return m, nil
}

// saveSearchState stores the current chat's active search state to the per-chat SearchStates map.
func (m *Model) saveSearchState() {
	chat := m.app.GetSelectedChat()
	if chat == nil {
		return
	}
	state, ok := m.app.SearchStates[chat.ID]
	if !ok {
		state = &ChatSearchState{
			ExpandedIndices: make(map[int]bool),
		}
		m.app.SearchStates[chat.ID] = state
	}
	if state.ExpandedIndices == nil {
		state.ExpandedIndices = make(map[int]bool)
	}
	state.Query = m.app.SearchQuery
	state.Results = m.app.SearchPopupResults
	state.SelectedIndex = m.app.SearchPopupSelectedIndex
	state.ScrollOffset = m.app.SearchPopupScrollOffset
}

// loadSearchState restores the search state for the selected chat into active fields.
func (m *Model) loadSearchState() {
	chat := m.app.GetSelectedChat()
	if chat == nil {
		return
	}
	state, ok := m.app.SearchStates[chat.ID]
	if !ok {
		m.app.SearchQuery = ""
		m.app.SearchPopupResults = nil
		m.app.SearchPopupSelectedIndex = 0
		m.app.SearchPopupScrollOffset = 0
		return
	}
	m.app.SearchQuery = state.Query
	m.app.SearchPopupResults = state.Results
	m.app.SearchPopupSelectedIndex = state.SelectedIndex
	m.app.SearchPopupScrollOffset = state.ScrollOffset
}

// getReactionKey creates a unique identifier for a reaction.
func getReactionKey(msgID string, r MessageReaction) string {
	userID := ""
	if r.User != nil && r.User.User != nil {
		if r.User.User.ID != nil {
			userID = *r.User.User.ID
		} else if r.User.User.DisplayName != nil {
			userID = *r.User.User.DisplayName
		}
	}
	return msgID + ":" + userID + ":" + r.ReactionType
}

// isOwnReaction checks if the reaction was added by the current user.
func (m Model) isOwnReaction(r MessageReaction) bool {
	if r.User == nil || r.User.User == nil {
		return false
	}
	if m.userID != "" && r.User.User.ID != nil && *r.User.User.ID == m.userID {
		return true
	}
	if m.app.CurrentUserName != nil && r.User.User.DisplayName != nil && *r.User.User.DisplayName == *m.app.CurrentUserName {
		return true
	}
	return false
}

// isOldReaction checks if the reaction was created before the app started.
func (m Model) isOldReaction(msg Message, r MessageReaction) bool {
	if r.CreatedDateTime != nil {
		if t, err := time.Parse(time.RFC3339, *r.CreatedDateTime); err == nil {
			return t.Before(m.app.AppStartTime)
		}
	}
	if t, err := time.Parse(time.RFC3339Nano, msg.CreatedDateTime); err == nil {
		return t.Before(m.app.AppStartTime)
	}
	return true
}

// getReactionKeys extracts unique reaction keys from a message, ignoring the current user's reactions.
func (m Model) getReactionKeys(msg *Message) []string {
	if msg == nil || len(msg.Reactions) == 0 {
		return nil
	}
	var keys []string
	for _, r := range msg.Reactions {
		if m.isOwnReaction(r) {
			continue
		}
		keys = append(keys, getReactionKey(msg.ID, r))
	}
	return keys
}

// hasUnreadReactions checks if the chat has any new reactions that the user has not seen.
func (m Model) hasUnreadReactions(c Chat) bool {
	return m.getLatestUnreadReactionEmoji(c) != ""
}

// getLatestUnreadReactionEmoji checks if the chat has any new reactions and returns the emoji of the most recent one.
func (m Model) getLatestUnreadReactionEmoji(c Chat) string {
	if chat := m.app.GetSelectedChat(); chat != nil && chat.ID == c.ID && m.focused {
		return ""
	}

	// Check reactions on LastMessagePreview first (as it represents the latest message)
	if c.LastMessagePreview != nil {
		for _, rKey := range m.getReactionKeys(c.LastMessagePreview) {
			if readMap, ok := m.lastReadReactions[c.ID]; !ok || !readMap[rKey] {
				for _, r := range c.LastMessagePreview.Reactions {
					if getReactionKey(c.LastMessagePreview.ID, r) == rKey {
						return reactionEmoji(r.ReactionType)
					}
				}
			}
		}
	}

	// Check reactions on cached HistoryMessages if we have them
	if hist, ok := m.app.HistoryMessages[c.ID]; ok {
		for _, msg := range hist {
			for _, rKey := range m.getReactionKeys(&msg) {
				if readMap, ok := m.lastReadReactions[c.ID]; !ok || !readMap[rKey] {
					for _, r := range msg.Reactions {
						if getReactionKey(msg.ID, r) == rKey {
							return reactionEmoji(r.ReactionType)
						}
					}
				}
			}
		}
	}

	return ""
}

// notifyReaction triggers a notification for newly detected reactions.
func (m *Model) notifyReaction(chat Chat, msg *Message, newReactions []MessageReaction) {
	chatName := ""
	if chat.CachedDisplayName != nil {
		chatName = *chat.CachedDisplayName
	}

	for _, r := range newReactions {
		reactorName := "Someone"
		if r.User != nil && r.User.User != nil && r.User.User.DisplayName != nil {
			reactorName = *r.User.User.DisplayName
		}

		emoji := reactionEmoji(r.ReactionType)
		msgBody := msg.GetPlainText()
		msgBody = stripANSI(msgBody)
		msgBody = strings.ReplaceAll(msgBody, "\n", " ")
		msgBody = strings.Join(strings.Fields(msgBody), " ")
		if len(msgBody) > 40 {
			msgBody = string([]rune(msgBody)[:40]) + "..."
		}

		title := fmt.Sprintf("TeamsTUI: %s", reactorName)
		body := fmt.Sprintf("Reacted %s to: \"%s\"", emoji, msgBody)
		if chatName != "" && chatName != reactorName {
			body = fmt.Sprintf("Reacted %s to \"%s\" in %s", emoji, msgBody, chatName)
		}

		switch m.app.NotificationMode {
		case NotificationConsole:
			fmt.Print("\a") // BEL
			m.app.TriggerVisualBell()
		case NotificationSystem:
			beeep.AppName = "TeamsTUI"
			_ = beeep.Notify(title, body, "")
		case NotificationBoth:
			fmt.Print("\a")
			m.app.TriggerVisualBell()
			beeep.AppName = "TeamsTUI"
			_ = beeep.Notify(title, body, "")
		}
	}
}

// applyPendingEdits patches any in-memory pending edits into the live message
// caches for the given chat. It also removes entries from pendingEdits once
// the API has confirmed the updated content (i.e. the slice already contains
// the new HTML), preventing stale overrides from lingering indefinitely.
func (m *Model) applyPendingEdits(chatID string) {
	if len(m.pendingEdits) == 0 {
		return
	}
	patchSlice := func(msgs []Message) {
		for i := range msgs {
			newHTML, isPending := m.pendingEdits[msgs[i].ID]
			if !isPending {
				continue
			}
			// Check if the API has already reflected this edit.
			if msgs[i].Body != nil && msgs[i].Body.Content != nil && *msgs[i].Body.Content == newHTML {
				delete(m.pendingEdits, msgs[i].ID)
				continue
			}
			// Overwrite with our optimistic content.
			if msgs[i].Body == nil {
				body := MessageBody{Content: &newHTML}
				msgs[i].Body = &body
			} else {
				msgs[i].Body.Content = &newHTML
			}
			msgs[i].PlainTextCached = nil // force re-render
		}
	}
	patchSlice(m.app.Messages)
	if cached, ok := m.app.CachedMessages[chatID]; ok {
		patchSlice(cached)
	}
	if hist, ok := m.app.HistoryMessages[chatID]; ok {
		patchSlice(hist)
	}
}

// mergeHistoryMessages combines existing history messages with newly fetched ones,
// updating existing ones and prepending/sorting them newest-first.
func mergeHistoryMessages(existing []Message, newMsgs []Message) []Message {
	msgMap := make(map[string]Message)
	for _, msg := range existing {
		msgMap[msg.ID] = msg
	}
	for _, msg := range newMsgs {
		msgMap[msg.ID] = msg
	}

	var merged []Message
	for _, msg := range msgMap {
		merged = append(merged, msg)
	}

	sort.Slice(merged, func(i, j int) bool {
		return merged[i].CreatedDateTime > merged[j].CreatedDateTime
	})
	return merged
}

type UserSearchItemType int

const (
	UserSearchItemLocal UserSearchItemType = iota
	UserSearchItemDirectory
	UserSearchItemDirect
)

type UserSearchItem struct {
	Type        UserSearchItemType
	LocalChat   *Chat
	DirUser     *User
	DirectEmail string
}

func (m Model) getUserSearchItems() []UserSearchItem {
	var items []UserSearchItem

	// Only Local results
	for i := range m.app.UserSearchLocalResults {
		items = append(items, UserSearchItem{
			Type:      UserSearchItemLocal,
			LocalChat: &m.app.UserSearchLocalResults[i],
		})
	}

	return items
}

func (m *Model) updateUserSearchLocalResults() {
	query := normalizeString(strings.ToLower(strings.TrimSpace(m.app.UserSearchQuery)))
	if query == "" {
		m.app.UserSearchLocalResults = nil
		return
	}

	var matches []Chat
	for _, c := range m.app.Chats {
		name := ""
		if c.CachedDisplayName != nil {
			name = normalizeString(strings.ToLower(*c.CachedDisplayName))
		}

		memberMatch := false
		for _, mem := range c.Members {
			if mem.DisplayName != nil && strings.Contains(normalizeString(strings.ToLower(*mem.DisplayName)), query) {
				memberMatch = true
				break
			}
			if mem.Email != nil && strings.Contains(normalizeString(strings.ToLower(*mem.Email)), query) {
				memberMatch = true
				break
			}
		}

		if strings.Contains(name, query) || memberMatch {
			matches = append(matches, c)
		}
	}
	m.app.UserSearchLocalResults = matches
}

func (m Model) handleUserSearchInputModeKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.app.UserSearchMode = false
		m.userSearchInput.Blur()
		return m, nil

	case "down", "up", "tab":
		m.app.UserSearchMode = false
		m.userSearchInput.Blur()
		m.app.UserSearchSelectedIndex = 0
		return m, nil

	case "enter":
		query := strings.TrimSpace(m.userSearchInput.Value())
		m.app.UserSearchMode = false
		m.userSearchInput.Blur()
		if query != "" {
			if strings.Contains(query, "@") {
				m.app.UserSearchLoading = true
				m.app.UserSearchStatus = "Opening chat..."
				return m, createChatCmd(m.clientID, m.userID, query)
			}
		}
		return m, nil
	}

	return m, nil
}

func (m Model) handleUserSearchNavigationKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	items := m.getUserSearchItems()

	switch msg.String() {
	case "esc", "q":
		m.app.UserSearchPopupMode = false
		m.app.UserSearchMode = false
		return m, nil

	case "j", "down":
		if len(items) > 0 && m.app.UserSearchSelectedIndex < len(items)-1 {
			m.app.UserSearchSelectedIndex++
		}
		return m, nil

	case "k", "up":
		if len(items) > 0 && m.app.UserSearchSelectedIndex > 0 {
			m.app.UserSearchSelectedIndex--
		}
		return m, nil

	case "/":
		m.app.UserSearchMode = true
		m.userSearchInput.Focus()
		return m, textinput.Blink

	case "enter":
		if len(items) == 0 || m.app.UserSearchSelectedIndex >= len(items) {
			return m, nil
		}

		item := items[m.app.UserSearchSelectedIndex]
		if item.Type == UserSearchItemLocal {
			targetID := item.LocalChat.ID
			idx := -1
			for i, c := range m.app.Chats {
				if c.ID == targetID {
					idx = i
					break
				}
			}
			if idx != -1 {
				m.app.SelectedIndex = idx
				m.app.UserSearchPopupMode = false
				m.app.UserSearchMode = false
				m.app.Messages = nil
				m.app.NextLink = ""
				m.app.SetLoadingMessages(true)
				m.app.SnapToBottom = true
				m.lastMessageRefresh = time.Now()
				return m, loadMessagesCmd(m.clientID, targetID, idx)
			}
		}
	}

	return m, nil
}

func (m Model) renderUserSearchPopup(w, h int) string {
	titleStyle := lipgloss.NewStyle().Foreground(colCyan).Bold(true)
	title := titleStyle.Render("Find Local Chat or Start Direct Chat")

	instructions := lipgloss.NewStyle().Foreground(colDimGray).Render(
		" j/k: Nav | Enter: Open selected chat or typed email | /: Edit | Esc: Close",
	)

	var list strings.Builder
	list.WriteString(title + "\n")
	list.WriteString(instructions + "\n\n")

	items := m.getUserSearchItems()
	msgH := h - 10
	if msgH < 3 {
		msgH = 3
	}

	if len(items) == 0 {
		if m.app.UserSearchQuery == "" {
			list.WriteString(lipgloss.NewStyle().Foreground(colDimGray).Render("Type a name/email and press Enter/arrows.") + "\n")
		} else {
			list.WriteString(lipgloss.NewStyle().Foreground(colDimGray).Render("No matching local chats found.") + "\n")
		}
		for l := 1; l < msgH; l++ {
			list.WriteString("\n")
		}
	} else {
		if m.app.UserSearchSelectedIndex >= len(items) {
			m.app.UserSearchSelectedIndex = len(items) - 1
		}
		if m.app.UserSearchSelectedIndex < 0 {
			m.app.UserSearchSelectedIndex = 0
		}

		linesRendered := 0
		for idx, item := range items {
			isSelected := idx == m.app.UserSearchSelectedIndex
			prefix := "  "
			if isSelected {
				prefix = "> "
			}

			var line string
			switch item.Type {
			case UserSearchItemLocal:
				chatName := "Unknown"
				if item.LocalChat.CachedDisplayName != nil {
					chatName = *item.LocalChat.CachedDisplayName
				}
				tag := lipgloss.NewStyle().Foreground(colGreen).Render("[Local Chat]")
				lineStr := fmt.Sprintf("%s %s %s", prefix, chatName, tag)
				if isSelected {
					line = lipgloss.NewStyle().Background(colDarkGray).Render(lineStr)
				} else {
					line = lineStr
				}
			}

			list.WriteString(line + "\n")
			linesRendered++
			if linesRendered >= msgH {
				break
			}
		}

		for l := linesRendered; l < msgH; l++ {
			list.WriteString("\n")
		}
	}

	m.userSearchInput.Width = w - 10
	tiView := m.userSearchInput.View()

	borderCol := colCyan
	if !m.app.UserSearchMode {
		borderCol = colDimGray
	}

	inputBox := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(borderCol).
		Width(w - 6).Height(3).
		Render(lipgloss.JoinHorizontal(lipgloss.Left,
			lipgloss.NewStyle().Foreground(borderCol).Bold(true).Render("🔍 "),
			tiView,
		))

	statusText := ""
	if m.app.UserSearchStatus != "" {
		statusText = "  " + lipgloss.NewStyle().Foreground(colYellow).Italic(true).Render(m.app.UserSearchStatus)
	} else if m.app.UserSearchLoading {
		statusText = "  " + lipgloss.NewStyle().Foreground(colYellow).Italic(true).Render("⏳ Opening chat...")
	}

	list.WriteString(statusText + "\n" + inputBox)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colCyan).
		Padding(1, 2).
		Width(w).Height(h).
		Render(list.String())

	return box
}

func (m Model) renderMessagePopup(w, h int) string {
	if len(m.app.Messages) == 0 || m.app.MessageSelectedIndex < 0 || m.app.MessageSelectedIndex >= len(m.app.Messages) {
		return ""
	}

	msg := m.app.Messages[m.app.MessageSelectedIndex]

	sender := "Unknown"
	if msg.From != nil && msg.From.User != nil && msg.From.User.DisplayName != nil {
		sender = *msg.From.User.DisplayName
		if m.app.CurrentUserName != nil && sender == *m.app.CurrentUserName {
			sender = "Me"
		}
	}

	msgTime, _ := time.Parse(time.RFC3339Nano, msg.CreatedDateTime)
	msgTime = msgTime.Local()
	timeStr := ""
	if !msgTime.IsZero() {
		timeStr = msgTime.Format("Jan 02, 2006 15:04:05")
	}

	innerW := w - 6
	innerH := h - 4
	if innerW < 10 {
		innerW = 10
	}
	if innerH < 4 {
		innerH = 4
	}

	headerLines := []string{
		lipgloss.NewStyle().Foreground(colCyan).Bold(true).Render("From: ") + sender,
		lipgloss.NewStyle().Foreground(colCyan).Bold(true).Render("Date: ") + timeStr,
		"",
	}

	attachmentsLines := []string{}
	if len(msg.Attachments) > 0 {
		attachmentsLines = append(attachmentsLines, lipgloss.NewStyle().Foreground(colYellow).Bold(true).Render("Attachments:"))
		for _, att := range msg.Attachments {
			name := "Unnamed attachment"
			if att.Name != nil && *att.Name != "" {
				name = *att.Name
			}
			contentType := ""
			if att.ContentType != nil && *att.ContentType != "" {
				contentType = fmt.Sprintf(" (%s)", *att.ContentType)
			}
			attachmentsLines = append(attachmentsLines, fmt.Sprintf("  📎 %s%s", name, contentType))
		}
	}

	reactionsLines := []string{}
	reactionsGrouped := make(map[string][]string)
	var reactionOrder []string
	seenReactions := make(map[string]bool)

	for _, r := range msg.Reactions {
		rType := strings.ToLower(r.ReactionType)
		emoji := reactionEmoji(rType)
		name := m.resolveReactorName(r)

		reactionsGrouped[emoji] = append(reactionsGrouped[emoji], name)
		if !seenReactions[emoji] {
			seenReactions[emoji] = true
			reactionOrder = append(reactionOrder, emoji)
		}
	}

	if len(msg.Reactions) > 0 {
		reactionsLines = append(reactionsLines, lipgloss.NewStyle().Foreground(colYellow).Bold(true).Render("Reactions:"))
		for _, emoji := range reactionOrder {
			names := reactionsGrouped[emoji]
			sort.Strings(names)
			namesStr := strings.Join(names, ", ")
			wrappedReactors := wordWrap(namesStr, innerW-6)
			for i, wrLine := range wrappedReactors {
				if i == 0 {
					reactionsLines = append(reactionsLines, fmt.Sprintf("  %s %s", emoji, wrLine))
				} else {
					reactionsLines = append(reactionsLines, fmt.Sprintf("    %s", wrLine))
				}
			}
		}
	}

	footer := lipgloss.NewStyle().Foreground(colDimGray).Italic(true).Render("Press ESC/q/v/Enter to close | j/k to navigate | J/K to scroll")

	nonBodyH := len(headerLines) + 1
	if len(attachmentsLines) > 0 {
		nonBodyH += len(attachmentsLines) + 1
	}
	if len(reactionsLines) > 0 {
		nonBodyH += len(reactionsLines) + 1
	}

	bodyMaxH := innerH - nonBodyH
	if bodyMaxH < 4 {
		bodyMaxH = 4
	}

	body := msg.GetPlainText()
	var wrappedBody []string
	if body != "" {
		wrappedBody = wordWrap(body, innerW)
	}

	var bodyLines []string
	if len(wrappedBody) > 0 {
		viewportH := bodyMaxH - 2
		if viewportH < 1 {
			viewportH = 1
		}

		maxScroll := len(wrappedBody) - viewportH
		if maxScroll < 0 {
			maxScroll = 0
		}
		if m.app.MessagePopupScrollOffset > maxScroll {
			m.app.MessagePopupScrollOffset = maxScroll
		}
		if m.app.MessagePopupScrollOffset < 0 {
			m.app.MessagePopupScrollOffset = 0
		}

		visibleBody := wrappedBody
		if len(wrappedBody) > viewportH {
			start := m.app.MessagePopupScrollOffset
			end := start + viewportH
			if end > len(wrappedBody) {
				end = len(wrappedBody)
			}
			visibleBody = wrappedBody[start:end]
		}

		headerText := lipgloss.NewStyle().Foreground(colYellow).Bold(true).Render("Message:")
		if len(wrappedBody) > viewportH {
			headerText += lipgloss.NewStyle().Foreground(colDimGray).Render(fmt.Sprintf(" (Shift+J/K to scroll - %d/%d)", m.app.MessagePopupScrollOffset+1, len(wrappedBody)))
		}
		bodyLines = append(bodyLines, headerText)
		bodyLines = append(bodyLines, visibleBody...)
		bodyLines = append(bodyLines, "")
	}

	var finalLines []string
	finalLines = append(finalLines, headerLines...)
	finalLines = append(finalLines, bodyLines...)
	if len(attachmentsLines) > 0 {
		finalLines = append(finalLines, attachmentsLines...)
		finalLines = append(finalLines, "")
	}
	if len(reactionsLines) > 0 {
		finalLines = append(finalLines, reactionsLines...)
		finalLines = append(finalLines, "")
	}

	targetH := innerH - 1
	if len(finalLines) > targetH {
		finalLines = finalLines[:targetH]
	} else {
		for len(finalLines) < targetH {
			finalLines = append(finalLines, "")
		}
	}
	finalLines = append(finalLines, footer)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colCyan).
		Padding(1, 2).
		Width(w).Height(h).
		Render(strings.Join(finalLines, "\n"))

	return box
}

func (m Model) resolveReactorName(r MessageReaction) string {
	if r.User == nil || r.User.User == nil {
		return "Someone"
	}

	if r.User.User.ID != nil && *r.User.User.ID != "" && m.app.CurrentUserID != "" && *r.User.User.ID == m.app.CurrentUserID {
		return "Me"
	}
	if r.User.User.DisplayName != nil && *r.User.User.DisplayName != "" {
		if m.app.CurrentUserName != nil && *r.User.User.DisplayName == *m.app.CurrentUserName {
			return "Me"
		}
	}

	if r.User.User.DisplayName != nil && *r.User.User.DisplayName != "" {
		return *r.User.User.DisplayName
	}

	if r.User.User.ID != nil && *r.User.User.ID != "" {
		if chat := m.app.GetSelectedChat(); chat != nil {
			for _, member := range chat.Members {
				match := false
				if member.UserID != nil && *member.UserID == *r.User.User.ID {
					match = true
				} else if member.ID != nil && *member.ID == *r.User.User.ID {
					match = true
				}
				if match {
					if member.DisplayName != nil && *member.DisplayName != "" {
						return *member.DisplayName
					}
				}
			}
		}
	}

	if r.User.User.ID != nil && *r.User.User.ID != "" {
		return *r.User.User.ID
	}

	return "Someone"
}
