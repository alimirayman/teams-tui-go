package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/net/html"
)

const graphAPIBase = "https://graph.microsoft.com/v1.0"
const graphAPIBeta = "https://graph.microsoft.com/beta"

// ---------------------------------------------------------------------------
// Data models
// ---------------------------------------------------------------------------

// ChatMember represents a participant in a chat.
type ChatMember struct {
	ID          *string `json:"id,omitempty"`
	UserID      *string `json:"userId,omitempty"`
	DisplayName *string `json:"displayName,omitempty"`
	Email       *string `json:"email,omitempty"`
}

// Chat represents a Microsoft Teams chat.
type Chat struct {
	ID                 string         `json:"id"`
	Topic              *string        `json:"topic,omitempty"`
	ChatType           string         `json:"chatType"`
	LastUpdated        *string        `json:"lastUpdatedDateTime,omitempty"`
	Viewpoint          *ChatViewpoint `json:"viewpoint,omitempty"`
	LastMessagePreview *Message       `json:"lastMessagePreview,omitempty"`
	Members            []ChatMember   `json:"-"` // populated separately
	CachedDisplayName  *string        `json:"-"` // computed, never from API
}

// ChatViewpoint contains the read state for the current user.
type ChatViewpoint struct {
	LastMessageReadDateTime string `json:"lastMessageReadDateTime"`
}

// Message represents a single message in a chat.
type Message struct {
	ID              string              `json:"id"`
	CreatedDateTime string              `json:"createdDateTime"`
	MessageType     string              `json:"messageType,omitempty"`
	From            *MessageFrom        `json:"from,omitempty"`
	Body            *MessageBody        `json:"body,omitempty"`
	Attachments     []MessageAttachment `json:"attachments,omitempty"`
	Reactions       []MessageReaction   `json:"reactions,omitempty"`
	PlainTextCached *string             `json:"-"`
}

// GetPlainText returns the cached plain text of the message, parsing HTML on demand once.
func (msg *Message) GetPlainText() string {
	if msg.PlainTextCached != nil {
		return *msg.PlainTextCached
	}
	if msg.Body == nil || msg.Body.Content == nil {
		empty := ""
		msg.PlainTextCached = &empty
		return empty
	}
	if *msg.Body.Content == "<systemEventMessage/>" {
		text := "── [system event] ──"
		msg.PlainTextCached = &text
		return text
	}
	text := HTMLToText(*msg.Body.Content, msg.Attachments)
	msg.PlainTextCached = &text
	return text
}

// MessageReaction represents a reaction to a message.
type MessageReaction struct {
	ReactionType    string       `json:"reactionType"`
	CreatedDateTime *string      `json:"createdDateTime,omitempty"`
	User            *MessageFrom `json:"user,omitempty"`
}

// MessageAttachment is a file or card attached to a message.
type MessageAttachment struct {
	ID          string  `json:"id"`
	Name        *string `json:"name,omitempty"`
	ContentType *string `json:"contentType,omitempty"`
	Content     *string `json:"content,omitempty"`
}

// MessageFrom holds the sender information.
type MessageFrom struct {
	User *MessageUser `json:"user,omitempty"`
}

// MessageUser holds the sender display name and ID.
type MessageUser struct {
	ID          *string `json:"id,omitempty"`
	DisplayName *string `json:"displayName,omitempty"`
}

// MessageBody holds the message content (HTML).
type MessageBody struct {
	Content *string `json:"content,omitempty"`
}

// User represents the authenticated Microsoft account user.
type User struct {
	DisplayName       string  `json:"displayName"`
	ID                string  `json:"id"`
	UserPrincipalName *string `json:"userPrincipalName,omitempty"`
}

// ---------------------------------------------------------------------------
// API response wrappers
// ---------------------------------------------------------------------------

type chatsResponse struct {
	Value    []Chat  `json:"value"`
	NextLink *string `json:"@odata.nextLink,omitempty"`
}

type membersResponse struct {
	Value []ChatMember `json:"value"`
}

type messagesResponse struct {
	Value    []Message `json:"value"`
	NextLink *string   `json:"@odata.nextLink,omitempty"`
}

type orgResponse struct {
	Value []struct {
		ID string `json:"id"`
	} `json:"value"`
}

// ---------------------------------------------------------------------------
// HTTP helper
// ---------------------------------------------------------------------------

// graphGet performs an authenticated GET request against the Graph API with retries.
func graphGet(accessToken, path string) ([]byte, error) {
	var body []byte
	var err error
	for i := 0; i < 3; i++ {
		body, err = graphGetOnce(accessToken, path)
		if err == nil {
			return body, nil
		}
		// Retry on transient server errors.
		if strings.Contains(err.Error(), "502") || strings.Contains(err.Error(), "503") || strings.Contains(err.Error(), "504") {
			time.Sleep(time.Duration(i+1) * 500 * time.Millisecond)
			continue
		}
		break
	}
	return body, err
}

// graphGetOnce performs a single authenticated GET request.
func graphGetOnce(accessToken, path string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, graphAPIBase+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d: %s", path, resp.StatusCode, body)
	}
	return body, nil
}

