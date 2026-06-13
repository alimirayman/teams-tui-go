package main

import (
	"testing"
)

func TestMarkdownToHTMLWithLinks(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Plain text, no links",
			input:    "Hello world",
			expected: "<p>Hello world</p>",
		},
		{
			name:     "Plain URL",
			input:    "Go to https://google.com",
			expected: `<p>Go to <a href="https://google.com">https://google.com</a></p>`,
		},
		{
			name:     "Plain URL with trailing dot",
			input:    "Go to https://google.com.",
			expected: `<p>Go to <a href="https://google.com">https://google.com</a>.</p>`,
		},
		{
			name:     "Plain URL with trailing comma",
			input:    "Go to https://google.com, which is cool",
			expected: `<p>Go to <a href="https://google.com">https://google.com</a>, which is cool</p>`,
		},
		{
			name:     "Plain URL with query parameters",
			input:    "Check https://google.com/search?q=query&hl=en",
			expected: `<p>Check <a href="https://google.com/search?q=query&amp;hl=en">https://google.com/search?q=query&amp;hl=en</a></p>`,
		},
		{
			name:     "Markdown link",
			input:    "Click [Google](https://google.com)",
			expected: `<p>Click <a href="https://google.com">Google</a></p>`,
		},
		{
			name:     "Mixed markdown link and plain URL",
			input:    "Click [Google](https://google.com) or go to https://github.com.",
			expected: `<p>Click <a href="https://google.com">Google</a> or go to <a href="https://github.com">https://github.com</a>.</p>`,
		},
		{
			name:     "URL inside backticks (inline code)",
			input:    "Do not linkify `https://google.com` here",
			expected: `<p>Do not linkify <code>https://google.com</code> here</p>`,
		},
		{
			name:     "URL inside multiline code block",
			input:    "```go\n// https://google.com\nfmt.Println(\"ok\")\n```",
			expected: `<pre><code class="language-go">// https://google.com
fmt.Println(&#34;ok&#34;)</code></pre>`,
		},
		{
			name:     "URL with matching parenthesis",
			input:    "Look at https://en.wikipedia.org/wiki/URL_(web_address)",
			expected: `<p>Look at <a href="https://en.wikipedia.org/wiki/URL_(web_address)">https://en.wikipedia.org/wiki/URL_(web_address)</a></p>`,
		},
		{
			name:     "URL inside outer parenthesis",
			input:    "Please look here (https://google.com)",
			expected: `<p>Please look here (<a href="https://google.com">https://google.com</a>)</p>`,
		},
		{
			name:     "URL with brackets",
			input:    "Look at [https://google.com]",
			expected: `<p>Look at [<a href="https://google.com">https://google.com</a>]</p>`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := markdownToHTML(tc.input)
			if got != tc.expected {
				t.Errorf("\nInput:    %s\nExpected: %s\nGot:      %s", tc.input, tc.expected, got)
			}
		})
	}
}

func TestContainsURL(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"http://google.com", true},
		{"https://github.com", true},
		{"Hello http://world", true},
		{"Hello", false},
		{"ftp://files.com", false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := containsURL(tc.input)
			if got != tc.expected {
				t.Errorf("containsURL(%q) = %v; expected %v", tc.input, got, tc.expected)
			}
		})
	}
}
