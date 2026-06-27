# Monitor / Warn Engine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Turn the size samples the dashboard already collects into a real **write-rate + WAL-growth** reading and a LOW/MED/HIGH/CRITICAL **risk** level, surfaced in the dashboard — upgrading the warning from "logs are big" to "Codex is hammering your disk."

**Architecture:** `internal/monitor` becomes a small pure engine: a `Sample` (point-in-time log sizes) + a ring helper, and an `Evaluate` function that computes MB/min from sample deltas, applies WAL-size thresholds and an idle-writer rule, and returns an `Assessment{Level, RateMBPerMin, WALBytes, Reasons}`. The TUI feeds a `Sample` on every 30s load, keeps a small in-RAM window, and renders the assessment. No new dependencies; monitor stays standard-library only and has **no** I/O (pure math over samples the TUI provides).

**Tech Stack:** Go 1.25, stdlib only. (charmbracelet stays only in `internal/tui`.)

## Global Constraints

- **Measurement = WAL-growth proxy** (locked): rate is computed from total-log-byte deltas over time; WAL size is read from the sample. No per-process counters, no cgo.
- **monitor is pure + stdlib-only:** `Evaluate`/`AppendSample`/`Sample` do no I/O and import nothing outside the stdlib. The TUI builds samples from its existing read-only load.
- **Low-write:** samples live in memory only (capped ring); nothing is written to disk.
- **Default thresholds** (already in `risk.go`, keep): medium 25, high 100, critical 500 MB/min; WAL high 1024 MiB, critical 8192 MiB.
- **No new dependencies.** Naming: CodexSSD / `codexssd`.
- **Verification gate:** `go build ./... && go vet ./... && go test ./...` green and `gofmt -l .` empty before each commit. TUI program never started from tests.

## File Structure

- `internal/monitor/samples.go` — **create.** Package doc + `Sample` + `AppendSample` ring.
- `internal/monitor/risk.go` — **modify.** Keep `Risk`/`String`/`Thresholds`/`DefaultThresholds`; add `Assessment`; replace stub `Evaluate` with the real one.
- `internal/monitor/sampler.go`, `internal/monitor/watch.go` — **delete.** Superseded stubs (the TUI is the sampler; a daemon `Watcher` is a Phase-4 concern). Their package doc moves to `samples.go`.
- `internal/monitor/*_test.go` — **create.**
- `internal/tui/model.go`, `commands.go`, `update.go`, `view.go` — **modify.** Feed samples, compute + render the assessment.
- `internal/tui/update_test.go` — **modify.**

Existing engine API (unchanged): `codex.HumanBytes`. Current monitor scaffold: `Risk` (RiskLow/Medium/High/Critical + `String()`), `Thresholds{MediumMBPerMin, HighMBPerMin, CriticalMBPerMin float64; HighWALSizeMB, CriticalWALSizeMB int64}`, `DefaultThresholds()`.

---

## Task 1: Sample + ring buffer (pure)

**Files:**
- Create: `internal/monitor/samples.go`, `internal/monitor/samples_test.go`
- Delete: `internal/monitor/sampler.go`, `internal/monitor/watch.go`

**Interfaces:**
- Produces: `type Sample struct { At time.Time; TotalBytes int64; WALBytes int64 }`, `func AppendSample(history []Sample, s Sample, max int) []Sample`.

- [ ] **Step 1: Write the failing test**

Create `internal/monitor/samples_test.go`:

