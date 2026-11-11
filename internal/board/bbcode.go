package board

import (
	"fmt"
	"html"
	"html/template"
	"regexp"
	"strconv"
	"strings"

	"agentboard/internal/store"
)

// renderBBCode escapes post text, then converts BBCode, bare URLs and @mentions
// to HTML. Only the tags added by the renderer reach the page as markup.

var (
	reTagCase = regexp.MustCompile(`(?i)\[(/?)(quote|code|b|i|u|url)\b`)
	reCode    = regexp.MustCompile(`(?s)\[code\]\n?(.*?)\n?\[/code\]`)
	reBold    = regexp.MustCompile(`(?s)\[b\](.*?)\[/b\]`)
	reItalic  = regexp.MustCompile(`(?s)\[i\](.*?)\[/i\]`)
	reUnder   = regexp.MustCompile(`(?s)\[u\](.*?)\[/u\]`)
	// A link never takes in a block marker, the NUL character. Otherwise a link
	// written next to a quote would carry the quote into its href, and once as
	// the link text, which doubles the quote with every level of nesting.
	reURLNamed  = regexp.MustCompile(`\[url=(?:&#34;)?(https?://[^\]\s\x00]+?)(?:&#34;)?\](.*?)\[/url\]`)
	reURLPlain  = regexp.MustCompile(`\[url\](https?://[^\[\s\x00]+)\[/url\]`)
	reBareURL   = regexp.MustCompile(`https?://[^\s<\x00]+[^\s<.,;:!?)\]'"\x00]`)
	reHTMLSplit = regexp.MustCompile(`(<[^>]+>)`)
	reBlockNL   = regexp.MustCompile(`\n?(\x00[QC]\d+\x00)\n?`)
	reBlockRef  = regexp.MustCompile(`\x00[QC](\d+)\x00`)
)

func renderBBCode(body string) template.HTML {
	// NUL characters identify saved blocks. Remove them from input, including
	// older posts stored before input validation rejected them.
	s := strings.ReplaceAll(body, "\x00", "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = html.EscapeString(s)
	s = reTagCase.ReplaceAllStringFunc(s, strings.ToLower)

	// Save code blocks before formatting the rest of the text.
	var blocks []string
	s = reCode.ReplaceAllStringFunc(s, func(m string) string {
		inner := reCode.FindStringSubmatch(m)[1]
		blocks = append(blocks, `<div class="bbcode-block"><div class="smallfont bb-label">Code:</div><pre class="alt2 bb-code">`+inner+`</pre></div>`)
		return fmt.Sprintf("\x00C%d\x00", len(blocks)-1)
	})

	// Quotes nest, so resolve the innermost one each pass.
	for {
		end := strings.Index(s, "[/quote]")
		if end < 0 {
			break
		}
		start := strings.LastIndex(s[:end], "[quote")
		if start < 0 {
			break
		}
		closeTag := strings.Index(s[start:end], "]")
		if closeTag < 0 {
			break
		}
		attr := s[start+len("[quote") : start+closeTag]
		inner := strings.Trim(s[start+closeTag+1:end], "\n")
		who := strings.Trim(strings.TrimPrefix(attr, "="), " \"'")
		who = strings.TrimSuffix(strings.TrimPrefix(who, "&#34;"), "&#34;")
		who = strings.TrimSuffix(strings.TrimPrefix(who, "&#39;"), "&#39;")
		var head string
		if who != "" {
			head = `<div>Originally Posted by <strong>` + who + `</strong></div>`
		}
		blocks = append(blocks, `<div class="bbcode-block"><div class="smallfont bb-label">Quote:</div><div class="alt2 bb-quote">`+head+`<div class="bb-quote-text">`+formatInline(inner)+`</div></div></div>`)
		s = s[:start] + fmt.Sprintf("\x00Q%d\x00", len(blocks)-1) + s[end+len("[/quote]"):]
	}

	s = formatInline(s)
	s = expandBlocks(s, blocks)
	return template.HTML(s)
}

func formatInline(s string) string {
	s = reBold.ReplaceAllString(s, "<strong>$1</strong>")
	s = reItalic.ReplaceAllString(s, "<em>$1</em>")
	s = reUnder.ReplaceAllString(s, "<u>$1</u>")
	s = reURLNamed.ReplaceAllString(s, `<a href="$1" rel="nofollow ugc" target="_blank">$2</a>`)
	s = reURLPlain.ReplaceAllString(s, `<a href="$1" rel="nofollow ugc" target="_blank">$1</a>`)
	s = linkify(s)
	s = reBlockNL.ReplaceAllString(s, "$1")
	return strings.ReplaceAll(s, "\n", "<br>\n")
}

// linkify links bare URLs and @mentions in text that is not already a link.
func linkify(s string) string {
	parts := reHTMLSplit.Split(s, -1)
	tags := reHTMLSplit.FindAllString(s, -1)
	var b strings.Builder
	inLink := false
	for i, text := range parts {
		if !inLink {
			text = reBareURL.ReplaceAllString(text, `<a href="$0" rel="nofollow ugc" target="_blank">$0</a>`)
			text = store.MentionPattern.ReplaceAllString(text, `$1<a class="mention" href="/members/$2">@$2</a>`)
		}
		b.WriteString(text)
		if i < len(tags) {
			tag := tags[i]
			if strings.HasPrefix(tag, "<a ") {
				inLink = true
			} else if tag == "</a>" {
				inLink = false
			}
			b.WriteString(tag)
		}
	}
	return b.String()
}

// expandBlocks replaces markers with saved HTML. A quote may contain only
// earlier blocks, such as code. Each block expands at most once to prevent
// repeated markers from multiplying the output.
func expandBlocks(s string, blocks []string) string {
	used := make([]bool, len(blocks))
	var expand func(s string, upTo int) string
	expand = func(s string, upTo int) string {
		return reBlockRef.ReplaceAllStringFunc(s, func(m string) string {
			n, err := strconv.Atoi(m[2 : len(m)-1])
			if err != nil || n >= upTo || used[n] {
				return ""
			}
			used[n] = true
			return expand(blocks[n], n)
		})
	}
	return expand(s, len(blocks))
}
