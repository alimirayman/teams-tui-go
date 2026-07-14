package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type testCSISequence []byte

func testStringPtr(s string) *string { return &s }

func TestFitLineFlattensEmbeddedNewlines(t *testing.T) {
	got := fitLine("alpha\nbeta\rgamma", 80)
	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("fitLine returned embedded newline characters: %q", got)
	}
}

func TestFitLinePreservesBengaliGraphemeBoundaries(t *testing.T) {
	got := fitLine("আমাদের রোগীদের স্বাস্থ্যসেবা নিশ্চিত করতে হবে", 16)
	if width := cellWidth(got); width > 16 {
		t.Fatalf("Bengali line width = %d, want <= 16: %q", width, got)
	}
	if strings.ContainsRune(got, '\uFFFD') {
		t.Fatalf("Bengali line contains a broken UTF-8 replacement rune: %q", got)
	}
}

func TestJoinColumnsUsesLegacyCellWidth(t *testing.T) {
	left := "বাংলা চ্যানেল"
	right := "বাংলা বার্তা"
	got := joinColumns(left, 18, "│", right, 24, 1)
	if width := cellWidth(got); width != 43 {
		t.Fatalf("joined width = %d, want 43: %q", width, got)
	}
	if strings.Count(got, "│") != 1 {
		t.Fatalf("expected one fixed divider: %q", got)
	}
}

func TestMainViewKeepsBengaliInsideFixedColumns(t *testing.T) {
	chatName := "বাংলা চ্যানেল এবং হাসপাতালের আলোচনা"
	body := "আমাদের রোগীদের জন্য সঠিক চিকিৎসা নিশ্চিত করতে এই বার্তাটি লেখা হয়েছে।"
	currentUser := "Current User"
	app := NewApp()
	app.CurrentUserName = &currentUser
	app.SelectedIndex = 0
	app.Chats = []Chat{{ID: "chat-id", CachedDisplayName: &chatName}}
	app.Messages = []Message{{
		ID:              "message-id",
		CreatedDateTime: "2026-07-10T00:00:00Z",
		Body:            &MessageBody{Content: &body},
	}}

	model := NewModel(app, "client-id", "user-id")
	model.width = 100
	model.height = 24
	view := model.View()
	lines := strings.Split(view, "\n")
	if len(lines) != 24 {
		t.Fatalf("view height = %d, want 24", len(lines))
	}
	for i, line := range lines {
		if width := cellWidth(line); width > 99 {
			t.Fatalf("line %d width = %d, want <= 99: %q", i, width, line)
		}
	}
}

func TestRenderMessagesReservesInlineImageRows(t *testing.T) {
	name := "diagram.png"
	contentType := "image/png"
	contentURL := "https://example.com/diagram.png"
	body := "Architecture diagram"
	chatName := "Design"
	app := NewApp()
	app.SelectedIndex = 0
	app.Features.FilePreviewInTerminal = true
	app.Chats = []Chat{{ID: "chat-id", CachedDisplayName: &chatName}}
	app.Messages = []Message{{
		ID:              "message-id",
		CreatedDateTime: "2026-07-10T00:00:00Z",
		Body:            &MessageBody{Content: &body},
		Attachments: []MessageAttachment{{
			ID:          "image-id",
			Name:        &name,
			ContentType: &contentType,
			ContentURL:  &contentURL,
		}},
	}}

	model := NewModel(app, "client-id", "user-id")
	_ = model.renderMessages(80, 24)
	if len(app.InlineImagePlacements) != 1 {
		t.Fatalf("inline image placements = %d, want 1", len(app.InlineImagePlacements))
	}
	if got := app.InlineImagePlacements[0].Height; got != 7 {
		t.Fatalf("thumbnail height = %d, want 7", got)
	}
}

func TestCollapsedMessageLinesBoundsLongPosts(t *testing.T) {
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	collapsed := collapsedMessageLines(lines, false)
	if len(collapsed) != collapsedMessageLineLimit {
		t.Fatalf("collapsed line count = %d, want %d", len(collapsed), collapsedMessageLineLimit)
	}
	if !strings.Contains(stripANSI(collapsed[len(collapsed)-1]), "33 more lines") {
		t.Fatalf("missing collapse indicator: %q", collapsed[len(collapsed)-1])
	}
	if expanded := collapsedMessageLines(lines, true); len(expanded) != len(lines) {
		t.Fatalf("expanded line count = %d, want %d", len(expanded), len(lines))
	}
}