// graphPost performs an authenticated POST request against the Graph API.
func graphPost(accessToken, path string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, graphAPIBase+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST %s: HTTP %d: %s", path, resp.StatusCode, body)
	}
	return nil
}

// graphPatch performs an authenticated PATCH request against the Graph API.
func graphPatch(accessToken, path string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	req, err := http.NewRequest(http.MethodPatch, graphAPIBase+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("PATCH %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("PATCH %s: HTTP %d: %s", path, resp.StatusCode, body)
	}
	return nil
}

// graphDelete performs an authenticated DELETE request against the Graph API.
func graphDelete(accessToken, path string, useBeta bool) error {
	base := graphAPIBase
	if useBeta {
		base = graphAPIBeta
	}
	req, err := http.NewRequest(http.MethodDelete, base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("DELETE %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("DELETE %s: HTTP %d: %s", path, resp.StatusCode, body)
	}
	return nil
}

// ---------------------------------------------------------------------------
// GetMe — current user profile with cache
// ---------------------------------------------------------------------------

// GetMe fetches the authenticated user's profile, using a local cache.
func GetMe(accessToken string) (*User, error) {
	cacheDir, err := GetCacheDir()
	if err == nil {
		profilePath := filepath.Join(cacheDir, "profile.json")
		if data, err := os.ReadFile(profilePath); err == nil {
			var u User
			if json.Unmarshal(data, &u) == nil {
				return &u, nil
			}
		}
	}

	body, err := graphGet(accessToken, "/me")
	if err != nil {
		return nil, fmt.Errorf("GetMe: %w", err)
	}

	var u User
	if err := json.Unmarshal(body, &u); err != nil {
		return nil, fmt.Errorf("GetMe: parse: %w", err)
	}

	// Persist to cache.
	if cacheDir != "" {
		_ = os.WriteFile(filepath.Join(cacheDir, "profile.json"), body, 0o600)
	}
	return &u, nil
}

// ---------------------------------------------------------------------------
// GetChatMembers
// ---------------------------------------------------------------------------

// GetChatMembers returns the members of a chat. On error it returns an empty slice.
func GetChatMembers(accessToken, chatID string) []ChatMember {
	body, err := graphGet(accessToken, "/chats/"+chatID+"/members")
	if err != nil {
		return nil
	}
	var r membersResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil
	}
	return r.Value
}

// ---------------------------------------------------------------------------
// GetMessages
// ---------------------------------------------------------------------------

// GetMessages returns the messages in a chat (newest first from the API) and a next link for pagination.
func GetMessages(accessToken, chatID string, top int) ([]Message, string, error) {
	pageSize := top
	if pageSize > 50 || pageSize <= 0 {
		pageSize = 50
	}

	url := fmt.Sprintf("/chats/%s/messages?$orderby=createdDateTime%%20desc&$top=%d", chatID, pageSize)
	body, err := graphGet(accessToken, url)
	if err != nil {
		return nil, "", fmt.Errorf("GetMessages: %w", err)
	}
	var r messagesResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, "", fmt.Errorf("GetMessages: parse: %w", err)
	}

	allMsgs := r.Value
	next := ""
	if r.NextLink != nil {
		next = *r.NextLink
	}

	// Keep fetching pages if we need more messages and a next link is available.
	for len(allMsgs) < top && next != "" {
		nextMsgs, newNext, err := GetMessagesFromLink(accessToken, next)
		if err != nil {
			// Return what we have so far instead of failing completely.
			break
		}
		allMsgs = append(allMsgs, nextMsgs...)
		next = newNext
	}

	// Ensure messages are sorted by creation time (newest first).
	sort.Slice(allMsgs, func(i, j int) bool {
		return allMsgs[i].CreatedDateTime > allMsgs[j].CreatedDateTime
	})

	return allMsgs, next, nil
}

// GetMessagesFromLink fetches messages from a full Graph API URL (used for pagination).
func GetMessagesFromLink(accessToken, nextLink string) ([]Message, string, error) {
	// nextLink is a full URL, but graphGet expects a path starting with /.
	// However, graphGetOnce uses graphAPIBase + path.
	// We should probably add a helper for full URLs or just strip the base.
	path := nextLink
	if strings.HasPrefix(path, graphAPIBase) {
		path = path[len(graphAPIBase):]
	} else if strings.HasPrefix(path, graphAPIBeta) {
		path = path[len(graphAPIBeta):]
	}

	body, err := graphGet(accessToken, path)
	if err != nil {
		return nil, "", fmt.Errorf("GetMessagesFromLink: %w", err)
	}
	var r messagesResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, "", fmt.Errorf("GetMessagesFromLink: parse: %w", err)
	}

	sort.Slice(r.Value, func(i, j int) bool {
		return r.Value[i].CreatedDateTime > r.Value[j].CreatedDateTime
	})

	next := ""
	if r.NextLink != nil {
		next = *r.NextLink
	}

	return r.Value, next, nil
}

// ---------------------------------------------------------------------------
// SendMessage
// ---------------------------------------------------------------------------

