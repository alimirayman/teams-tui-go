package main

import (
	"bytes"
	"strings"
	"testing"
)

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
	if got := app.InlineImagePlacements[0].Height; got != 10 {
		t.Fatalf("thumbnail height = %d, want 10", got)
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
