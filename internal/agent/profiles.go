package agent

import (
	"fmt"
	"strings"
)

// Profile selects how strict the installed AGENTS.md rules are.
type Profile string

const (
	// ProfileBalanced is the good default: discourages unnecessary churn.
	ProfileBalanced Profile = "balanced"
	// ProfileStrict suits fragile repos, low disk space, or long sessions.
	ProfileStrict Profile = "strict"
	// ProfileRepoOnly focuses on source-control cleanliness.
	ProfileRepoOnly Profile = "repo-only"
	// ProfileDiskTokenSafe guards both SSD writes and token budget.
	ProfileDiskTokenSafe Profile = "disk-token-safe"
)

// Profiles is the set of valid profiles (for help text and validation).
var Profiles = [...]Profile{ProfileBalanced, ProfileStrict, ProfileRepoOnly, ProfileDiskTokenSafe}

// marker is the hidden first-line tag CodexSSD writes into every generated
// AGENTS.md, so it can tell its own file from a hand-written one.
const marker = "<!-- codexssd:generated"

// Parse validates a profile name.
func Parse(name string) (Profile, error) {
	for _, p := range Profiles {
		if string(p) == name {
			return p, nil
		}
	}
	return "", fmt.Errorf("unknown profile %q (choose balanced, strict, repo-only, disk-token-safe)", name)
}

// coreRules apply to every profile.
var coreRules = []string{
	"Make minimal, targeted edits — don't rewrite whole files.",
	"Don't modify lockfiles unless the task is specifically about dependencies.",
	"Don't re-run the full test suite repeatedly — prefer targeted tests.",
	"Don't create coverage/, dist/, build/, or cache directories casually.",
	"Don't paste large command output (full test logs, full git diff, ls -R) back into context.",
	"Don't create persistent local databases or caches.",
}

// profileExtra adds profile-specific emphasis on top of the core rules.
var profileExtra = map[Profile][]string{
	ProfileBalanced: nil,
	ProfileStrict: {
		"Make the smallest change that works; never rewrite a file wholesale.",
		"Ask before running the full test suite or other long-running commands.",
		"Don't start background processes; clean up any generated artifacts.",
	},
	ProfileRepoOnly: {
		"Never touch lockfiles or commit generated/build artifacts.",
		"Respect .gitignore; don't leave stray tracked files behind.",
		"Keep diffs small and reviewable.",
	},
	ProfileDiskTokenSafe: {
		"Avoid ls -R, repeated full git diff, and verbose build/test/docker logs in context.",
		"Keep command output short; summarize instead of pasting.",
		"Never create persistent local DBs or caches.",
	},
}

// Content renders the full AGENTS.md for a profile, including the marker.
func Content(p Profile) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s profile=%s -->\n", marker, p)
	b.WriteString("# AGENTS.md\n\n")
	b.WriteString("House rules for AI coding agents in this repo, installed by CodexSSD\n")
	b.WriteString("to keep disk and token use sane. Safe to edit.\n\n")
	b.WriteString("## Rules\n\n")
	for _, r := range coreRules {
		fmt.Fprintf(&b, "- %s\n", r)
	}
	for _, r := range profileExtra[p] {
		fmt.Fprintf(&b, "- %s\n", r)
	}
	return b.String()
}