func TestChatNavigationUsesCacheBeforeSettledLoad(t *testing.T) {
	firstName := "First"
	secondName := "Second"
	secondBody := "cached second conversation"
	app := NewApp()
	app.SelectedIndex = 0
	app.Chats = []Chat{
		{ID: "first", CachedDisplayName: &firstName},
		{ID: "second", CachedDisplayName: &secondName},
	}
	app.CachedMessages["second"] = []Message{{
		ID:   "cached-message",
		Body: &MessageBody{Content: &secondBody},
	}}
	model := NewModel(app, "client-id", "user-id")

	next, cmd := model.handleNormalModeKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if next.app.SelectedIndex != 1 {
		t.Fatalf("selected index = %d, want 1", next.app.SelectedIndex)
	}
	if len(next.app.Messages) != 1 || next.app.Messages[0].ID != "cached-message" {
		t.Fatalf("cached conversation was not shown immediately: %#v", next.app.Messages)
	}
	if cmd == nil {
		t.Fatal("expected a delayed settle command")
	}
	if next.navigationGeneration != 1 {
		t.Fatalf("navigation generation = %d, want 1", next.navigationGeneration)
	}
}

func TestMouseWheelScrollsChatTimelineWithoutChangingSelection(t *testing.T) {
	firstName := "First"
	secondName := "Second"
	app := NewApp()
	app.Chats = []Chat{
		{ID: "first", CachedDisplayName: &firstName},
		{ID: "second", CachedDisplayName: &secondName},
	}
	app.SelectedIndex = 1
	app.ScrollOffset = 12
	app.MaxScroll = 40
	app.SnapToBottom = true
	model := NewModel(app, "client-id", "user-id")

	next, cmd := model.updateInternal(tea.MouseMsg(tea.MouseEvent{
		X:      0,
		Y:      5,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelUp,
	}))
	if cmd != nil {
		t.Fatal("unexpected command while scrolling loaded chat messages")
	}
	if next.app.SelectedIndex != 1 || next.channelSelectedIndex != -1 {
		t.Fatalf("mouse wheel changed chat selection: chat=%d channel=%d", next.app.SelectedIndex, next.channelSelectedIndex)
	}
	if next.app.ScrollOffset != 9 || next.app.SnapToBottom {
		t.Fatalf("chat timeline scroll state = offset %d, snap %v", next.app.ScrollOffset, next.app.SnapToBottom)
	}
}

func TestMouseWheelScrollsChannelTimelineWithoutChangingSelection(t *testing.T) {
	app := NewApp()
	app.Features.TeamsChannels = true
	app.TeamsData = []TeamWithChannels{{
		Team: Team{ID: "team", DisplayName: "Engineering"},
		Channels: []Channel{
			{ID: "general", DisplayName: "General"},
			{ID: "releases", DisplayName: "Releases"},
		},
	}}
	app.ScrollOffset = 7
	app.MaxScroll = 20
	model := NewModel(app, "client-id", "user-id")
	model.channelSelectedIndex = 1
	app.SelectedChannelTeamID = "team"
	app.SelectedChannelID = "releases"

	next, cmd := model.updateInternal(tea.MouseMsg(tea.MouseEvent{
		X:      0,
		Y:      5,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelDown,
	}))
	if cmd != nil {
		t.Fatal("unexpected command while scrolling loaded channel messages")
	}
	if next.channelSelectedIndex != 1 {
		t.Fatalf("mouse wheel changed channel selection to %d", next.channelSelectedIndex)
	}
	if next.app.ScrollOffset != 10 {
		t.Fatalf("channel timeline offset = %d, want 10", next.app.ScrollOffset)
	}
}