```go
package monitor

import (
	"testing"
	"time"
)

func TestAppendSampleCapsLength(t *testing.T) {
	var h []Sample
	base := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		h = AppendSample(h, Sample{At: base.Add(time.Duration(i) * time.Minute), TotalBytes: int64(i)}, 3)
	}
	if len(h) != 3 {
		t.Fatalf("len = %d, want 3 (capped)", len(h))
	}
	// Oldest entries evicted; newest kept in order.
	if h[0].TotalBytes != 2 || h[2].TotalBytes != 4 {
		t.Errorf("window = %d..%d, want 2..4", h[0].TotalBytes, h[2].TotalBytes)
	}
}

func TestAppendSampleBelowCap(t *testing.T) {
	var h []Sample
	h = AppendSample(h, Sample{TotalBytes: 1}, 10)
	h = AppendSample(h, Sample{TotalBytes: 2}, 10)
	if len(h) != 2 {
		t.Fatalf("len = %d, want 2", len(h))
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/monitor/ -run TestAppendSample -v`
Expected: FAIL — `AppendSample`/`Sample` mismatch (old `Sample` has different fields; build error).

- [ ] **Step 3: Delete the superseded stubs and create samples.go**

Delete the two stub files:

```bash
git rm internal/monitor/sampler.go internal/monitor/watch.go
```

Create `internal/monitor/samples.go`:

