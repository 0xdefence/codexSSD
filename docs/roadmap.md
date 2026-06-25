# Roadmap

> Mirrored from the CodexSSD Notion spec (earlier drafts named the project "CodexGuard").

> The plan, phase by phase, in plain language. Each phase is something we can
> actually finish and ship before moving to the next. We build trust first, then
> earn the right to do cleverer things.

## The guiding idea

We build in order of *safety earned*. The earliest versions can't embarrass us —
they only watch, warn, and tidy the AI tool's own recoverable mess. The
cleverer, riskier abilities come *later*, once the tool has proven itself in the
real world. We'd rather ship something small and trustworthy than something big
and fragile.

Think of it as a dial we're turning up slowly — from "smoke detector" to
"helpful housekeeper" — and never turning it faster than trust allows.

---

## Phase 1 — The Watchdog (the safe core)

**Goal:** A tool that watches, warns, and safely tidies the AI tool's own logs.
Nothing it does can hurt you.

What it does:

- Quietly watches AI coding tools (starting with Codex) while they run
- Warns you in plain language when disk or memory use gets alarming
- Safely clears Codex's *own* log files — into a recoverable recycling bin,
  never a true delete
- Installs a set of "please behave" rules for the AI agent to reduce mess in the
  first place
- Honestly reports its *own* footprint, so you can see it isn't the problem

Why this first: it's small enough to finish, safe enough that early users will
recommend it, and it delivers the core promise — noticing what busy people miss.

---

## Phase 2 — The Recycling Bin & Visibility (notice it for them)

**Goal:** Make the invisible visible, and give safe actions a giant undo button.

What it adds:

- A clear, plain-language report of what's eating disk and memory — "this 9GB of
  old Codex logs from a project you haven't touched since March"
- The recycling bin: flagged items the user approves get *moved aside*, held for
  ~2 weeks, then sent to the trash on their own if not missed
- Anything Codex-related can always be clawed back
- Spots stale, forgotten logs from old projects — the quiet build-up that busy
  people never notice

Why this matters: this is the version that's both safe *and* genuinely useful.
The recycling bin removes fear — nothing is ever really gone until two weeks of
not missing it has proven it was junk.

---

## Phase 3 — The Shallow Map (connection-awareness, carefully)

**Goal:** Make the tool's judgment sharper by being aware of what's connected to
what — but only to make it *more* cautious.

What it adds:

- A first, deliberately shallow version of relationship-mapping: "does anything
  obvious point at this folder?"
- If something *is* connected (used in deployment, Docker, depended on by other
  code) → leave it alone, extra caution
- If *nothing* is found → still only flag it, never act. (Finding no connection
  is **not** proof it's safe — it just means we didn't find one.)

The golden rule here: **finding a connection is trustworthy. Finding nothing is
not.** The map is a better flashlight, never a license to act.

---

## Phase 4 — The Deep Map & Broader Support (graduating into cleverness)

**Goal:** Once trust is banked and we have real-world evidence, deepen the map
and broaden beyond Codex.

What it adds:

- Fuller relationship-mapping between code files and modules
- Behavioural detection — noticing that a folder *was created by a build command*
  (a much stronger signal than guessing by name)
- Support for more AI coding tools (Claude Code, Cursor, Gemini CLI, etc.)
- Daily/weekly summaries, cost and token awareness, optional interfaces (editor
  plugin, menu-bar app)
- Team-level settings

Why last: these are the abilities we *graduate into*, built on a foundation of
trust we've already earned — not bet on with fingers crossed on day one.

---

## What we're deliberately NOT doing early

- Letting the tool delete things on its own (it only ever *moves aside*, and only
  for Codex's own logs)
- Letting the tool make confident judgments about your real work
- Acting on anything it's uncertain about — when in doubt, it reports, never
  resolves

That deliberate "laziness" is the product's integrity, not a limitation.
