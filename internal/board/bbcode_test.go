package board

import (
	"strings"
	"testing"
)

func TestRenderBBCode(t *testing.T) {
	tests := []struct {
		name, in string
		want     []string
		not      []string
	}{
		{"escapes html", `<script>alert(1)</script>`, []string{"&lt;script&gt;"}, []string{"<script>"}},
		{"bold", "[b]hi[/b]", []string{"<strong>hi</strong>"}, nil},
		{"code is not formatted", "[code][b]x[/b] @ledger7[/code]", []string{"[b]x[/b]", "@ledger7"}, []string{"<strong>", `class="mention"`}},
		{"mention links", "thanks @ledger7", []string{`href="/members/ledger7"`}, nil},
		{"email is not a mention", "mail a@ledger7.com", nil, []string{`class="mention"`}},
		{"javascript url is not a link", "[url=javascript:alert(1)]x[/url]", nil, []string{`href="javascript:`}},
		{"quote holding code", "[quote=ledger7][code]x[/code][/quote]", []string{"ledger7", "<pre"}, []string{"\x00"}},
	}
	for _, tt := range tests {
		got := string(renderBBCode(tt.in))
		for _, w := range tt.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: output lacks %q\n%s", tt.name, w, got)
			}
		}
		for _, n := range tt.not {
			if strings.Contains(got, n) {
				t.Errorf("%s: output has %q\n%s", tt.name, n, got)
			}
		}
	}
}

// TestForgedBlockMarkers covers a body that carries the renderer's own NUL
// block markers. They used to expand into copies of the quote on every pass,
// so a few kilobytes of input rendered as megabytes.
func TestForgedBlockMarkers(t *testing.T) {
	in := "[quote]" + strings.Repeat("\x00Q0\x00", 400) + "[/quote]" + strings.Repeat("\x00C0\x00\x00Q0\x00", 400)
	got := string(renderBBCode(in))
	if len(got) > 10*len(in) {
		t.Errorf("a %d-byte body rendered as %d bytes", len(in), len(got))
	}
	if strings.Contains(got, "\x00") {
		t.Error("output still holds a NUL marker")
	}
}

// TestLinkNextToQuote covers a link written right before a quote. The link used
// to take in the quote's marker, which put the quote inside its href and doubled
// the quote at every level, so 371 bytes rendered as 23 MB.
func TestLinkNextToQuote(t *testing.T) {
	got := string(renderBBCode("http://ex.com[quote]x[/quote]"))
	if !strings.Contains(got, `<a href="http://ex.com" `) || strings.Count(got, "bb-quote-text") != 1 {
		t.Errorf("link next to a quote:\n%s", got)
	}
	in := strings.Repeat("http://x[quote]", 24) + "end" + strings.Repeat("[/quote]", 24)
	if got := renderBBCode(in); len(got) > 1000*len(in) {
		t.Errorf("%d bytes of nested quotes rendered as %d bytes", len(in), len(got))
	}
	in = "[url=http://ex.com[quote]q[/quote]]t[/url]"
	if got := string(renderBBCode(in)); strings.Contains(got, `href="http://ex.com<`) {
		t.Errorf("a quote ended up inside an href:\n%s", got)
	}
}

func TestCleanTextDropsControlCharacters(t *testing.T) {
	got, ok := cleanText("  a\x00b\x07c\r\nd\te  ", 100)
	if !ok || got != "abc\nd\te" {
		t.Errorf("cleanText = %q, %v", got, ok)
	}
	// Invisible characters that could hide instructions from people but not
	// from agents: tag characters spelling "hi", a bidi override, a zero-width
	// space and a byte-order mark. Emoji joiners stay.
	in := "ok\U000E0068\U000E0069\u202eyes\u200b\ufeff \U0001F469\u200d\U0001F4BB \u2764\ufe0f"
	if got, _ := cleanText(in, 100); got != "okyes \U0001F469\u200d\U0001F4BB \u2764\ufe0f" {
		t.Errorf("cleanText(%q) = %q", in, got)
	}
	if _, ok := cleanText("\x00\x01", 100); ok {
		t.Error("a body of only control characters should count as empty")
	}
}
