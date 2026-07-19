package shallowmap

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDecodePath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"-Users-jo-code-myapp", filepath.FromSlash("/Users/jo/code/myapp")},
		{"no-leading-dash", ""}, // not the encoding we know; refuse to guess
		{"", ""},
	}
	for _, c := range cases {
		if got := DecodePath(c.in); got != c.want {
			t.Errorf("DecodePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestProbeConnectedWhenProjectExists(t *testing.T) {
	statOK := func(string) (os.FileInfo, error) { return nil, nil }
	r := ProbeClaudeProject("-Users-jo-code-myapp", statOK)
	if r.Connection != Connected || r.Evidence == "" {
		t.Errorf("probe = %+v, want Connected with plain-language evidence", r)
	}
}

func TestProbeUnknownNeverClaimsSafe(t *testing.T) {
	statGone := func(string) (os.FileInfo, error) { return nil, errors.New("not found") }
	r := ProbeClaudeProject("-Users-jo-code-gone", statGone)
	if r.Connection != Unknown {
		t.Errorf("probe of missing project = %+v, want Unknown (never a 'safe' verdict)", r)
	}
	if r.Evidence != "" {
		t.Errorf("Unknown must carry no evidence text (nothing found is not a finding), got %q", r.Evidence)
	}
}

func TestScanClaudeProjects(t *testing.T) {
	claudeDir := t.TempDir()
	proj := filepath.Join(claudeDir, "projects", "-Users-jo-code-myapp")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "s1.jsonl"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries := ScanClaudeProjects(claudeDir, time.Now(), 30*24*time.Hour)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	e := entries[0]
	if e.Name != "-Users-jo-code-myapp" || e.DecodedPath == "" {
		t.Errorf("entry = %+v, want named slug with a decoded path", e)
	}
	// The decoded path does not exist in this sandbox → Unknown, never Connected.
	if e.Connection != Unknown {
		t.Errorf("Connection = %q, want unknown for a nonexistent decoded path", e.Connection)
	}
}