// replaceEmoticons replaces popular text emoticons with their Unicode equivalents.
func replaceEmoticons(s string) string {
	// Order matters: replace longer versions first to avoid partial matches.
	replacements := []struct{ from, to string }{
		{":-D", "😀"},
		{":D", "😀"},
		{":-)", "🙂"},
		{":)", "🙂"},
		{";-)", "😉"},
		{";)", "😉"},
		{":-(", "🙁"},
		{":(", "🙁"},
		{":-P", "😛"},
		{":P", "😛"},
		{"<3", "❤️"},
		{"(y)", "👍"},
		{"(n)", "👎"},
	}

	for _, r := range replacements {
		s = strings.ReplaceAll(s, r.from, r.to)
	}
	return s
}

// formatMessageBody prepares the payload body for sending or updating a message.
func formatMessageBody(content string) map[string]any {
	content = replaceEmoticons(content)

	// Detect whether content needs HTML rendering (markdown or multi-line).
	hasMarkdown := containsMarkdown(content)
	isMultiLine := strings.Contains(content, "\n")
	isIndented := strings.HasPrefix(content, " ") || strings.HasPrefix(content, "\t")

	if !hasMarkdown && !isMultiLine && !isIndented {
		// Plain single-line text — send as-is.
		return map[string]any{
			"content": content,
		}
	}

	// Convert markdown (and handle multi-line) to Teams-compatible HTML.
	return map[string]any{
		"contentType": "html",
		"content":     markdownToHTML(content),
	}
}

// SendMessage posts a message to the given chat.
func SendMessage(accessToken, chatID, content string) error {
	payload := map[string]any{
		"body": formatMessageBody(content),
	}
	return graphPost(accessToken, "/chats/"+chatID+"/messages", payload)
}

// SendMessageWithReference posts a reply-to-message using a Teams messageReference
// attachment, making it appear as a proper quoted reply in the Teams client.
func SendMessageWithReference(accessToken, chatID string, ref *Message, content string) error {
	if ref == nil {
		return SendMessage(accessToken, chatID, content)
	}

	// Build the sender JSON for the attachment content field.
	senderName := ""
	senderID := ""
	if ref.From != nil && ref.From.User != nil {
		if ref.From.User.DisplayName != nil {
			senderName = *ref.From.User.DisplayName
		}
		if ref.From.User.ID != nil {
			senderID = *ref.From.User.ID
		}
	}

	// messagePreview is the plain-text snippet of the quoted message.
	preview := ""
	if ref.Body != nil && ref.Body.Content != nil {
		preview = stripBasicHTML(*ref.Body.Content)
	}
	const maxPreview = 200
	if len([]rune(preview)) > maxPreview {
		preview = string([]rune(preview)[:maxPreview]) + "…"
	}

	attContent := map[string]any{
		"messageId":      ref.ID,
		"messagePreview": preview,
		"messageSender": map[string]any{
			"application": nil,
			"device":      nil,
			"user": map[string]any{
				"userIdentityType": "aadUser",
				"id":               senderID,
				"displayName":      senderName,
			},
		},
	}
	attContentJSON, _ := json.Marshal(attContent)

	// The body MUST be HTML and MUST contain <attachment id="..."></attachment> as a
	// placeholder so Teams knows where to render the quote bubble.
	marker := fmt.Sprintf(`<attachment id="%s"></attachment>`, ref.ID)
	var bodyHTML string
	if containsMarkdown(content) || strings.Contains(content, "\n") {
		bodyHTML = marker + "\n" + markdownToHTML(content)
	} else {
		bodyHTML = marker + "\n<p>" + content + "</p>"
	}

	payload := map[string]any{
		"body": map[string]any{
			"contentType": "html",
			"content":     bodyHTML,
		},
		"attachments": []map[string]any{
			{
				"id":          ref.ID,
				"contentType": "messageReference",
				"content":     string(attContentJSON),
			},
		},
	}
	return graphPost(accessToken, "/chats/"+chatID+"/messages", payload)
}

// stripBasicHTML removes HTML tags to produce a plain-text preview.
// It is a lightweight alternative to HTMLToText for building attachment content fields.
func stripBasicHTML(s string) string {
	tokenizer := html.NewTokenizer(strings.NewReader(s))
	var sb strings.Builder
	for {
		tt := tokenizer.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt == html.TextToken {
			sb.WriteString(html.UnescapeString(tokenizer.Token().Data))
		}
	}
	return strings.TrimSpace(sb.String())
}

// UpdateMessage modifies an existing message in a chat.
func UpdateMessage(accessToken, chatID, messageID, content string) error {
	payload := map[string]any{
		"body": formatMessageBody(content),
	}
	return graphPatch(accessToken, "/chats/"+chatID+"/messages/"+messageID, payload)
}


// ---------------------------------------------------------------------------
// SetReaction
// ---------------------------------------------------------------------------

// SetReaction adds or updates a reaction on a message.
func SetReaction(accessToken, chatID, messageID, reactionType string) error {
	payload := map[string]any{
		"reactionType": reactionType,
	}
	return graphPost(accessToken, "/chats/"+chatID+"/messages/"+messageID+"/setReaction", payload)
}

// UnsetReaction removes a reaction from a message.
func UnsetReaction(accessToken, chatID, messageID, reactionType string) error {
	payload := map[string]any{
		"reactionType": reactionType,
	}
	return graphPost(accessToken, "/chats/"+chatID+"/messages/"+messageID+"/unsetReaction", payload)
}

