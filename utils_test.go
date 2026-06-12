package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestNormalizeString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Jérémy", "Jeremy"},
		{"François", "Francois"},
		{"jeremy", "jeremy"},
		{"", ""},
		{"München", "Munchen"},
		{"Álvaro", "Alvaro"},
	}

	for _, test := range tests {
		result := normalizeString(test.input)
		if result != test.expected {
			t.Errorf("normalizeString(%q) = %q; expected %q", test.input, result, test.expected)
		}
	}
}

func TestHighlightQuery(t *testing.T) {
	// Force a color profile so that lipgloss outputs ANSI sequences during headless tests.
	lipgloss.SetColorProfile(termenv.TrueColor)

	tests := []struct {
		text     string
		query    string
		contains string // what the highlighted text should contain
	}{
		{"Hello Jérémy", "jeremy", "Jérémy"},
		{"Hello Jeremy", "jeremy", "Jeremy"},
		{"François is here", "francois", "François"},
		{"No match here", "jeremy", "No match here"},
	}

	for _, test := range tests {
		result := highlightQuery(test.text, test.query)
		if test.text == test.contains {
			// If it's a "No match here", result should be identical to input
			if result != test.text {
				t.Errorf("highlightQuery(%q, %q) = %q; expected no match", test.text, test.query, result)
			}
		} else {
			// If it matches, the original substring (with accents) should be in the result,
			// wrapped in some ANSI escape codes.
			if !strings.Contains(result, test.contains) {
				t.Errorf("highlightQuery(%q, %q) = %q; expected to contain %q", test.text, test.query, result, test.contains)
			}
			// And it should not be identical to original text
			if result == test.text {
				t.Errorf("highlightQuery(%q, %q) = %q; expected highlighted output, got original text", test.text, test.query, result)
			}
		}
	}
}

func TestMessageMatches(t *testing.T) {
	strPtr := func(s string) *string { return &s }

	msg := Message{
		ID: "1",
		From: &MessageFrom{
			User: &MessageUser{
				DisplayName: strPtr("Alice Smith"),
			},
		},
		Body: &MessageBody{
			Content: strPtr("<p>Hello world from Go</p>"),
		},
		Attachments: []MessageAttachment{
			{
				Name: strPtr("report.pdf"),
			},
		},
	}

	model := Model{}

	tests := []struct {
		query    string
		expected bool
	}{
		{"", true},
		{"hello", true},
		{"from Go", true},
		{"report", true},
		{"report.pdf", true},
		{"Alice", false}, // Should NOT match creator display name
		{"Smith", false}, // Should NOT match creator display name
		{"nonexistent", false},
	}

	for _, test := range tests {
		result := model.messageMatches(&msg, test.query)
		if result != test.expected {
			t.Errorf("messageMatches(query=%q) = %v; expected %v", test.query, result, test.expected)
		}
	}
}

func TestExtractAndProcessInlineImages(t *testing.T) {
	strPtr := func(s string) *string { return &s }

	htmlContent := `<p>Check out this image: <img src="https://graph.microsoft.com/v1.0/chats/123/messages/456/hostedContents/abc/$value" alt="My screenshot" /> and another: <img src="https://graph.microsoft.com/v1.0/chats/123/messages/456/hostedContents/def/$value" /></p>`

	// Test ExtractInlineImages
	inlineAtts := ExtractInlineImages(htmlContent)
	if len(inlineAtts) != 2 {
		t.Fatalf("expected 2 inline images, got %d", len(inlineAtts))
	}

	if *inlineAtts[0].Name != "My screenshot.png" {
		t.Errorf("expected first name 'My screenshot.png', got %q", *inlineAtts[0].Name)
	}
	if *inlineAtts[0].ContentURL != "https://graph.microsoft.com/v1.0/chats/123/messages/456/hostedContents/abc/$value" {
		t.Errorf("expected first URL to match, got %q", *inlineAtts[0].ContentURL)
	}

	if *inlineAtts[1].Name != "inline-image-2.png" {
		t.Errorf("expected second name 'inline-image-2.png', got %q", *inlineAtts[1].Name)
	}
	if *inlineAtts[1].ContentURL != "https://graph.microsoft.com/v1.0/chats/123/messages/456/hostedContents/def/$value" {
		t.Errorf("expected second URL to match, got %q", *inlineAtts[1].ContentURL)
	}

	// Test ProcessInlineImages
	msg := Message{
		ID: "1",
		Body: &MessageBody{
			Content: strPtr(htmlContent),
		},
		Attachments: []MessageAttachment{
			{
				ID:         "existing-doc",
				Name:       strPtr("report.pdf"),
				ContentURL: strPtr("https://sharepoint.com/report.pdf"),
			},
		},
	}

	msg.ProcessInlineImages()

	if len(msg.Attachments) != 3 {
		t.Errorf("expected 3 attachments after processing inline images, got %d", len(msg.Attachments))
	}

	// Double processing check
	msg.ProcessInlineImages()
	if len(msg.Attachments) != 3 {
		t.Errorf("expected attachments count to remain 3 on double processing, got %d", len(msg.Attachments))
	}

	// Test HTMLToText inline image naming
	plainText := HTMLToText(htmlContent, msg.Attachments)
	if !strings.Contains(plainText, "My screenshot.png") {
		t.Errorf("expected plainText to contain 'My screenshot.png', got %q", plainText)
	}
	if !strings.Contains(plainText, "inline-image-2.png") {
		t.Errorf("expected plainText to contain 'inline-image-2.png', got %q", plainText)
	}
}

