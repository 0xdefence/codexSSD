package agent

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	for _, name := range []string{"balanced", "strict", "repo-only", "disk-token-safe"} {
		if _, err := Parse(name); err != nil {
			t.Errorf("Parse(%q) errored: %v", name, err)
		}
	}
	if _, err := Parse("bogus"); err == nil {
		t.Error("Parse(\"bogus\") should error")
	}
}

func TestContentHasMarkerAndRules(t *testing.T) {
	c := Content(ProfileBalanced)
	if !strings.HasPrefix(c, marker) {
		t.Errorf("content should start with the generated marker:\n%s", c)
	}
	if !strings.Contains(c, "profile=balanced") {
		t.Errorf("content should record the profile name")
	}
	if !strings.Contains(c, "minimal") {
		t.Errorf("content should include the core 'minimal edits' rule")
	}

	cases := map[Profile]string{
		ProfileStrict:        "smallest change",
		ProfileRepoOnly:      ".gitignore",
		ProfileDiskTokenSafe: "ls -R",
	}
	for p, want := range cases {
		if !strings.Contains(Content(p), want) {
			t.Errorf("Content(%s) missing %q", p, want)
		}
	}
}
