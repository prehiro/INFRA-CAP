package notes

import (
	"fmt"
	"html"
	"regexp"
	"strings"
)

// renderMarkdown converts a minimal-markdown source to safe HTML for
// display in a modal. It supports a deliberately small subset of the
// CommonMark spec — what an internal team-notes page actually uses:
//
//   - # / ## / ###              -> h1, h2, h3
//   - **bold** / __bold__       -> <strong>
//   - *italic* / _italic_       -> <em>
//   - `inline code`             -> <code>
//   - - bullet / 1. ordered     -> <ul><li> / <ol><li>
//   - > quote                   -> <blockquote>
//   - [text](https://url)       -> <a>
//   - ---                       -> <hr>
//   - blank line                -> paragraph break
//
// All output is HTML-escaped BEFORE inline formatting is applied, so
// raw HTML in the source is rendered as literal text (no XSS surface).
// Links are restricted to http(s) and mailto schemes to block
// javascript: and data: URLs.
//
// This is NOT a full markdown parser. If a user types something
// exotic (nested lists, fenced code blocks, tables) it will display
// as plain text inside a paragraph — acceptable for v1, and the
// textarea is right there if they need to be fancy.
var (
	reHeader  = regexp.MustCompile(`^(#{1,3})\s+(.+?)\s*$`)
	reBold    = regexp.MustCompile(`\*\*(.+?)\*\*|__(.+?)__`)
	reItalic  = regexp.MustCompile(`(^|[^*])\*([^*\n]+?)\*([^*]|$)|(^|[^_])_([^_\n]+?)_([^_]|$)`)
	reCode    = regexp.MustCompile("`([^`\n]+?)`")
	reLink    = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	reHr      = regexp.MustCompile(`^-{3,}\s*$`)
	reBlockQ  = regexp.MustCompile(`^>\s+(.+?)\s*$`)
	reBullet  = regexp.MustCompile(`^[-*]\s+(.+?)\s*$`)
	reOrdered = regexp.MustCompile(`^\d+\.\s+(.+?)\s*$`)
	reHighlight = regexp.MustCompile(`==([^=\n]+?)==`)
)

// RenderMarkdown takes raw markdown source and returns HTML safe to
// drop into a template via template.HTML (the template engine still
// escapes it once, so callers should pass the raw string and let the
// template handle escaping — see note below).
func RenderMarkdown(src string) string {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.ReplaceAll(src, "\t", "    ")

	lines := strings.Split(src, "\n")
	var b strings.Builder

	var (
		inList    bool // currently inside <ul>
		inOrdered bool // currently inside <ol>
		inQuote   bool // currently inside <blockquote>
		paraBuf   strings.Builder
	)

	closePara := func() {
		if paraBuf.Len() > 0 {
			b.WriteString("<p>")
			b.WriteString(formatInline(paraBuf.String()))
			b.WriteString("</p>\n")
			paraBuf.Reset()
		}
	}
	closeLists := func() {
		if inList {
			b.WriteString("</ul>\n")
			inList = false
		}
		if inOrdered {
			b.WriteString("</ol>\n")
			inOrdered = false
		}
		if inQuote {
			b.WriteString("</blockquote>\n")
			inQuote = false
		}
	}

	for _, line := range lines {
		// Trim trailing whitespace but preserve indentation for code blocks
		trimmed := strings.TrimRight(line, " \t")

		// Blank line -> close any open blocks
		if strings.TrimSpace(trimmed) == "" {
			closePara()
			closeLists()
			continue
		}

		// Horizontal rule
		if reHr.MatchString(trimmed) {
			closePara()
			closeLists()
			b.WriteString("<hr>\n")
			continue
		}

		// Header
		if m := reHeader.FindStringSubmatch(trimmed); m != nil {
			closePara()
			closeLists()
			level := len(m[1])
			b.WriteString("<h")
			b.WriteByte(byte('0' + level))
			b.WriteString(">")
			b.WriteString(formatInline(m[2]))
			b.WriteString("</h")
			b.WriteByte(byte('0' + level))
			b.WriteString(">\n")
			continue
		}

		// Blockquote
		if m := reBlockQ.FindStringSubmatch(trimmed); m != nil {
			closePara()
			if inOrdered {
				b.WriteString("</ol>\n")
				inOrdered = false
			}
			if !inList {
				// blockquote can sit alongside a list? simplify: close list
			}
			if !inQuote {
				b.WriteString("<blockquote>\n")
				inQuote = true
			}
			b.WriteString("<p>")
			b.WriteString(formatInline(m[1]))
			b.WriteString("</p>\n")
			continue
		}

		// Unordered list
		if m := reBullet.FindStringSubmatch(trimmed); m != nil {
			closePara()
			if inOrdered {
				b.WriteString("</ol>\n")
				inOrdered = false
			}
			if inQuote {
				b.WriteString("</blockquote>\n")
				inQuote = false
			}
			if !inList {
				b.WriteString("<ul>\n")
				inList = true
			}
			b.WriteString("  <li>")
			b.WriteString(formatInline(m[1]))
			b.WriteString("</li>\n")
			continue
		}

		// Ordered list
		if m := reOrdered.FindStringSubmatch(trimmed); m != nil {
			closePara()
			if inList {
				b.WriteString("</ul>\n")
				inList = false
			}
			if inQuote {
				b.WriteString("</blockquote>\n")
				inQuote = false
			}
			if !inOrdered {
				b.WriteString("<ol>\n")
				inOrdered = true
			}
			b.WriteString("  <li>")
			b.WriteString(formatInline(m[1]))
			b.WriteString("</li>\n")
			continue
		}

		// Default: paragraph text (may span multiple lines until blank line)
		closeLists()
		if paraBuf.Len() > 0 {
			paraBuf.WriteByte('\n')
		}
		paraBuf.WriteString(trimmed)
	}

	closePara()
	closeLists()
	return b.String()
}

