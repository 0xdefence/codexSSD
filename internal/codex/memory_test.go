package codex

import "testing"

func TestParseRSSKiB(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int64
	}{
		{"empty", "", 0},
		{"single", "1024\n", 1024},
		{"multiple with padding", "  512\n 1536\n\n", 2048},
		{"garbage lines skipped", "abc\n100\n", 100},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ParseRSSKiB(c.in); got != c.want {
				t.Errorf("ParseRSSKiB(%q) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}