func TestEnhancedComposeShortcutsInsertNewlinesAndToggleImportance(t *testing.T) {
	app := NewApp()
	app.InputMode = true
	model := NewModel(app, "client-id", "user-id")
	model.textarea.SetValue("first line")

	next, _ := model.updateInternal(testCSISequence("\x1b[13;2u"))
	if !next.app.InputMode || next.textarea.Value() != "first line\n" {
		t.Fatalf("Shift+Enter compose state = input:%v value:%q", next.app.InputMode, next.textarea.Value())
	}

	next, _ = next.updateInternal(testCSISequence("\x1b[47;9u"))
	if !next.app.ComposeImportant {
		t.Fatal("Cmd+/ did not enable Important message mode")
	}

	next, _ = next.updateInternal(testCSISequence("\x1b[47;9u"))
	if next.app.ComposeImportant {
		t.Fatal("second Cmd+/ did not toggle Important message mode back off")
	}

	next, _ = next.updateInternal(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if next.textarea.Value() != "first line\n\n" {
		t.Fatalf("Alt+Enter inserted an unexpected number of newlines: %q", next.textarea.Value())
	}

	next, _ = next.updateInternal(testCSISequence("\x1b[27u"))
	if next.app.InputMode {
		t.Fatal("enhanced Escape key did not cancel compose mode")
	}
}

func TestCommandCopyCopiesWholeSelectedMessage(t *testing.T) {
	originalWrite := clipboardWriteAll
	defer func() { clipboardWriteAll = originalWrite }()

	copied := ""
	clipboardWriteAll = func(text string) error {
		copied = text
		return nil
	}

	body := "<p>Hello <strong>team</strong>.</p><p>Second line.</p>"
	app := NewApp()
	app.Messages = []Message{{
		ID:   "message-id",
		Body: &MessageBody{Content: &body},
	}}
	app.MessageSelectionMode = true
	app.MessageSelectedIndex = 0
	model := NewModel(app, "client-id", "user-id")

	next, _ := model.updateInternal(testCSISequence("\x1b[99;9u"))
	if copied != "Hello team.\nSecond line." {
		t.Fatalf("Cmd+C copied %q", copied)
	}
	if !next.app.MessageSelectionMode {
		t.Fatal("Cmd+C unexpectedly exited message-selection mode")
	}
	if next.app.Status != "Whole message copied to clipboard" {
		t.Fatalf("copy status = %q", next.app.Status)
	}
}

func TestCopyableMessageTextIncludesDetachedCard(t *testing.T) {
	body := ""
	contentType := "application/vnd.microsoft.card.adaptive"
	content := `{"type":"AdaptiveCard","body":[{"type":"TextBlock","text":"Deployment approved"}]}`
	message := Message{
		ID:   "card-message",
		Body: &MessageBody{Content: &body},
		Attachments: []MessageAttachment{{
			ID:          "card-id",
			ContentType: &contentType,
			Content:     &content,
		}},
	}

	if got := copyableMessageText(&message); got != "Deployment approved" {
		t.Fatalf("copyable card text = %q", got)
	}
}

func TestKittyKeyEventConvertsLegacyControlKeys(t *testing.T) {
	event, ok := parseKittyKeyEvent(testCSISequence("\x1b[99;5u"))
	if !ok {
		t.Fatal("could not parse Kitty Ctrl+C event")
	}
	key, ok := event.bubbleTeaKeyMsg()
	if !ok || key.Type != tea.KeyCtrlC || key.String() != "ctrl+c" {
		t.Fatalf("converted key = %#v, ok=%v", key, ok)
	}
}

func TestImportantMessagePayloadUsesGraphHighImportance(t *testing.T) {
	body := map[string]any{"contentType": "html", "content": "Please review"}
	important := buildChatMessagePayload(body, nil, nil, nil, true)
	if important["importance"] != "high" {
		t.Fatalf("important payload = %#v", important)
	}
	normal := buildChatMessagePayload(body, nil, nil, nil, false)
	if _, exists := normal["importance"]; exists {
		t.Fatalf("normal payload unexpectedly contains importance: %#v", normal)
	}
}

func TestFriendlyPreviewErrorDoesNotExposeGraphDetails(t *testing.T) {
	err := fmt.Errorf("GET /shares/u!sensitive/driveItem: HTTP 403: accessDenied")
	if got := friendlyPreviewError(err); got != "file permission required — re-authenticate" {
		t.Fatalf("friendly preview error = %q", got)
	}
}

func TestFavouriteChannelsSortFirstAndRenderStar(t *testing.T) {
	app := NewApp()
	app.Features.TeamsChannels = true
	app.TeamsDataLoading = false
	app.TeamsData = []TeamWithChannels{
		{
			Team: Team{ID: "team-a", DisplayName: "Engineering"},
			Channels: []Channel{
				{ID: "general", DisplayName: "General"},
				{ID: "releases", DisplayName: "Releases"},
			},
		},
		{
			Team:     Team{ID: "team-b", DisplayName: "Operations"},
			Channels: []Channel{{ID: "alerts", DisplayName: "Alerts"}},
		},
	}
	model := NewModel(app, "client-id", "user-id")
	model.unhiddenChannels = map[string]bool{"general": true, "releases": true, "alerts": true}
	model.channelFavourites = map[string]bool{"alerts": true}

	channels := model.allChannels()
	if len(channels) != 3 || channels[0].channelID != "alerts" {
		t.Fatalf("channel order = %#v, want alerts first", channels)
	}
	view := stripANSI(model.renderChatList(60, 16))
	if !strings.Contains(view, "★ # Operations » Alerts") {
		t.Fatalf("favourite star missing from channel list:\n%s", view)
	}
}

func TestChannelFavouriteTogglePersistsAndPreservesSelection(t *testing.T) {
	withTempConfigHome(t)
	app := NewApp()
	app.Features.TeamsChannels = true
	app.TeamsData = []TeamWithChannels{{
		Team: Team{ID: "team", DisplayName: "Engineering"},
		Channels: []Channel{
			{ID: "general", DisplayName: "General"},
			{ID: "releases", DisplayName: "Releases"},
		},
	}}
	model := NewModel(app, "client-id", "user-id")
	model.unhiddenChannels = map[string]bool{"general": true, "releases": true}
	model.channelSelectedIndex = 1
	selectedID := model.allChannels()[model.channelSelectedIndex].channelID
	app.SelectedChannelTeamID = "team"
	app.SelectedChannelID = selectedID

	next, _ := model.handleNormalModeKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	if !next.channelFavourites[selectedID] {
		t.Fatalf("channel %q was not favourited", selectedID)
	}
	if got := next.allChannels()[next.channelSelectedIndex].channelID; got != selectedID {
		t.Fatalf("selected channel changed to %q, want %q", got, selectedID)
	}
	if persisted := LoadChannelFavourites(); !persisted[selectedID] {
		t.Fatalf("persisted channel favourites = %#v", persisted)
	}
}

func TestPanelWidthsStayValidInNarrowTerminals(t *testing.T) {
	for total := 3; total <= 80; total++ {
		chatWidth := chatPanelWidth(total)
		messageWidth := msgPanelWidth(total)
		if chatWidth < 0 || messageWidth < 0 {
			t.Fatalf("total %d produced negative widths: chat=%d message=%d", total, chatWidth, messageWidth)
		}
		if chatWidth+messageWidth+2 > total {
			t.Fatalf("total %d overflowed: chat=%d message=%d", total, chatWidth, messageWidth)
		}
	}
}

func TestInlinePreviewQueueHonorsConcurrencyLimit(t *testing.T) {
	app := NewApp()
	app.Features.FilePreviewInTerminal = true
	model := NewModel(app, "client-id", "user-id")
	for i := 0; i < 12; i++ {
		name := fmt.Sprintf("preview-%d.png", i)
		contentType := "image/png"
		contentURL := fmt.Sprintf("https://example.com/preview-%d.png?run=concurrency", i)
		app.Messages = append(app.Messages, Message{
			ID: fmt.Sprintf("message-%d", i),
			Attachments: []MessageAttachment{{
				ID:          fmt.Sprintf("attachment-%d", i),
				Name:        &name,
				ContentType: &contentType,
				ContentURL:  &contentURL,
			}},
		})
	}

	if cmd := model.queueInlineImagePreviews(4); cmd == nil {
		t.Fatal("expected preview downloads to be queued")
	}
	if got := len(model.previewDownloads); got != 4 {
		t.Fatalf("active preview downloads = %d, want 4", got)
	}
	if cmd := model.queueInlineImagePreviews(4); cmd != nil {
		t.Fatal("queue should not exceed the active download limit")
	}
	if got := len(model.previewDownloads); got != 4 {
		t.Fatalf("active preview downloads changed to %d, want 4", got)
	}
}

func TestPersistentKittyImageTransmitsOnceThenPlaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preview.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 16, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: 20, G: 180, B: 220, A: 255})
		}
	}
	if err := png.Encode(file, img); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	model := NewModel(NewApp(), "client-id", "user-id")
	first := model.persistentKittySequence(path, 2, 3, 20, 6, 1)
	if !strings.Contains(first, "a=t") || !strings.Contains(first, "a=p") {
		t.Fatalf("first sequence must transmit and place: %q", first)
	}
	second := model.persistentKittySequence(path, 4, 5, 20, 6, 2)
	if strings.Contains(second, "a=t") || !strings.Contains(second, "a=p") {
		t.Fatalf("second sequence must only place: %q", second)
	}
}

