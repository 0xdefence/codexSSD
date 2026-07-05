package tui

import (
	"strings"
	"testing"
)

func TestRenderLogoWideHasBlockArt(t *testing.T) {
	out := renderLogo(100)
	if !strings.Contains(out, "██████╗") {
		t.Errorf("wide logo should contain the block art, got:\n%s", out)
	}
	if !strings.Contains(out, "the disk watchdog") {
		t.Errorf("logo should contain the subtitle, got:\n%s", out)
	}
}

func TestRenderLogoNarrowFallsBackToCompact(t *testing.T) {
	out := renderLogo(30)
	if strings.Contains(out, "██████╗") {
		t.Errorf("narrow logo must NOT use the block art, got:\n%s", out)
	}
	if !strings.Contains(out, "codexSSD") {
		t.Errorf("narrow logo should show the compact wordmark, got:\n%s", out)
	}
	if !strings.Contains(out, "the disk watchdog") {
		t.Errorf("narrow logo should still show the subtitle, got:\n%s", out)
	}
}

func TestRenderCompactLogoAlwaysOneLineWordmark(t *testing.T) {
	out := renderCompactLogo(100) // wide, but compact form is forced
	if strings.Contains(out, "██████╗") {
		t.Errorf("compact logo must never use the block art, got:\n%s", out)
	}
	if !strings.Contains(out, "codexSSD") || !strings.Contains(out, "the disk watchdog") {
		t.Errorf("compact logo should show wordmark + subtitle, got:\n%s", out)
	}
}