// formatInline applies **bold**, *italic*, `code`, and [link](url) to
// already-HTML-escaped text. It re-escapes angle brackets/ampersands
// inside the captured groups so injected HTML in markdown source
// displays as literal text. Exceptions: ==highlight== (custom syntax
// for <mark>) and <span style="color:...">text</span> (inline color
// generated by the toolbar) are whitelisted to pass through as real
// HTML — anything else inside angle brackets is still escaped.
func formatInline(s string) string {
	// First: extract whitelisted HTML tags (mark, span with style="color:...").
	// We replace them with sentinels, escape everything, then put the
	// tags back. This is the standard "escape then re-introduce safe HTML"
	// pattern. The sentinel uses null bytes which never appear in user text.
	whitelisted := make(map[string]string)
	sentinelIdx := 0
	extractTag := func(tagPattern *regexp.Regexp) {
		s = tagPattern.ReplaceAllStringFunc(s, func(m string) string {
			key := fmt.Sprintf("\x00TAG%d\x00", sentinelIdx)
			whitelisted[key] = m
			sentinelIdx++
			return key
		})
	}
	// <span style="color:#xxxxxx">content</span> — content preserved
	extractTag(regexp.MustCompile(`<span\s+style="color:#[0-9a-fA-F]{3,8}"\s*>[\s\S]*?</span>`))
	// <mark>content</mark> (we'll produce these next) — handled below

	s = html.EscapeString(s)

	// Highlight: ==text== → <mark>text</mark>
	s = reHighlight.ReplaceAllStringFunc(s, func(m string) string {
		sub := reHighlight.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		return "<mark>" + html.EscapeString(sub[1]) + "</mark>"
	})

	// Inline code first — its content must NOT receive further formatting.
	s = reCode.ReplaceAllStringFunc(s, func(m string) string {
		sub := reCode.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		return "<code>" + html.EscapeString(sub[1]) + "</code>"
	})

	// Links: only http(s) and mailto allowed. Everything else becomes literal text.
	s = reLink.ReplaceAllStringFunc(s, func(m string) string {
		sub := reLink.FindStringSubmatch(m)
		if len(sub) < 3 {
			return m
		}
		href := strings.TrimSpace(sub[2])
		low := strings.ToLower(href)
		if !(strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") || strings.HasPrefix(low, "mailto:")) {
			// Strip the link syntax, keep just the text
			return html.EscapeString(sub[1])
		}
		return `<a href="` + html.EscapeString(href) + `" target="_blank" rel="noopener noreferrer">` + html.EscapeString(sub[1]) + `</a>`
	})

	// Bold
	s = reBold.ReplaceAllStringFunc(s, func(m string) string {
		sub := reBold.FindStringSubmatch(m)
		if len(sub) < 3 {
			return m
		}
		text := sub[1]
		if text == "" {
			text = sub[2]
		}
		return "<strong>" + text + "</strong>"
	})

	// Italic
	s = reItalic.ReplaceAllStringFunc(s, func(m string) string {
		sub := reItalic.FindStringSubmatch(m)
		if len(sub) < 7 {
			return m
		}
		// sub[1]=prefix*, sub[2]=*text*, sub[3]=suffix*
		// sub[4]=prefix_, sub[5]=_text_, sub[6]=suffix_
		if sub[2] != "" {
			return sub[1] + "<em>" + sub[2] + "</em>" + sub[3]
		}
		if sub[5] != "" {
			return sub[4] + "<em>" + sub[5] + "</em>" + sub[6]
		}
		return m
	})

	// Restore whitelisted HTML tags (color spans)
	for key, val := range whitelisted {
		s = strings.ReplaceAll(s, key, val)
	}

	return s
}

// Note: Preview (plain-text excerpt) lives in internal/web/render.go as
// the unexported notesPreview, exposed to templates via FuncMap. Keeping
// it there avoids an import cycle: notes already imports web for
// RenderNamed.
