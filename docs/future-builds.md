# Future Builds

> Mirrored from the CodexSSD Notion spec (earlier drafts named the project "CodexGuard").

> Where CodexSSD goes once the safe core has proven itself. These are the
> exciting, more ambitious chapters — built on a foundation of trust we've
> already banked, not bet on day one.

## The shallow map, then the deep map

The first step into "understanding the project" is deliberately shallow: just
"does anything obvious point at this folder?" If yes, leave it alone with extra
caution. If no, still only flag it.

Later, this grows into a fuller map of how code files and modules connect — a
picture the tool uses to sharpen its judgment. Crucially, the map only ever makes
the tool *more careful*. Finding a connection means "hands off." Finding nothing
is never treated as permission to act.

## Behavioural detection (watching the mess get made)

Instead of guessing whether a folder is junk by its name, the tool *watches it
being created*. If it sees the AI agent run a build command and a new folder
appears right after, it *knows* that folder is build output — a far stronger
signal than any guess. This fits the product's identity perfectly: "I watched
what the agent did."

## Beyond Codex

The same problems exist with other AI coding tools. Once the Codex experience is
rock-solid, we broaden to Claude Code, Cursor, Gemini CLI, and others. The wedge
is Codex; the category is *all* local AI coding agents.

## Cost and token awareness

The same wasteful behaviours that fill your disk also burn money and "context." A
later module can spot when an agent is being expensive — and explain *why* a
session got pricey, which is often more useful than an exact number.

## Convenience layers

- **Daily / weekly summaries** — a gentle digest of what's piling up
- **An editor add-on** — for developers who live in their code editor
- **A menu-bar app** — a simple traffic-light (green / amber / red) for
  non-technical users who never touch a terminal
- **Team settings** — shared rules across a whole team

## The long-term vision

The Codex disk issue is the *wedge*. The bigger category is **local
observability and hygiene for AI agents** — the trusted control panel that
answers: what did the agent write, delete, use, and leave behind? As AI agents
get more autonomous, every developer and team will want that answer. CodexSSD can
become the place they get it.