func TestMessagePopupPreviewDirectlyDisplaysOnEveryRedraw(t *testing.T) {
	withTempConfigHome(t)
	name := "inline-image-1.png"
	contentType := "image/png"
	contentURL := "https://graph.microsoft.com/v1.0/chats/chat-id/messages/message-id/hostedContents/content-id/$value"
	chatName := "Design"
	att := MessageAttachment{
		ID:          "content-id",
		Name:        &name,
		ContentType: &contentType,
		ContentURL:  &contentURL,
	}
	cachePath, err := getAttachmentCachePath(att)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 16, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: 30, G: 190, B: 120, A: 255})
		}
	}
	if err := png.Encode(file, img); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.SelectedIndex = 0
	app.Features.FilePreviewInTerminal = true
	app.Chats = []Chat{{ID: "chat-id", CachedDisplayName: &chatName}}
	app.Messages = []Message{{ID: "message-id", Attachments: []MessageAttachment{att}}}
	app.MessagePopupMode = true
	app.AttachmentCursorMode = true
	app.MessageSelectedIndex = 0
	app.AttachmentSelectedIndex = 0
	model := NewModel(app, "client-id", "user-id")
	model.width = 120
	model.height = 40

	for redraw := 1; redraw <= 2; redraw++ {
		view := model.View()
		if !strings.Contains(view, "\x1b_Ga=T,f=100,") || !strings.Contains(view, "p=900") {
			t.Fatalf("redraw %d did not directly transmit and display popup image", redraw)
		}
		if strings.Contains(view, "\x1b_Ga=p,") {
			t.Fatalf("redraw %d used an indirect popup placement", redraw)
		}
	}
}

