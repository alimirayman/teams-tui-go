package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
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
	if width := ansi.StringWidth(got); width > 16 {
		t.Fatalf("Bengali line width = %d, want <= 16: %q", width, got)
	}
	if strings.ContainsRune(got, '\uFFFD') {
		t.Fatalf("Bengali line contains a broken UTF-8 replacement rune: %q", got)
	}
}

func TestFitFrameReservesWrapColumn(t *testing.T) {
	got := fitFrame(strings.Repeat("x", 30)+"\n"+strings.Repeat("y", 30)+"\nz", 10, 2)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 frame lines, got %d: %q", len(lines), got)
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w > 9 {
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