```go
// Package monitor turns Codex's log-growth over time into a plain-language risk
// level (LOW/MEDIUM/HIGH/CRITICAL). It is a pure engine: it does no I/O and
// keeps no state of its own — callers (the interactive app) supply samples and
// render the result. Samples live in memory only; nothing is written to disk.
package monitor

import "time"

// Sample is a point-in-time reading of Codex's log sizes.
type Sample struct {
	At         time.Time
	TotalBytes int64 // total size of Codex's known log files
	WALBytes   int64 // size of logs_2.sqlite-wal
}

// AppendSample adds s to history, keeping at most max most-recent samples.
func AppendSample(history []Sample, s Sample, max int) []Sample {
	history = append(history, s)
	if max > 0 && len(history) > max {
		history = history[len(history)-max:]
	}
	return history
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/monitor/ -v`
Expected: PASS. (`risk.go`'s `Evaluate` still references `Sample` — it compiles because `Sample` still exists; its body returns `RiskLow` for now, fixed in Task 2.)

Note: if `go build ./...` fails because `risk.go`'s `Evaluate(samples []Sample, t Thresholds) Risk` body references removed identifiers, it should not — it only uses `Sample` and `RiskLow`, both still present. If the deleted files held the only `errNotImplemented`, confirm nothing else referenced it (only the deleted `sampler.go`/`watch.go` did).

- [ ] **Step 5: Verify build/vet/format**

Run: `go build ./... && go vet ./... && gofmt -l . && go test ./...`
Expected: no `gofmt` output; all green.

- [ ] **Step 6: Commit**

```bash
git add internal/monitor
git commit -m "feat(monitor): Sample + capped ring; drop superseded sampler/watch stubs"
```

---

## Task 2: Assessment + Evaluate (the risk math)

**Files:**
- Modify: `internal/monitor/risk.go`
- Test: `internal/monitor/risk_test.go` (create)

**Interfaces:**
- Consumes: `Sample` (Task 1), `Risk`, `Thresholds`, `DefaultThresholds` (existing).
- Produces:
  - `type Assessment struct { Level Risk; RateMBPerMin float64; WALBytes int64; Reasons []string }`
  - `func Evaluate(samples []Sample, codexRunning bool, t Thresholds) Assessment` (replaces the old `Evaluate(samples, t) Risk`).

- [ ] **Step 1: Write the failing test**

Create `internal/monitor/risk_test.go`:

```go
package monitor

import (
	"strings"
	"testing"
	"time"
)

func mib(n int64) int64 { return n * 1024 * 1024 }

// two samples one minute apart with the given total-byte delta
func window(deltaBytes int64, wal int64) []Sample {
	base := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	return []Sample{
		{At: base, TotalBytes: 0, WALBytes: wal},
		{At: base.Add(time.Minute), TotalBytes: deltaBytes, WALBytes: wal},
	}
}

func TestEvaluateRateLevels(t *testing.T) {
	th := DefaultThresholds()
	cases := []struct {
		name      string
		mbPerMin  int64
		want      Risk
	}{
		{"calm", 5, RiskLow},
		{"medium", 30, RiskMedium},
		{"high", 150, RiskHigh},
		{"critical", 600, RiskCritical},
	}
	for _, c := range cases {
		a := Evaluate(window(mib(c.mbPerMin), 0), true, th)
		if a.Level != c.want {
			t.Errorf("%s: level = %v, want %v (rate %.0f)", c.name, a.Level, c.want, a.RateMBPerMin)
		}
	}
}

func TestEvaluateWALSizeEscalates(t *testing.T) {
	th := DefaultThresholds()
	// Low rate, but a huge WAL should still escalate to CRITICAL.
	a := Evaluate(window(mib(1), mib(9000)), true, th)
	if a.Level != RiskCritical {
		t.Errorf("level = %v, want RiskCritical for 9000 MiB WAL", a.Level)
	}
	if a.WALBytes != mib(9000) {
		t.Errorf("WALBytes = %d, want %d", a.WALBytes, mib(9000))
	}
}

func TestEvaluateIdleWriterEscalates(t *testing.T) {
	th := DefaultThresholds()
	// Medium rate while Codex is NOT running is more alarming → at least HIGH.
	a := Evaluate(window(mib(30), 0), false, th)
	if a.Level < RiskHigh {
		t.Errorf("level = %v, want >= RiskHigh (idle writer)", a.Level)
	}
	found := false
	for _, r := range a.Reasons {
		if strings.Contains(r, "idle") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an idle-writer reason, got %v", a.Reasons)
	}
}

func TestEvaluateEmptyAndSingle(t *testing.T) {
	th := DefaultThresholds()
	if a := Evaluate(nil, true, th); a.Level != RiskLow || a.RateMBPerMin != 0 {
		t.Errorf("empty: %+v, want calm/0", a)
	}
	one := []Sample{{TotalBytes: mib(500), WALBytes: 0}}
	if a := Evaluate(one, true, th); a.RateMBPerMin != 0 {
		t.Errorf("single sample should have 0 rate, got %.1f", a.RateMBPerMin)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/monitor/ -run TestEvaluate -v`
Expected: FAIL — `Assessment` undefined and `Evaluate` signature mismatch (build error).

- [ ] **Step 3: Replace Evaluate and add Assessment**

In `internal/monitor/risk.go`, add `import "fmt"` and replace the stub `Evaluate` (the `func Evaluate(samples []Sample, t Thresholds) Risk { return RiskLow }`) with:

```go
// Assessment is the monitor's read on current Codex log activity.
type Assessment struct {
	Level        Risk
	RateMBPerMin float64
	WALBytes     int64
	Reasons      []string
}

// Evaluate computes a risk Assessment from a window of samples. Rate is the
// total-log growth between the oldest and newest sample, in MB/min. WAL size and
// an idle-writer rule (growth while Codex is not running) can escalate the level.
// Pure: no I/O, no clock — everything comes from the samples.
func Evaluate(samples []Sample, codexRunning bool, t Thresholds) Assessment {
	a := Assessment{Level: RiskLow}
	if len(samples) == 0 {
		return a
	}
	newest := samples[len(samples)-1]
	a.WALBytes = newest.WALBytes

	if len(samples) >= 2 {
		oldest := samples[0]
		mins := newest.At.Sub(oldest.At).Minutes()
		if mins > 0 {
			delta := newest.TotalBytes - oldest.TotalBytes
			if delta < 0 {
				delta = 0
			}
			a.RateMBPerMin = float64(delta) / (1024 * 1024) / mins
		}
	}

	// Write-rate thresholds.
	switch {
	case a.RateMBPerMin >= t.CriticalMBPerMin:
		a.Level = RiskCritical
		a.Reasons = append(a.Reasons, fmt.Sprintf("writing %.0f MB/min", a.RateMBPerMin))
	case a.RateMBPerMin >= t.HighMBPerMin:
		a.Level = RiskHigh
		a.Reasons = append(a.Reasons, fmt.Sprintf("writing %.0f MB/min", a.RateMBPerMin))
	case a.RateMBPerMin >= t.MediumMBPerMin:
		a.Level = RiskMedium
		a.Reasons = append(a.Reasons, fmt.Sprintf("writing %.0f MB/min", a.RateMBPerMin))
	}

	// WAL size can escalate.
	walMB := newest.WALBytes / (1024 * 1024)
	if walMB >= t.CriticalWALSizeMB {
		a.Level = maxRisk(a.Level, RiskCritical)
		a.Reasons = append(a.Reasons, fmt.Sprintf("WAL file is %d MiB", walMB))
	} else if walMB >= t.HighWALSizeMB {
		a.Level = maxRisk(a.Level, RiskHigh)
		a.Reasons = append(a.Reasons, fmt.Sprintf("WAL file is %d MiB", walMB))
	}

	// An idle writer (logs growing while Codex isn't running) is extra alarming.
	if !codexRunning && a.RateMBPerMin >= t.MediumMBPerMin {
		a.Level = maxRisk(a.Level, RiskHigh)
		a.Reasons = append(a.Reasons, "growing while Codex is idle")
	}

	return a
}

func maxRisk(a, b Risk) Risk {
	if a > b {
		return a
	}
	return b
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/monitor/ -v`
Expected: PASS (rate levels, WAL escalation, idle-writer, empty/single).

- [ ] **Step 5: Verify build/vet/format**

Run: `go build ./... && go vet ./... && gofmt -l . && go test ./...`
Expected: no `gofmt` output; all green. (The TUI doesn't call `Evaluate` yet, so the signature change doesn't break it.)

- [ ] **Step 6: Commit**

```bash
git add internal/monitor
git commit -m "feat(monitor): Evaluate write-rate + WAL + idle-writer into a risk Assessment"
```

---

## Task 3: Wire the monitor into the dashboard

**Files:**
- Modify: `internal/tui/model.go` (fields + `maxSamples`), `internal/tui/commands.go` (`loadedMsg.at` + `walBytes` helper, set `at` in `loadCmd`), `internal/tui/update.go` (append sample + evaluate on `loadedMsg`), `internal/tui/view.go` (risk line + risk-aware banner)
- Test: `internal/tui/update_test.go`

**Interfaces:**
- Consumes: `monitor.Sample`, `monitor.AppendSample`, `monitor.Assessment`, `monitor.Evaluate`, `monitor.DefaultThresholds`, `monitor.RiskMedium/High/Critical`, `codex.HumanBytes`.
- Produces: `Model.samples []monitor.Sample`, `Model.assessment monitor.Assessment`, `const maxSamples`, `loadedMsg.at time.Time`, `walBytes(codex.LogReport) int64`, updated `bannerState`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/update_test.go` (add `"time"` and the monitor import if not present):

```go
func TestHighRiskDrivesActionableBanner(t *testing.T) {
	base := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	m := New()
	// First sample: small, idle.
	first := sampleLoaded()
	first.report.TotalBytes = 10 * 1024 * 1024
	first.plan.TotalBytes = 10 * 1024 * 1024
	first.at = base
	m, _ = step(m, first)
	// Second sample one minute later: +600 MiB → CRITICAL rate, even though size < deadweight.
	second := sampleLoaded()
	second.report.TotalBytes = 610 * 1024 * 1024
	second.plan.TotalBytes = 610 * 1024 * 1024
	second.at = base.Add(time.Minute)
	m, _ = step(m, second)

	if m.assessment.Level != monitor.RiskCritical {
		t.Fatalf("assessment level = %v, want RiskCritical", m.assessment.Level)
	}
	view := m.View()
	if !strings.Contains(view, "Risk:") {
		t.Errorf("dashboard should show a Risk line:\n%s", view)
	}
	if m.bannerState() != bannerActionable {
		t.Errorf("high risk + idle should be actionable, got %v", m.bannerState())
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestHighRisk -v`
Expected: FAIL — `loadedMsg.at`, `m.assessment`, `monitor` undefined (build error).

- [ ] **Step 3: Model fields + constant**

In `internal/tui/model.go`: add the monitor import (`"github.com/0xdefence/codexssd/internal/monitor"`), add fields to `Model`, and a constant:

```go
	// monitor (write-activity risk)
	samples    []monitor.Sample
	assessment monitor.Assessment
```

```go
// maxSamples bounds the in-memory sample window the monitor evaluates.
const maxSamples = 20
```

- [ ] **Step 4: loadedMsg timestamp + walBytes helper**

In `internal/tui/commands.go`: add an `at time.Time` field to the `loadedMsg` struct; set `at: time.Now()` in the `loadCmd` return; add the helper:

```go
// walBytes returns the size of the -wal file from a scan report (0 if absent).
func walBytes(r codex.LogReport) int64 {
	for _, f := range r.Files {
		if f.Name == "logs_2.sqlite-wal" && f.Exists {
			return f.Size
		}
	}
	return 0
}
```

- [ ] **Step 5: Append sample + evaluate in the loadedMsg handler**

In `internal/tui/update.go`, extend the `loadedMsg` case (after the existing field assignments, before `return m, nil`) with the monitor import added:

```go
		s := monitor.Sample{At: msg.at, TotalBytes: msg.report.TotalBytes, WALBytes: walBytes(msg.report)}
		m.samples = monitor.AppendSample(m.samples, s, maxSamples)
		m.assessment = monitor.Evaluate(m.samples, m.running, monitor.DefaultThresholds())
```

- [ ] **Step 6: Risk line + risk-aware banner in view.go**

In `internal/tui/view.go`:

Update `bannerState` so elevated risk also counts as a concern:

```go
func (m Model) bannerState() banner {
	concern := m.deadweight() || m.assessment.Level >= monitor.RiskMedium
	if !concern {
		return bannerCalm
	}
	if m.supported && !m.running {
		return bannerActionable
	}
	return bannerInformational
}
```

(Add the `monitor` import to `view.go`.)

In `renderDashboard`, just before the deadweight/banner block, add a risk line when the level is at least MEDIUM:

```go
	if m.assessment.Level >= monitor.RiskMedium {
		reason := ""
		if len(m.assessment.Reasons) > 0 {
			reason = " — " + m.assessment.Reasons[0]
		}
		fmt.Fprintf(&b, "Risk: %s · %.0f MB/min · WAL %s%s\n",
			m.assessment.Level, m.assessment.RateMBPerMin, codex.HumanBytes(m.assessment.WALBytes), reason)
	}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/tui/ -v`
Expected: PASS (new high-risk test + all prior dashboard/banner tests).

- [ ] **Step 8: Verify build/vet/format and exercise**

Run: `go build ./... && go vet ./... && gofmt -l . && go test ./...`
Expected: no `gofmt` output; all green.

- [ ] **Step 9: Commit**

```bash
git add internal/tui
git commit -m "feat(tui): surface monitor risk (write-rate/WAL) in the dashboard banner"
```

---

## Self-Review notes

- **Coverage:** Sample + ring (T1); rate/WAL/idle-writer → Assessment with the locked thresholds (T2); dashboard integration — sample feed, evaluate, risk line, risk-aware banner (T3). Monitor stays pure/stdlib (no I/O); samples in RAM only (capped). Superseded `sampler.go`/`watch.go` removed.
- **Type consistency:** `Sample{At,TotalBytes,WALBytes}`, `Assessment{Level,RateMBPerMin,WALBytes,Reasons}`, `Evaluate(samples, codexRunning, t)`, `AppendSample(history, s, max)`, `loadedMsg.at`, `maxSamples` used identically across tasks.
- **Mutation-test target (for review):** the threshold comparisons in `Evaluate` and the idle-writer escalation are the safety-relevant logic — verify a test fails if a threshold or the `!codexRunning` guard is broken.
- **Out of scope:** disk-free "disk filling" CRITICAL (future), per-process counters (Phase 4), a background daemon `Watcher` (Phase 4).
