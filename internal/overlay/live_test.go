package overlay

import (
	"strings"
	"testing"
)

func TestLiveTailKeepsShortText(t *testing.T) {
	c, tt := liveTail("hello there", "general")
	if c != "hello there" || tt != "general" {
		t.Fatalf("got %q %q", c, tt)
	}
}

func TestLiveTailTrimsAtWordBoundary(t *testing.T) {
	committed := strings.Repeat("word ", 200) + "last"
	c, tt := liveTail(committed, "maybe this")
	if !strings.HasPrefix(c, "… word") {
		t.Fatalf("expected an ellipsis and a whole first word, got %q", c[:20])
	}
	if !strings.HasSuffix(c, "last") || tt != "maybe this" {
		t.Fatalf("newest text lost: %q / %q", c[len(c)-10:], tt)
	}
	if n := len([]rune(c)) + len([]rune(tt)); n > liveMaxRunes+2 {
		t.Fatalf("kept %d runes, want about %d", n, liveMaxRunes)
	}
}

func TestLiveTailTentativeAlwaysWhole(t *testing.T) {
	long := strings.Repeat("x", liveMaxRunes+50)
	c, tt := liveTail("committed words", long)
	if tt != long {
		t.Fatal("tentative text was trimmed")
	}
	if c != "… " && !strings.HasPrefix(c, "…") {
		t.Fatalf("committed should be dropped to the ellipsis, got %q", c)
	}
}
