package main

import (
	"fmt"
	"net/http"
	"os"
	"sort"

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
func loadChatsCmd(clientID string, existingChats []Chat, currentUserName *string) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return MsgChatsLoaded{Chats: existingChats, CurrentUserName: currentUserName}
		}
		chats, currentUser, err := GetChats(token, existingChats, currentUserName)
		if err != nil {
			return MsgChatsLoaded{Chats: existingChats, CurrentUserName: currentUserName}
		}
		return MsgChatsLoaded{Chats: chats, CurrentUserName: currentUser}
	}
}

// loadBackgroundMessagesCmd fetches the latest 10 messages for a chat to inspect reactions.
func loadBackgroundMessagesCmd(clientID, chatID string) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return nil
		}
		msgs, _, err := GetMessages(token, chatID, 10)
		if err != nil {
			return nil
		}
		return MsgBackgroundMessagesLoaded{ChatID: chatID, Messages: msgs}
	}
}

// pollReactionsCmd fetches the latest 10 messages for a list of chats in parallel.
func pollReactionsCmd(clientID string, chatIDs []string) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return nil
		}

		results := make(map[string][]Message)
		type fetchResult struct {
			chatID string
			msgs   []Message
		}
		ch := make(chan fetchResult, len(chatIDs))
		for _, id := range chatIDs {
			go func(chatID string) {
				msgs, _, err := GetMessages(token, chatID, 10)
				if err != nil {
					ch <- fetchResult{chatID, nil}
					return
				}
				ch <- fetchResult{chatID, msgs}
			}(id)
		}
		for range chatIDs {
			res := <-ch
			if res.msgs != nil {
				results[res.chatID] = res.msgs
			}
		}
		return MsgPollReactionsLoaded{Results: results}
	}
}

// loadMessagesCmd fetches messages for a specific chat in the background.
func loadMessagesCmd(clientID, chatID string, chatIndex int) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return nil
		}
		msgs, next, err := GetMessages(token, chatID, ResolveMessageLimit())
		if err != nil {
			return nil
		}
		return MsgMessagesLoaded{ChatIndex: chatIndex, Messages: msgs, NextLink: next}
	}
}

// loadMoreMessagesCmd fetches the next page of messages using a nextLink.
func loadMoreMessagesCmd(clientID, nextLink string, chatIndex int, isSearch bool) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return nil
		}
		msgs, next, err := GetMessagesFromLink(token, nextLink)
		if err != nil {
			return nil
		}
		return MsgMoreMessagesLoaded{ChatIndex: chatIndex, Messages: msgs, NextLink: next, IsSearch: isSearch}
	}
}

// searchUsersCmd searches the directory for users by name or email.
func searchUsersCmd(clientID, query string) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return MsgUserSearchDone{Err: err}
		}
		users, err := SearchUsers(token, query)
		return MsgUserSearchDone{Users: users, Err: err}
	}
}

// createChatCmd creates or retrieves a 1-on-1 chat with a user by UPN in the background.
func createChatCmd(clientID, myUserID, otherUPN string) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return MsgCreateChatDone{Err: err}
		}
		chat, err := GetOrCreateOneOnOneChat(token, myUserID, otherUPN)
		return MsgCreateChatDone{Chat: chat, Err: err}
	}
}

// sendMessageCmd sends a message to a chat in the background.
func sendMessageCmd(clientID, chatID, content string, images []PastedImage) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return MsgSendDone{Err: err}
		}
		err = SendMessage(token, chatID, content, images)
		return MsgSendDone{Err: err}
	}
}

// sendChannelMessageCmd sends a message to a Teams channel in the background.
func sendChannelMessageCmd(clientID, teamID, channelID, content string, images []PastedImage) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return MsgSendDone{Err: err}
		}
		err = SendChannelMessage(token, teamID, channelID, content, images)
		return MsgSendDone{Err: err}
	}
}

// sendChannelReplyCmd posts a reply into an existing Teams channel thread.
func sendChannelReplyCmd(clientID, teamID, channelID, rootMsgID, content string, images []PastedImage) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return MsgSendDone{Err: err}
		}
		err = SendChannelReply(token, teamID, channelID, rootMsgID, content, images)
		return MsgSendDone{Err: err}
	}
}

