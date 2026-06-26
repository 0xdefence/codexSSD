# Scope: In & Out

> Mirrored from the CodexSSD Notion spec (earlier drafts named the project "CodexGuard").

> A clear line in the sand for the first build: what we ARE doing, and what we
> are deliberately leaving for later. Keeping this honest is how we ship
> something finishable and safe.

## In scope for this build (Phase 1 + early Phase 2)

These are the things we're committing to build first. All of them are safe —
nothing here can hurt a user's machine or data.

- **Watching AI coding tools** (starting with Codex) while they run
- **Plain-language warnings** when disk or memory use gets alarming
- **Safely clearing Codex's own log files** — moved into a recoverable recycling
  bin, never truly deleted
- **The recycling bin itself** — holds moved-aside items for ~2 weeks, then
  releases them if not missed; Codex data always clawable back
- **A clear report** showing what's eating disk and memory, including stale old
  logs from forgotten projects
- **"Please behave" rules** installed for the AI agent, to reduce mess at the
  source
- **Honest self-reporting** of CodexSSD's own footprint (disk, memory, processor)
- **Flagging** other clutter for the user to decide on — report only, never
  acting

## Out of scope for this build (deliberately deferred)

These are real, valuable, and worth building — just *not yet*. Each one carries
more risk or cost than the safe core, so it waits until trust is earned.

- **Deleting anything on its own.** The tool only ever *moves aside*, and only
  Codex's own logs. No automatic permanent deletion, ever, in this build.
- **Cleaning up the user's actual project files automatically.** It can flag
  them, but it will not act on them.
- **The deep relationship-map** between code files and modules. (A shallow,
  cautious version may come in Phase 3; the full version is Phase 4.)
- **Behavioural detection** (noticing a folder was created by a build command).
- **Support for other AI tools** (Claude Code, Cursor, Gemini CLI) — Codex first.
- **Fancy interfaces** — editor plugins and a menu-bar app come later.
- **Cost and token tracking, daily summaries, team settings.**

## The principle behind the line

We set the dial at **"watch, warn, and tidy only the AI tool's own recoverable
mess."** Everything riskier is a *later* decision, not a *now* decision.

This isn't cutting corners — it's matching how much risk we take on to how mature
the tool is. We can always add the clever stuff later. We can never un-delete
someone's work. A tool that occasionally leaves junk behind is annoying; one that
deletes something needed is fatal to trust — and trust is the entire product.
