package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/0xdefence/codexssd/internal/monitor"
)

func TestRiskColorPerLevel(t *testing.T) {
	// Each level maps to its own distinct color; adjacent levels must differ.
	seen := map[lipgloss.TerminalColor]bool{}
	levels := []monitor.Risk{monitor.RiskLow, monitor.RiskMedium, monitor.RiskHigh, monitor.RiskCritical}
	for _, lv := range levels {
		c := riskColor(lv)
		if c == nil {
			t.Fatalf("riskColor(%v) is nil", lv)
		}
		if seen[c] {
			t.Errorf("riskColor(%v) duplicates an earlier level's color", lv)
		}
		seen[c] = true
	}
}

func TestRiskStyleUsesRiskColor(t *testing.T) {
	if got := riskStyle(monitor.RiskCritical).GetForeground(); got != riskColor(monitor.RiskCritical) {
		t.Errorf("riskStyle foreground = %v, want riskColor(Critical) %v", got, riskColor(monitor.RiskCritical))
	}
	if !riskStyle(monitor.RiskHigh).GetBold() {
		t.Error("riskStyle should be bold")
	}
}

func TestRiskGlyph(t *testing.T) {
	if riskGlyph(monitor.RiskLow) != "●" {
		t.Errorf("riskGlyph = %q, want ●", riskGlyph(monitor.RiskLow))
	}
}