// sendMessageWithRefCmd sends a reply message with a Teams messageReference attachment.
func sendMessageWithRefCmd(clientID, chatID, content string, ref *Message, images []PastedImage) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return MsgSendDone{Err: err}
		}
		err = SendMessageWithReference(token, chatID, ref, content, images)
		return MsgSendDone{Err: err}
	}
}

// setReactionCmd adds a reaction to a chat message in the background.
func setReactionCmd(clientID, chatID, messageID, reactionType string) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return MsgSendDone{Err: err}
		}
		err = SetReaction(token, chatID, messageID, reactionType)
		return MsgSendDone{Err: err}
	}
}

// setChannelReactionCmd adds a reaction to a Teams channel message in the background.
func setChannelReactionCmd(clientID, teamID, channelID, messageID, reactionType string) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return MsgSendDone{Err: err}
		}
		err = SetChannelReaction(token, teamID, channelID, messageID, reactionType)
		return MsgSendDone{Err: err}
	}
}

// unsetReactionCmd removes a reaction from a chat message in the background.
func unsetReactionCmd(clientID, chatID, messageID, reactionType string) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return MsgSendDone{Err: err}
		}
		err = UnsetReaction(token, chatID, messageID, reactionType)
		return MsgSendDone{Err: err}
	}
}

// unsetChannelReactionCmd removes a reaction from a Teams channel message in the background.
func unsetChannelReactionCmd(clientID, teamID, channelID, messageID, reactionType string) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return MsgSendDone{Err: err}
		}
		err = UnsetChannelReaction(token, teamID, channelID, messageID, reactionType)
		return MsgSendDone{Err: err}
	}
}

// deleteMessageCmd removes a chat message in the background.
func deleteMessageCmd(clientID, chatID, messageID string) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return MsgSendDone{Err: err}
		}
		err = DeleteMessage(token, chatID, messageID)
		return MsgSendDone{Err: err}
	}
}

// deleteChannelMessageCmd removes a Teams channel message in the background.
func deleteChannelMessageCmd(clientID, teamID, channelID, messageID string) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return MsgSendDone{Err: err}
		}
		err = DeleteChannelMessage(token, teamID, channelID, messageID)
		return MsgSendDone{Err: err}
	}
}

// updateMessageCmd modifies a chat message in the background.
func updateMessageCmd(clientID, chatID, messageID, content string) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return MsgEditDone{ChatID: chatID, MessageID: messageID, Content: content, Err: err}
		}
		err = UpdateMessage(token, chatID, messageID, content)
		return MsgEditDone{ChatID: chatID, MessageID: messageID, Content: content, Err: err}
	}
}

// updateChannelMessageCmd modifies a Teams channel message in the background.
func updateChannelMessageCmd(clientID, teamID, channelID, messageID, content string) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return MsgEditDone{ChatID: channelID, MessageID: messageID, Content: content, Err: err}
		}
		err = UpdateChannelMessage(token, teamID, channelID, messageID, content)
		return MsgEditDone{ChatID: channelID, MessageID: messageID, Content: content, Err: err}
	}
}

// loadPresenceCmd fetches the presence status for a user by their Azure AD user ID.
// Requires Presence.Read.All scope; returns MsgPresenceLoaded.
func loadPresenceCmd(clientID, userID, displayName string) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return MsgPresenceLoaded{UserID: userID, Err: err}
		}
		p, err := GetUserPresence(token, userID)
		return MsgPresenceLoaded{UserID: userID, Presence: p, Err: err}
	}
}

// loadUserProfileCmd fetches the full profile for a user by their Azure AD user ID.
// Requires User.ReadBasic.All (or User.Read.All for extended info); returns MsgUserProfileLoaded.
func loadUserProfileCmd(clientID, userID string) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return MsgUserProfileLoaded{UserID: userID, Err: err}
		}
		p, err := GetUserProfile(token, userID)
		return MsgUserProfileLoaded{UserID: userID, Profile: p, Err: err}
	}
}