// DeleteMessage removes a message from a chat.
func DeleteMessage(accessToken, chatID, messageID string) error {
	// The Graph API does not support true DELETE for chat messages.
	// We perform a "soft delete" by updating the content to a placeholder.
	payload := map[string]any{
		"body": map[string]any{
			"content": "*(deleted)*",
		},
	}
	return graphPatch(accessToken, "/chats/"+chatID+"/messages/"+messageID, payload)
}

// ---------------------------------------------------------------------------
// MarkChatAsRead
// ---------------------------------------------------------------------------

// MarkChatAsRead marks the chat as read for the current user.
// All errors are silently ignored so as not to disrupt the UX.
func MarkChatAsRead(accessToken, chatID, userID string) {
	// Fetch tenant ID.
	body, err := graphGet(accessToken, "/organization")
	if err != nil {
		return
	}
	var org orgResponse
	if err := json.Unmarshal(body, &org); err != nil || len(org.Value) == 0 {
		return
	}
	tenantID := org.Value[0].ID

	payload := map[string]any{
		"user": map[string]string{
			"id":       userID,
			"tenantId": tenantID,
		},
	}
	_ = graphPost(accessToken, "/chats/"+chatID+"/markChatReadForUser", payload)
}

// ---------------------------------------------------------------------------
// GetChats — main chat list with member fetch + display name computation
// ---------------------------------------------------------------------------

// GetChats fetches the user's chats, fetches members for each,
// detects the current user (by frequency analysis), filters the current user
// from member lists, computes CachedDisplayName, and returns
// (chats, detectedCurrentUserName).
func GetChats(accessToken string, existingChats []Chat, currentUserName *string) ([]Chat, *string, error) {
	limit := ResolveChatLimit()

	// Build a map of existing members to avoid fetching them again in background refreshes.
	// We copy the slice to prevent background threads from sharing/mutating slice backing arrays with the UI thread.
	existingMembers := make(map[string][]ChatMember)
	for _, c := range existingChats {
		if len(c.Members) > 0 {
			membersCopy := make([]ChatMember, len(c.Members))
			copy(membersCopy, c.Members)
			existingMembers[c.ID] = membersCopy
		}
	}

	// We load a larger batch of chat metadata first to ensure we don't miss recent chats,
	// since the Graph API does not guarantee chronological order on /me/chats pages.
	metadataLimit := 150
	if limit > metadataLimit {
		metadataLimit = limit
	}

	url := "/me/chats?$expand=lastMessagePreview"
	body, err := graphGet(accessToken, url)
	if err != nil {
		return nil, nil, fmt.Errorf("GetChats: %w", err)
	}
	var r chatsResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, nil, fmt.Errorf("GetChats: parse: %w", err)
	}

	chats := r.Value
	next := ""
	if r.NextLink != nil {
		next = *r.NextLink
	}

	// Keep fetching pages of chats if we need more to satisfy the metadata limit.
	for len(chats) < metadataLimit && next != "" {
		path := next
		if strings.HasPrefix(path, graphAPIBase) {
			path = path[len(graphAPIBase):]
		} else if strings.HasPrefix(path, graphAPIBeta) {
			path = path[len(graphAPIBeta):]
		}

		nextBody, err := graphGet(accessToken, path)
		if err != nil {
			break
		}
		var nextR chatsResponse
		if err := json.Unmarshal(nextBody, &nextR); err != nil {
			break
		}
		chats = append(chats, nextR.Value...)
		if nextR.NextLink != nil {
			next = *nextR.NextLink
		} else {
			next = ""
		}
	}

	// Filter out meeting chats with no messages (LastMessagePreview is nil)
	var filtered []Chat
	for _, c := range chats {
		if c.ChatType == "meeting" && c.LastMessagePreview == nil {
			continue
		}
		filtered = append(filtered, c)
	}
	chats = filtered

	// Sort the entire list of chats by latest activity (message or update time) descending.
	type chatWithTime struct {
		chat Chat
		t    time.Time
	}
	combined := make([]chatWithTime, len(chats))
	for i, c := range chats {
		t := time.Time{}
		if c.LastMessagePreview != nil {
			t, _ = time.Parse(time.RFC3339Nano, c.LastMessagePreview.CreatedDateTime)
		}
		if c.LastUpdated != nil {
			lut, _ := time.Parse(time.RFC3339Nano, *c.LastUpdated)
			if lut.After(t) {
				t = lut
			}
		}
		combined[i] = chatWithTime{c, t}
	}

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
	chats = sorted

	// Truncate to the user's requested chat limit.
	if len(chats) > limit {
		chats = chats[:limit]
	}

	// Fetch members concurrently only for the truncated, active chats (if not already cached).
	type result struct {
		index   int
		members []ChatMember
	}
	ch := make(chan result, len(chats))
	for i, c := range chats {
		go func(i int, id string) {
			if cached, ok := existingMembers[id]; ok {
				ch <- result{i, cached}
			} else {
				ch <- result{i, GetChatMembers(accessToken, id)}
			}
		}(i, c.ID)
	}
	for range chats {
		res := <-ch
		chats[res.index].Members = res.members
	}

	// Detect current user by name frequency across oneOnOne chats if not already provided.
	if currentUserName == nil {
		currentUserName = detectCurrentUser(chats)
	}

	// Filter current user from member lists and compute display names.
	for i := range chats {
		if currentUserName != nil {
			chats[i].Members = filterMember(chats[i].Members, *currentUserName)
		}
		chats[i].CachedDisplayName = new(string)
		*chats[i].CachedDisplayName = computeDisplayName(&chats[i])
	}

	return chats, currentUserName, nil
}

