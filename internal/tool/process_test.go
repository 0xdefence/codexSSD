package tool

import "testing"

// TestParseProcesses is ported verbatim from the former
// internal/codex.TestParseProcesses: parseProcesses moved here unchanged when
// process detection became per-profile, so its coverage moves with it.
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

func TestMatchesProfile(t *testing.T) {
	p := Profile{Name: "claude", ProcessNames: []string{"claude"}}
	cases := []struct {
		command string
		want    bool
	}{
		{"/usr/local/bin/claude --resume", true},
		{"node /Users/x/.nvm/bin/claude", true},       // node runner containing the tool name
		{"/usr/local/bin/codexssd watch", false},      // never match ourselves
		{"vim /Users/x/notes/claude-ideas.md", false}, // mentioning the name is not running it
		{"", false},
	}
	for _, c := range cases {
		got := matchesProfile(p, Process{Command: c.command})
		if got != c.want {
			t.Errorf("matchesProfile(%q) = %v, want %v", c.command, got, c.want)
		}
	}
}

func TestMatchesProfileHints(t *testing.T) {
	p := Codex()
	if !matchesProfile(p, Process{Command: "/opt/thing codex app-server --port 1"}) {
		t.Error("command hint 'codex app-server' should match the Codex profile")
	}
}

// TestMatchesProfileCodex ports the former internal/codex.TestMatchesCodex cases
// onto the generic matchesProfile against the Codex profile. matchesCodex was
// deleted from package codex when this logic moved here, so its coverage moves
// with it (union'd with TestMatchesProfile/TestMatchesProfileHints above, which
// exercise the "claude" profile and the multiword-hint case respectively).
func TestMatchesProfileCodex(t *testing.T) {
	p := Codex()
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
		got := matchesProfile(p, Process{Command: c.command})
		if got != c.want {
			t.Errorf("matchesProfile(Codex(), %q) = %v, want %v", c.command, got, c.want)
		}
	}
}
