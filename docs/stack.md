# Proposed Stack

> Mirrored from the CodexSSD Notion spec (earlier drafts named the project "CodexGuard").

> What we're building CodexSSD with, and why — explained without assuming a
> technical background.

## The headline choice: it's all built in Go

CodexSSD is written entirely in **Go** (a programming language built by Google).
We're using Go for everything — including the relationship-mapping piece later
on. Here's why that matters, in plain terms.

## Why Go fits this product perfectly

The whole promise of CodexSSD is that it's a **tiny, lightweight, trustworthy
thing** that sits on your machine and doesn't cause trouble. Go delivers exactly
that:

- **It ships as a single, small file you just run.** Nothing to install, nothing
  to set up, no clutter left on your machine. For a non-technical user, this is
  the difference between "it just works" and "I gave up before I started."
- **It's fast and uses very little memory.** Ideal for a tool that mostly sits
  quietly in the background.
- **It's good at watching processes and files** — the core of what CodexSSD does.
- **It runs on Mac, Windows, and Linux** from the same codebase.
- **It's easy to distribute** through the channels developers already trust.

## Clearing up a common myth

There's a widespread belief that you need Python "because it's the data
language." That reputation is real — but only for a *specific* kind of work:
heavy number-crunching, statistics, and machine learning.

The relationship-mapping in CodexSSD is **not** that kind of work. We're not
doing statistics — we're *tracing wires*: opening files and noticing "this one
points to that one." That's a structure problem, and Go handles it well.

**Bringing Python in would actually break our core promise.** Python usually
needs to be installed and set up separately on the user's machine, with the right
version and extra pieces — exactly the kind of friction and clutter CodexSSD
exists to remove. It would also split the tool into two languages that have to
talk to each other: more moving parts, more to break, more to maintain.

## The honest exception

If, *far* in the future, the mapping grows into something genuinely statistical —
clever pattern-detection or machine-learning-flavoured analysis — then Python's
ecosystem might start to earn its place, and we'd weigh it seriously at that
point. But for everything on the current roadmap, **Go is not just adequate, it's
the better fit.**

## Summary table

| Layer                         | Choice                              | Why                                              |
| ----------------------------- | ----------------------------------- | ------------------------------------------------ |
| Core language                 | Go                                  | Single small binary, fast, low-memory, trustworthy |
| Watching the system           | Go's built-in process/file tools    | No extra dependencies                            |
| Storing session history       | A simple plain-text file (JSONL)    | Lightweight; deliberately NOT a database         |
| Relationship-mapping (later)  | Go                                  | It's wire-tracing, not statistics                |
| Optional editor add-on (later)| Thin plugin, Go does the real work  | Keeps the heavy lifting in one place             |

## One deliberate non-choice: no database

CodexSSD deliberately does **not** use a heavyweight database to store its own
records. It uses a simple plain-text file instead. Why? Because the original
problem we're solving is partly *caused* by a tool writing to a database too
aggressively. It would look absurd — and be hypocritical — for the tool that
guards against that to do the same thing. CodexSSD must stay featherweight.