// ---------------------------------------------------------------------------
// GetChat — fetch a single chat by ID
// ---------------------------------------------------------------------------

// GetChat fetches a single chat by ID, populates its members, filters the
// current user, and computes CachedDisplayName. Used to hydrate favourited
// chats that fall outside the regular chat_limit window.
func GetChat(accessToken, chatID string, currentUserName *string) (*Chat, error) {
	body, err := graphGet(accessToken, "/chats/"+chatID+"?$expand=lastMessagePreview")
	if err != nil {
		return nil, fmt.Errorf("GetChat: %w", err)
	}
	var c Chat
	if err := json.Unmarshal(body, &c); err != nil {
		return nil, fmt.Errorf("GetChat: parse: %w", err)
	}

	// Fetch members (same as GetChats does per-chat).
	c.Members = GetChatMembers(accessToken, chatID)

	// Filter current user and compute display name.
	if currentUserName != nil {
		c.Members = filterMember(c.Members, *currentUserName)
	}
	c.CachedDisplayName = new(string)
	*c.CachedDisplayName = computeDisplayName(&c)
	return &c, nil
}

// detectCurrentUser identifies the current user by finding the display name
// that appears most frequently across oneOnOne chats.
func detectCurrentUser(chats []Chat) *string {
	freq := map[string]int{}
	for _, c := range chats {
		if c.ChatType != "oneOnOne" {
			continue
		}
		for _, m := range c.Members {
			if m.DisplayName != nil {
				freq[*m.DisplayName]++
			}
		}
	}
	if len(freq) == 0 {
		return nil
	}

	var best string
	var bestCount int
	for name, count := range freq {
		if count > bestCount {
			best = name
			bestCount = count
		}
	}

	// Only treat as current user if appears ≥2 times, or is the sole member in all oneOnOne chats.
	oneOnOneCount := 0
	for _, c := range chats {
		if c.ChatType == "oneOnOne" {
			oneOnOneCount++
		}
	}
	if bestCount >= 2 || oneOnOneCount == 1 {
		return &best
	}
	return nil
}

// filterMember removes the named member from the slice by allocating a new slice (never modifying in-place).
func filterMember(members []ChatMember, name string) []ChatMember {
	var out []ChatMember
	for _, m := range members {
		if m.DisplayName == nil || *m.DisplayName != name {
			out = append(out, m)
		}
	}
	return out
}

// computeDisplayName derives a human-readable display name for a chat.
func computeDisplayName(c *Chat) string {
	switch c.ChatType {
	case "oneOnOne":
		if len(c.Members) > 0 && c.Members[0].DisplayName != nil {
			return *c.Members[0].DisplayName
		}
		return "Unknown"

	case "group", "meeting":
		if c.Topic != nil && *c.Topic != "" {
			return *c.Topic
		}
		parts := memberAbbreviations(c.Members, 3)
		if len(parts) > 0 {
			return strings.Join(parts, ", ")
		}
		if c.ChatType == "group" {
			return "Unnamed Group"
		}
		return "Unnamed Meeting"

	default:
		if c.Topic != nil && *c.Topic != "" {
			return *c.Topic
		}
		parts := memberAbbreviations(c.Members, 3)
		if len(parts) > 0 {
			return strings.Join(parts, ", ")
		}
		return "Unknown Chat"
	}
}

// memberAbbreviations returns up to n abbreviated member display names.
func memberAbbreviations(members []ChatMember, n int) []string {
	var names []string
	for _, m := range members {
		if m.DisplayName == nil {
			continue
		}
		names = append(names, abbreviateName(*m.DisplayName))
	}

	sort.Strings(names)

	var out []string
	for i := 0; i < len(names) && i < n; i++ {
		out = append(out, names[i])
	}
	return out
}

// abbreviateName converts "Matt Davidson" → "Matt D", single word stays as-is.
func abbreviateName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	parts := strings.Fields(name)
	if len(parts) == 1 {
		return parts[0]
	}
	return parts[0] + " " + string([]rune(parts[len(parts)-1])[0])
}

// ---------------------------------------------------------------------------
// HTML-to-text rendering
// ---------------------------------------------------------------------------

func decodeSafeLink(u string) string {
	parsed, err := url.Parse(u)
	if err != nil {
		return u
	}
	if strings.Contains(parsed.Host, "safelinks.protection.outlook.com") {
		realURL := parsed.Query().Get("url")
		if realURL != "" {
			return realURL
		}
	}
	return u
}

