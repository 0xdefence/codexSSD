package agent

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

// Rules returns the AGENTS.md rule text for a profile.
//
// STUB: not implemented yet.
func Rules(p Profile) (string, error) {
	return "", errNotImplemented
}
