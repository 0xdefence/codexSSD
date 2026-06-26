package codex

import "testing"

func TestParseProcesses(t *testing.T) {
	out := "" +
		"  101 /usr/bin/codex --serve\n" +
		"  202 /usr/local/bin/node /opt/codex/cli.js\n" +
		"303 /bin/zsh -l\n" +
		"\n" + // blank line should be skipped
		"notanumber /bin/bad\n" // non-numeric pid skipped

	got := parseProcesses(out)

	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (%+v)", len(got), got)
	}
	if got[0].PID != 101 || got[0].Name != "codex" {
		t.Errorf("got[0] = %+v, want pid 101 name codex", got[0])
	}
	if got[1].PID != 202 || got[1].Name != "node" {
		t.Errorf("got[1] = %+v, want pid 202 name node", got[1])
	}
	if got[2].PID != 303 || got[2].Name != "zsh" {
		t.Errorf("got[2] = %+v, want pid 303 name zsh", got[2])
	}
}

func TestMatchesCodex(t *testing.T) {
	cases := []struct {
		command string
		want    bool
	}{
		{"/usr/bin/codex --serve", true},            // exact base name codex
		{"codex", true},                             // bare
		{"/usr/bin/codex app-server", true},         // multiword hint
		{"/usr/bin/codex desktop", true},            // multiword hint
		{"node /opt/codex/cli.js", true},            // node wrapper running codex
		{"/bin/zsh -l", false},                      // unrelated
		{"/Users/me/go/bin/codexssd status", false}, // our own tool, excluded
		{"", false}, // empty
	}
	for _, c := range cases {
		got := matchesCodex(Process{Command: c.command})
		if got != c.want {
			t.Errorf("matchesCodex(%q) = %v, want %v", c.command, got, c.want)
		}
	}
}