// ExtractURLs extracts all unique URLs from a Teams message HTML body.
func ExtractURLs(htmlContent string) []string {
	tokenizer := html.NewTokenizer(strings.NewReader(htmlContent))
	var urls []string
	urlMap := make(map[string]bool)

	addURL := func(u string) {
		u = decodeSafeLink(u)
		if u == "" {
			return
		}
		if !urlMap[u] {
			urls = append(urls, u)
			urlMap[u] = true
		}
	}

	for {
		tt := tokenizer.Next()
		if tt == html.ErrorToken {
			break
		}
		token := tokenizer.Token()
		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			if token.Data == "a" {
				for _, a := range token.Attr {
					if a.Key == "href" {
						addURL(a.Val)
					}
				}
			}
		case html.TextToken:
			text := html.UnescapeString(token.Data)
			matches := urlRegex.FindAllString(text, -1)
			for _, m := range matches {
				addURL(m)
			}
		}
	}
	return urls
}

var urlRegex = regexp.MustCompile(`https?://[^\s<>"]+`)

// HTMLToText converts a Teams message HTML body to plain text suitable for
// terminal display. It returns the rendered text and a lipgloss-compatible
// styled string (where special elements are coloured).
func HTMLToText(htmlContent string, attachments []MessageAttachment) string {
	if htmlContent == "" {
		return ""
	}

	// Build an attachment lookup by ID.
	attByID := make(map[string]MessageAttachment, len(attachments))
	for _, a := range attachments {
		attByID[a.ID] = a
	}

	tokenizer := html.NewTokenizer(strings.NewReader(htmlContent))
	var sb strings.Builder
	var lastChar rune
	var tagAddedNewline bool

	// ---- existing state ----
	var inPre bool
	var inLink bool
	var currentLinkURL string
	var linkText strings.Builder

	// ---- NEW: inline formatting state ----
	inBold := false
	inItalic := false
	inStrike := false
	inCode := false // <code> tag (inline or inside <pre>)

	// ---- NEW: list state ----
	type listInfo struct {
		ordered bool
		counter int
	}
	var listStack []listInfo

	// ---- lipgloss styles ----
	preCodeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#98C379"))
	bulletStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))

	// applyInlineStyles applies the currently active inline text styles.
	applyInlineStyles := func(text string) string {
		if inCode && inPre {
			return preCodeStyle.Render(text)
		}
		s := lipgloss.NewStyle()
		anySet := false
		if inBold {
			s = s.Bold(true)
			anySet = true
		}
		if inItalic {
			s = s.Italic(true)
			anySet = true
		}
		if inStrike {
			s = s.Strikethrough(true)
			anySet = true
		}
		if inCode {
			s = s.Foreground(lipgloss.Color("#E5C07B"))
			anySet = true
		}
		if !anySet {
			return text
		}
		return s.Render(text)
	}

	for {
		tt := tokenizer.Next()
		if tt == html.ErrorToken {
			break
		}

		token := tokenizer.Token()

		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			tag := token.Data
			if tag == "pre" {
				inPre = true
			}
			switch tag {
			// ---- inline formatting on ----
			case "b", "strong":
				inBold = true
			case "em", "i":
				inItalic = true
			case "s", "strike", "del":
				inStrike = true
			case "code":
				inCode = true

			// ---- lists ----
			case "ul":
				listStack = append(listStack, listInfo{ordered: false})
			case "ol":
				listStack = append(listStack, listInfo{ordered: true})
			case "li":
				if lastChar != '\n' && sb.Len() > 0 {
					sb.WriteRune('\n')
					lastChar = '\n'
				}
				if len(listStack) > 0 {
					info := &listStack[len(listStack)-1]
					indent := strings.Repeat("  ", len(listStack)-1)
					var prefix string
					if info.ordered {
						info.counter++
						prefix = bulletStyle.Render(fmt.Sprintf("%s%d. ", indent, info.counter))
					} else {
						prefix = bulletStyle.Render(indent + "• ")
					}
					sb.WriteString(prefix)
					lastChar = ' '
				}
				tagAddedNewline = false

			case "img":
				orangeText := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF8700")).Render("image")
				content := "🖼️  " + orangeText
				if inLink {
					content = fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", currentLinkURL, content)
				}
				sb.WriteString(content)
				lastChar = 'e'

			case "attachment":
				var attID string
				for _, a := range token.Attr {
					if a.Key == "id" {
						attID = a.Val
						break
					}
				}
				if att, ok := attByID[attID]; ok {
					if att.ContentType != nil && *att.ContentType == "messageReference" {
						// Render a quoted-message block: ▎ Sender: preview text
						if att.Content != nil {
							quote := renderMessageReference(*att.Content)
							if quote != "" {
								if sb.Len() > 0 && lastChar != '\n' {
									sb.WriteRune('\n')
								}
								sb.WriteString(quote)
								sb.WriteRune('\n')
								lastChar = '\n'
							}
						}
						continue
					}
					orangeText := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF8700")).Render("Attachment")
					sb.WriteString("📎 " + orangeText)
					lastChar = 't'
				}

			case "emoji":
				var altText string
				for _, a := range token.Attr {
					if a.Key == "alt" {
						altText = a.Val
						break
					}
				}
				if altText != "" {
					sb.WriteString(altText)
					r, _ := utf8.DecodeLastRuneInString(altText)
					lastChar = r
				}

			case "br":
				if lastChar != '\n' && sb.Len() > 0 {
					sb.WriteRune('\n')
					lastChar = '\n'
				}
				tagAddedNewline = true

			// Block-level elements — closing tag emits newline.
			case "p", "div", "pre":
				// Do nothing — closing tag will emit newline.

			case "a":
				for _, a := range token.Attr {
					if a.Key == "href" {
						currentLinkURL = decodeSafeLink(a.Val)
						inLink = true
						linkText.Reset()
						break
					}
				}
			}

		case html.EndTagToken:
			tag := token.Data
			if tag == "pre" {
				inPre = false
			}
			switch tag {
			// ---- inline formatting off ----
			case "b", "strong":
				inBold = false
			case "em", "i":
				inItalic = false
			case "s", "strike", "del":
				inStrike = false
			case "code":
				inCode = false

			// ---- lists ----
			case "ul", "ol":
				if len(listStack) > 0 {
					listStack = listStack[:len(listStack)-1]
				}
			case "li":
				if lastChar != '\n' && sb.Len() > 0 {
					sb.WriteRune('\n')
					lastChar = '\n'
				}
				tagAddedNewline = true

			case "p", "div", "pre":
				if lastChar != '\n' && sb.Len() > 0 {
					sb.WriteRune('\n')
					lastChar = '\n'
				}
				tagAddedNewline = true
			case "br":
				if lastChar != '\n' && sb.Len() > 0 {
					sb.WriteRune('\n')
					lastChar = '\n'
				}
				tagAddedNewline = true
			case "a":
				if inLink {
					lt := strings.TrimSpace(linkText.String())
					if lt != "" && lt != currentLinkURL && !strings.Contains(currentLinkURL, lt) {
						diag := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Render(" (" + currentLinkURL + ")")
						sb.WriteString(diag)
					}
					inLink = false
					currentLinkURL = ""
					linkText.Reset()
				}
			}

		case html.TextToken:
			text := html.UnescapeString(token.Data)
			if tagAddedNewline {
				// Consume exactly one leading newline if a tag just added one.
				if strings.HasPrefix(text, "\n") {
					text = text[1:]
				} else if strings.HasPrefix(text, "\r\n") {
					text = text[2:]
				}
				tagAddedNewline = false
			}
			if text != "" {
				// Skip whitespace-only tokens if they follow a newline and we're not in pre.
				// IMPORTANT: We do NOT skip non-breaking spaces (\u00A0) as they are used
				// to represent intentional empty lines or indentation.
				if !inPre && lastChar == '\n' && strings.TrimSpace(text) == "" && !strings.Contains(text, "\u00A0") {
					continue
				}

				// Apply inline formatting styles.
				styledText := applyInlineStyles(text)

				if inLink && currentLinkURL != "" {
					linkText.WriteString(text)
					linkStyled := lipgloss.NewStyle().
						Foreground(lipgloss.Color("#00AFFF")).
						Underline(true).
						Render(styledText)
					styledText = fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", currentLinkURL, linkStyled)
				} else if !inBold && !inItalic && !inStrike && !inCode {
					// Plain text: detect and style bare URLs.
					styledText = urlRegex.ReplaceAllStringFunc(text, func(u string) string {
						styled := lipgloss.NewStyle().
							Foreground(lipgloss.Color("#00AFFF")).
							Underline(true).
							Render(u)
						return fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", u, styled)
					})
				}

				sb.WriteString(styledText)
				r, _ := utf8.DecodeLastRuneInString(styledText)
				lastChar = r
			}
		}
	}

	result := sb.String()

	// Collapse runs of more than 2 consecutive newlines.
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}

	return strings.Trim(result, "\n\r")
}

