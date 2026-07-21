# Scope: In & Out

> Mirrored from the CodexSSD Notion spec (earlier drafts named the project "CodexGuard").

> A clear line in the sand for the first build: what we ARE doing, and what we
> are deliberately leaving for later. Keeping this honest is how we ship
> something finishable and safe.

## In scope for this build (Phase 1 + early Phase 2)

These are the things we're committing to build first. All of them are safe —
nothing here can hurt a user's machine or data.

- **Watching AI coding tools** (starting with Codex) while they run — **[shipped]**
  (`watch`; memory warnings currently cover Codex's own process RSS only)
- **Plain-language warnings** when disk or memory use gets alarming — **[shipped]**
  (`watch`, using configurable thresholds)
- **Safely clearing Codex's own log files** — moved into a recoverable recycling
  bin, never truly deleted — **[shipped]** (`clean`)
- **The recycling bin itself** — holds moved-aside items for ~2 weeks, then
  releases them if not missed; Codex data always clawable back — **[shipped]**
  (`restore`, `prune`)
- **A clear report** showing what's eating disk and memory, including stale old
  logs from forgotten projects — **[shipped]** (`report`)
- **"Please behave" rules** installed for the AI agent, to reduce mess at the
  source — **[shipped]** (`install-agent`)
- **Honest self-reporting** of CodexSSD's own footprint (disk, memory, processor)
  — **[shipped]** (`self`; covers disk and history, not a live processor metric)
- **Flagging** other clutter for the user to decide on — report only, never
  acting — **[shipped]** (`report`)

## Shipped since (2026-07)

Two items originally listed as out of scope have since shipped, in the
deliberately narrow form the roadmap describes:

- **Support for other AI tools** — Claude Code is now supported end to end
  (`status`, `report`, `clean`, `restore`, `prune` via `--tool claude`), gated
  by its own per-tool allow-list (fresh session files are never touched,
  because they power `claude --resume`). Cursor and Gemini CLI are still not
  supported.
- **Behavioural detection** — `watch` now records, best-effort, when a new
  entry appears in `~/.codex` while Codex is running (Codex only for now), and
  `report` annotates matching entries. This is observation only; it never acts
  on what it notices.

## Out of scope for this build (deliberately deferred)

These are real, valuable, and worth building — just *not yet*. Each one carries
more risk or cost than the safe core, so it waits until trust is earned.

- **Deleting anything on its own.** The tool only ever *moves aside*, and only
  a tool's own known files. No automatic permanent deletion, ever, in this
  build.
- **Cleaning up the user's actual project files automatically.** It can flag
  them, but it will not act on them.
- **The deep relationship-map** between code files and modules. (A shallow,
  cautious version has shipped for Claude Code in Phase 3; the full version is
  Phase 4.)
- **Cursor, Gemini CLI, and other AI tools** beyond Codex and Claude Code.
- **Fancy interfaces** — editor plugins and a menu-bar app come later.
- **Cost and token tracking, daily summaries, team settings.**

## The principle behind the line

We set the dial at **"watch, warn, and tidy only the AI tool's own recoverable
mess."** Everything riskier is a *later* decision, not a *now* decision.

This isn't cutting corners — it's matching how much risk we take on to how mature
the tool is. We can always add the clever stuff later. We can never un-delete
someone's work. A tool that occasionally leaves junk behind is annoying; one that
deletes something needed is fatal to trust — and trust is the entire product.