func TestKittyPlacementMatchesCmuxGhosttySequence(t *testing.T) {
	prepared := kittyPreparedImage{Cols: 20, Rows: 6, PadX: 2, PadY: 1}
	got := kittyPlaceSequence(prepared, 3, 1, 4, 6)
	want := "\x1b7\x1b[8;7H\x1b_Ga=p,i=3,p=1,c=20,r=6,q=2;\x1b\\\x1b8"
	if got != want {
		t.Fatalf("placement sequence = %q, want %q", got, want)
	}
}

func TestKittyTransmissionUsesQuietContinuationChunks(t *testing.T) {
	prepared := kittyPreparedImage{Encoded: strings.Repeat("a", 4096) + "bbbb", Cols: 1, Rows: 1}
	got := kittyTransmitSequence(prepared, 8)
	if !strings.Contains(got, "\x1b_Ga=t,f=100,i=8,q=2,m=1;") {
		t.Fatalf("missing first transmission chunk: %q", got[:min(len(got), 120)])
	}
	if !strings.Contains(got, "\x1b_Gq=2,m=0;bbbb\x1b\\") {
		t.Fatalf("missing quiet continuation chunk")
	}
}

func TestKittyImageIDUsesCmuxCompatibleRange(t *testing.T) {
	for _, key := range []string{"small", strings.Repeat("high-bit", 100), "বাংলা-image"} {
		id := kittyImageID(key)
		if id < 1 || id > 2_000_000_000 {
			t.Fatalf("image id %d is outside cmux-compatible range", id)
		}
	}
}