// getAttachmentIcon returns an emoji icon based on the attachment's file extension
// or content type.
func getAttachmentIcon(att MessageAttachment) string {
	name := ""
	if att.Name != nil {
		name = strings.ToLower(*att.Name)
	}
	ct := ""
	if att.ContentType != nil {
		ct = strings.ToLower(*att.ContentType)
	}

	// Check extension first.
	ext := ""
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		ext = name[idx+1:]
	}

	switch ext {
	case "jpg", "jpeg", "png", "gif", "bmp", "svg", "webp":
		return "🖼️"
	case "pdf", "txt":
		return "📄"
	case "doc", "docx":
		return "📝"
	case "xls", "xlsx", "csv":
		return "📊"
	case "ppt", "pptx":
		return "📊"
	case "mp4", "avi", "mov", "mkv", "webm":
		return "🎥"
	case "mp3", "wav", "ogg", "flac":
		return "🎵"
	case "zip", "rar", "7z", "tar", "gz":
		return "📦"
	case "html", "htm":
		return "🌐"
	case "json", "xml":
		return "📋"
	}

	// Fall back to content type.
	switch {
	case strings.HasPrefix(ct, "image/"):
		return "🖼️"
	case strings.HasPrefix(ct, "video/"):
		return "🎥"
	case strings.HasPrefix(ct, "audio/"):
		return "🎵"
	case strings.Contains(ct, "word") || strings.Contains(ct, "document"):
		return "📝"
	case strings.Contains(ct, "excel") || strings.Contains(ct, "spreadsheet"):
		return "📊"
	case strings.Contains(ct, "powerpoint") || strings.Contains(ct, "presentation"):
		return "📊"
	case strings.Contains(ct, "zip") || strings.Contains(ct, "archive"):
		return "📦"
	}

	return "📎"
}

