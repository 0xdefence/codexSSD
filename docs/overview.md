# Overview & Plain-English Description

> Mirrored from the CodexSSD Notion spec (earlier drafts named the project
> "CodexGuard"). Content is faithful to the spec; naming uses **CodexSSD**.

> A one-page, jargon-free explanation of what CodexSSD is, who it's for, and why
> it matters. Share this with anyone — technical or not.

## What is CodexSSD, in one sentence?

CodexSSD is a small, quiet watchdog that sits on your computer and notices when
AI coding tools (like Codex) are quietly eating up your disk space and memory —
so you don't have to.

## The problem it solves

AI coding tools are incredibly useful, but they behave a bit like very fast
junior assistants with full access to your computer. While they work, they leave
things behind: huge log files, caches, leftover junk, and stale files. Most of
the time, nobody notices.

Here's the catch: **this is a problem that doesn't hurt you today — it hurts you
tomorrow.** The junk piles up slowly, invisibly, across all the different
projects you work on. Then one day your laptop feels sluggish, your battery dies
faster, your disk is mysteriously full, and you have no idea why. By the time you
feel it, months of quiet build-up have already happened.

And the people who suffer most are the ones least equipped to spot it — busy
people jumping between projects, non-technical founders, designers, project
managers. A seasoned engineer clears a fat log folder in ten seconds. Everyone
else doesn't even know it exists.

## What CodexSSD actually does

CodexSSD's superpower is simple: **it notices on your behalf.**

- It quietly **watches** what AI coding tools are doing to your machine.
- It **warns** you, in plain language, when something starts eating too much
  disk or memory.
- It safely **clears** the AI tool's own leftover log files — moving them to a
  recoverable recycling bin, never deleting outright.
- It **flags** anything else that looks like clutter, and lets *you* decide what
  to do with it. It never touches your real work on its own.

## Who it's for

- People using AI coding tools across many projects over long periods
- Busy developers and teams who won't notice junk piling up
- Non-technical people who don't know where to look
- Anyone who cares about their laptop staying fast and healthy

## The core promise

CodexSSD is a tool you let near your machine *precisely because* you're worried
about another tool near your machine. That means trust is everything. So CodexSSD
is deliberately careful: it only ever takes safe, reversible actions on its own,
and for anything uncertain, it asks you first.

**It would rather leave a bit of junk behind than ever risk touching something
you needed.** That caution isn't a weakness — it's the whole point.