func TestQuickPreviewSelectedAttachmentUsesPrivateCache(t *testing.T) {
	withTempConfigHome(t)
	name := "quarterly-report.pdf"
	contentType := "application/pdf"
	contentURL := "https://graph.microsoft.com/v1.0/drives/drive/items/item/content"
	chatName := "Finance"
	att := MessageAttachment{Name: &name, ContentType: &contentType, ContentURL: &contentURL}
	app := NewApp()
	app.Features.FilePreview = true
	app.AttachmentCursorMode = true
	app.AttachmentSelectedIndex = 0
	app.MessageSelectedIndex = 0
	app.SelectedIndex = 0
	app.Chats = []Chat{{ID: "chat-id", CachedDisplayName: &chatName}}
	app.Messages = []Message{{ID: "message-id", Attachments: []MessageAttachment{att}}}
	model := NewModel(app, "client-id", "user-id")

	cachePath, err := getAttachmentCachePath(att)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePrivateFile(cachePath, []byte("%PDF-1.7\n")); err != nil {
		t.Fatal(err)
	}
	cmd := model.quickPreviewSelectedAttachment()
	if cmd == nil {
		t.Fatal("expected cached quick preview command")
	}
	msg, ok := cmd().(MsgPreviewDownloaded)
	if !ok || !msg.QuickLook || msg.DestPath != cachePath || msg.Err != nil {
		t.Fatalf("quick preview message = %#v", msg)
	}
}

func BenchmarkRenderMessagesLongPosts(b *testing.B) {
	chatName := "Engineering"
	longBody := strings.Repeat("SELECT id, slug FROM doctors WHERE slug IS NOT NULL;\n", 80)
	app := NewApp()
	app.SelectedIndex = 0
	app.Chats = []Chat{{ID: "chat-id", CachedDisplayName: &chatName}}
	for i := 0; i < 50; i++ {
		body := longBody
		app.Messages = append(app.Messages, Message{
			ID:              fmt.Sprintf("message-%d", i),
			CreatedDateTime: "2026-07-10T00:00:00Z",
			Body:            &MessageBody{Content: &body},
		})
	}
	model := NewModel(app, "client-id", "user-id")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = model.renderMessages(120, 40)
	}
}

func TestFitFrameReservesWrapColumn(t *testing.T) {
	got := fitFrame(strings.Repeat("x", 30)+"\n"+strings.Repeat("y", 30)+"\nz", 10, 2)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 frame lines, got %d: %q", len(lines), got)
	}
	for i, line := range lines {
		if w := cellWidth(line); w > 9 {
			t.Fatalf("line %d width = %d, want <= 9: %q", i, w, line)
		}
	}
}

func TestSynchronizedWriterWrapsEachRepaint(t *testing.T) {
	var buf bytes.Buffer
	w := newSynchronizedWriter(&buf)
	if _, err := w.Write([]byte("frame")); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	want := beginSynchronizedOutput + "frame" + endSynchronizedOutput
	if got := buf.String(); got != want {
		t.Fatalf("synchronized frame = %q, want %q", got, want)
	}
}

func TestResolveAttachmentContentURL(t *testing.T) {
	att := MessageAttachment{ContentURL: testStringPtr("../hostedContents/1/$value")}

	chatURL := resolveAttachmentContentURL(Message{ID: "message-id"}, att, "chat-id", "", "")
	wantChat := graphAPIBase + "/chats/chat-id/messages/message-id/hostedContents/1/$value"
	if chatURL != wantChat {
		t.Fatalf("chat URL = %q, want %q", chatURL, wantChat)
	}

	channelURL := resolveAttachmentContentURL(Message{ID: "root-id"}, att, "", "team-id", "channel-id")
	wantChannel := graphAPIBase + "/teams/team-id/channels/channel-id/messages/root-id/hostedContents/1/$value"
	if channelURL != wantChannel {
		t.Fatalf("channel URL = %q, want %q", channelURL, wantChannel)
	}

	reply := Message{ID: "reply-id", IsReply: true, ReplyToID: "root-id"}
	replyURL := resolveAttachmentContentURL(reply, att, "", "team-id", "channel-id")
	wantReply := graphAPIBase + "/teams/team-id/channels/channel-id/messages/root-id/replies/reply-id/hostedContents/1/$value"
	if replyURL != wantReply {
		t.Fatalf("channel reply URL = %q, want %q", replyURL, wantReply)
	}

	absolute := "https://example.com/image.png"
	att.ContentURL = &absolute
	if got := resolveAttachmentContentURL(Message{}, att, "chat-id", "", ""); got != absolute {
		t.Fatalf("absolute URL changed to %q", got)
	}
}