// GetTenantID fetches the first tenant ID from the /organization endpoint.
func GetTenantID(accessToken string) (string, error) {
	body, err := graphGet(accessToken, "/organization")
	if err != nil {
		return "", err
	}
	var org orgResponse
	if err := json.Unmarshal(body, &org); err != nil || len(org.Value) == 0 {
		return "", fmt.Errorf("could not parse organization response")
	}
	return org.Value[0].ID, nil
}

// graphPostWithResponse performs an authenticated POST request and returns the response body.
func graphPostWithResponse(accessToken, path string, payload any) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, graphAPIBase+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("POST %s: HTTP %d: %s", path, resp.StatusCode, body)
	}
	return body, nil
}

// SearchUsers searches for users in the tenant directory.
func SearchUsers(accessToken, query string) ([]User, error) {
	escaped := strings.ReplaceAll(query, "'", "''")
	filterExpr := fmt.Sprintf("startsWith(displayName,'%s') or startsWith(userPrincipalName,'%s')", escaped, escaped)
	path := "/users?$filter=" + url.QueryEscape(filterExpr) + "&$top=10"
	
	body, err := graphGet(accessToken, path)
	if err != nil {
		return nil, fmt.Errorf("SearchUsers: %w", err)
	}
	
	var r struct {
		Value []User `json:"value"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("SearchUsers parse: %w", err)
	}
	return r.Value, nil
}

// renderMessageReference parses a messageReference attachment content JSON and
// returns a styled terminal quote block: "▎ SenderName [2 Jan 15:04]: message preview".
// Returns an empty string if the content cannot be parsed.
func renderMessageReference(content string) string {
	var ref struct {
		MessageID      string `json:"messageId"`
		MessagePreview string `json:"messagePreview"`
		MessageSender  struct {
			User *struct {
				DisplayName string `json:"displayName"`
			} `json:"user"`
		} `json:"messageSender"`
	}
	if err := json.Unmarshal([]byte(content), &ref); err != nil {
		return ""
	}

	preview := strings.TrimSpace(ref.MessagePreview)
	if preview == "" {
		return ""
	}

	// Truncate very long previews.
	const maxPreview = 120
	if len([]rune(preview)) > maxPreview {
		runes := []rune(preview)
		preview = string(runes[:maxPreview]) + "…"
	}

	// Teams message IDs are Unix timestamps in milliseconds.
	var timeStr string
	if ms, err := strconv.ParseInt(ref.MessageID, 10, 64); err == nil && ms > 0 {
		t := time.UnixMilli(ms).Local()
		now := time.Now()
		if t.Year() == now.Year() {
			timeStr = t.Format("2 Jan 15:04")
		} else {
			timeStr = t.Format("2 Jan 2006 15:04")
		}
	}

	quoteStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6C7A89"))
	barStyle   := lipgloss.NewStyle().Foreground(lipgloss.Color("#4A90D9")).Bold(true)
	nameStyle  := lipgloss.NewStyle().Foreground(lipgloss.Color("#7EC8E3")).Bold(true)
	timeStyle  := lipgloss.NewStyle().Foreground(lipgloss.Color("#4A5568"))

	bar := barStyle.Render("▎")

	var meta string
	if ref.MessageSender.User != nil && ref.MessageSender.User.DisplayName != "" {
		meta = nameStyle.Render(ref.MessageSender.User.DisplayName)
	}
	if timeStr != "" {
		meta += timeStyle.Render(" [" + timeStr + "]")
	}

	if meta != "" {
		return bar + " " + meta + quoteStyle.Render(": "+preview)
	}
	return bar + " " + quoteStyle.Render(preview)
}

// GetOrCreateOneOnOneChat creates a new 1-on-1 chat with the user specified by their UPN (email).
// If the chat already exists, the Graph API returns the existing one.
func GetOrCreateOneOnOneChat(accessToken, myUserID, otherUPN string) (*Chat, error) {
	payload := map[string]any{
		"chatType": "oneOnOne",
		"members": []map[string]any{
			{
				"@odata.type": "#microsoft.graph.aadUserConversationMember",
				"roles":       []string{"owner"},
				"user@odata.bind": fmt.Sprintf("https://graph.microsoft.com/v1.0/users('%s')", myUserID),
			},
			{
				"@odata.type": "#microsoft.graph.aadUserConversationMember",
				"roles":       []string{"owner"},
				"user@odata.bind": fmt.Sprintf("https://graph.microsoft.com/v1.0/users('%s')", otherUPN),
			},
		},
	}

	body, err := graphPostWithResponse(accessToken, "/chats", payload)
	if err != nil {
		return nil, fmt.Errorf("create chat: %w", err)
	}

	var chat Chat
	if err := json.Unmarshal(body, &chat); err != nil {
		return nil, fmt.Errorf("unmarshal chat response: %w", err)
	}

	// Fetch members for the chat to compute display name properly
	chat.Members = GetChatMembers(accessToken, chat.ID)

	return &chat, nil
}