// downloadFileCmd downloads a file attachment to destPath.
// Requires Files.Read scope; returns MsgFileDownloaded.
func downloadFileCmd(clientID, fileURL, destPath string) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return MsgFileDownloaded{Err: err}
		}
		err = DownloadFile(token, fileURL, destPath)
		return MsgFileDownloaded{DestPath: destPath, Err: err}
	}
}

// loadTeamsChannelsCmd fetches the list of joined Teams with their channels.
// Requires Team.ReadBasic.All + Channel.ReadBasic.All scopes; returns MsgTeamsChannelsLoaded.
func loadTeamsChannelsCmd(clientID string) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return MsgTeamsChannelsLoaded{Err: err}
		}
		teams, err := GetTeamsWithChannels(token)
		return MsgTeamsChannelsLoaded{Teams: teams, Err: err}
	}
}

// loadChannelMessagesCmd fetches messages for a specific Teams channel.
// Requires ChannelMessage.Read.All scope; returns MsgChannelMessagesLoaded.
func loadChannelMessagesCmd(clientID, teamID, channelID string) tea.Cmd {
	return func() tea.Msg {
		token, err := GetValidTokenSilent(clientID)
		if err != nil {
			return MsgChannelMessagesLoaded{TeamID: teamID, ChannelID: channelID, Err: err}
		}
		msgs, next, err := GetChannelMessages(token, teamID, channelID, ResolveMessageLimit())
		return MsgChannelMessagesLoaded{TeamID: teamID, ChannelID: channelID, Messages: msgs, NextLink: next, Err: err}
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

// loadInitialChatOrder returns the chats sorted by most recent message timestamp (descending),
// using the pre-fetched LastMessagePreview field, along with the last message IDs and timestamps.
func loadInitialChatOrder(chats []Chat) ([]Chat, map[string]string, map[string]time.Time) {
	lastMsgIDs := make(map[string]string)
	lastMsgTimes := make(map[string]time.Time)

	type chatWithTime struct {
		chat Chat
		t    time.Time
	}
	combined := make([]chatWithTime, len(chats))
	for i, c := range chats {
		t := time.Time{}
		if c.LastMessagePreview != nil {
			t, _ = time.Parse(time.RFC3339Nano, c.LastMessagePreview.CreatedDateTime)
			lastMsgIDs[c.ID] = c.LastMessagePreview.ID
			lastMsgTimes[c.ID] = t
		}

		// Fallback/override: if LastUpdated is set and is newer, use it.
		if c.LastUpdated != nil {
			lut, _ := time.Parse(time.RFC3339Nano, *c.LastUpdated)
			if lut.After(t) {
				t = lut
				lastMsgTimes[c.ID] = t
			}
		}
		combined[i] = chatWithTime{c, t}
	}

	// Sort by latest message timestamp descending.
	sort.Slice(combined, func(a, b int) bool {
		ta := combined[a].t
		tb := combined[b].t
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

	sorted := make([]Chat, len(chats))
	for i, cw := range combined {
		sorted[i] = cw.chat
	}

	return sorted, lastMsgIDs, lastMsgTimes
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

var version = "dev"

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "preview-image" {
		previewImage(os.Args[2])
		os.Exit(0)
	}

	// Set default HTTP client timeout to prevent background refreshes from hanging indefinitely.
	http.DefaultClient.Timeout = 15 * time.Second

	// 1. Banner.
	fmt.Printf("TeamsTUI %s\n", version)
	fmt.Println("================================")

	// Initialize configuration and write defaults for any missing keys.
	InitConfig()

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
	chats, currentUserName, err := GetChats(accessToken, nil, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not fetch chats: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Loaded %d chats\n", len(chats))

	// 5 & 6. Sort chats by most recent message.
	var lastMsgIDs map[string]string
	var lastMsgTimes map[string]time.Time
	chats, lastMsgIDs, lastMsgTimes = loadInitialChatOrder(chats)

	// 7 & 8. Initialise application state.
	app := NewApp()
	app.SetChats(chats)
	if currentUserName != nil {
		app.SetCurrentUser(*currentUserName)
	}
	app.CurrentUserID = me.ID

	// Load persisted notification mode and settings.
	app.ChannelMsgRefreshMin = ResolveChannelMsgRefreshMin()
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
		if cfg.ChatIconTheme != nil {
			app.ChatIconTheme = *cfg.ChatIconTheme
		}
		if cfg.CustomChatIcons != nil {
			app.CustomChatIcons = cfg.CustomChatIcons
		}
	}

	// Resolve optional feature flags once at startup.
	app.Features = FeatureFlags{
		FilePreview:           ResolveFeatureFilePreview(),
		FilePreviewInTerminal: ResolveFeatureFilePreview() && ResolveFeatureFilePreviewInTerminal(),
		Presence:              ResolveFeaturePresence(),
		UserProfile:           ResolveFeatureUserProfile(),
		ProfileExtended:       ResolveFeatureUserProfileExtended(),
		TeamsChannels:         ResolveFeatureTeamsChannels(),
	}

	// Build initial stable chat order.
	model := NewModel(app, clientID, me.ID)
	// Chats are already loaded synchronously above; set lastChatRefresh so the
	// first tick-driven background refresh fires ~15 s from now rather than
	// immediately (Init() no longer fires a redundant loadChatsCmd).
	model.lastChatRefresh = time.Now()
	model.lastChannelRefresh = time.Now()
	model.latestChats = chats
	model.lastMsgID = lastMsgIDs
	model.lastMsgTime = lastMsgTimes
	model.lastReadReactions = make(map[string]map[string]bool)
	model.reactionsInitialized = make(map[string]bool)
	model.notifiedReactions = make(map[string]map[string]bool)
	for _, c := range chats {
		model.lastReadReactions[c.ID] = make(map[string]bool)
		model.notifiedReactions[c.ID] = make(map[string]bool)
		for _, rKey := range model.getReactionKeys(c.LastMessagePreview) {
			model.lastReadReactions[c.ID][rKey] = true
		}
	}
	stableOrder := make([]string, len(chats))
	for i, c := range chats {
		stableOrder[i] = c.ID
	}
	model.stableChatOrder = stableOrder

	// Load persisted favourites and apply them so favourites appear at the top on launch.
	model.favourites = LoadFavourites()
	model.unhiddenChannels = LoadUnhiddenChannels()


	// Fetch any favourited chats that weren't returned by the regular API call
	// (e.g. chats with very old activity that fell outside chat_limit).
	// We do this concurrently to keep startup fast.
	if len(model.favourites) > 0 {
		loadedIDs := make(map[string]bool, len(chats))
		for _, c := range chats {
			loadedIDs[c.ID] = true
		}

		type fetchResult struct {
			chat *Chat
		}
		missingIDs := make([]string, 0)
		for id := range model.favourites {
			if !loadedIDs[id] {
				missingIDs = append(missingIDs, id)
			}
		}

		if len(missingIDs) > 0 {
			fmt.Printf("⭐ Fetching %d favourited chat(s) not in recent activity...\n", len(missingIDs))
			ch := make(chan fetchResult, len(missingIDs))
			for _, id := range missingIDs {
				go func(chatID string) {
					c, err := GetChat(accessToken, chatID, currentUserName)
					if err != nil {
						ch <- fetchResult{nil}
						return
					}
					ch <- fetchResult{c}
				}(id)
			}
			for range missingIDs {
				res := <-ch
				if res.chat == nil {
					continue
				}
				c := *res.chat
				model.latestChats = append(model.latestChats, c)
				model.stableChatOrder = append(model.stableChatOrder, c.ID)
				model.lastReadReactions[c.ID] = make(map[string]bool)
				model.notifiedReactions[c.ID] = make(map[string]bool)
				if c.LastMessagePreview != nil {
					model.lastMsgID[c.ID] = c.LastMessagePreview.ID
					t, _ := time.Parse(time.RFC3339Nano, c.LastMessagePreview.CreatedDateTime)
					model.lastMsgTime[c.ID] = t
					for _, rKey := range model.getReactionKeys(c.LastMessagePreview) {
						model.lastReadReactions[c.ID][rKey] = true
					}
				}
			}
		}
	}

	model = model.rebuildChatList()

	// 9. Start Bubble Tea program.
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}
}
