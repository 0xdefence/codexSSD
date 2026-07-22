package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/0xdefence/codexssd/internal/behavior"
	"github.com/0xdefence/codexssd/internal/config"
	"github.com/0xdefence/codexssd/internal/monitor"
	"github.com/0xdefence/codexssd/internal/recorder"
)

// scriptedDeps feeds a fixed sequence of readings, then closes stop.
// scan and now use SEPARATE counters: runWatch calls now() for the session
// start/end too, so sharing one index would misalign (and overrun) totals.
func scriptedDeps(t *testing.T, totals []int64) (watchDeps, *[]string) {
	t.Helper()
	tick := make(chan time.Time)
	stop := make(chan struct{})
	scanIdx, timeIdx := 0, 0
	var notified []string
	base := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	deps := watchDeps{
		scan: func() (int64, int64) {
			total := totals[scanIdx]
			if scanIdx < len(totals)-1 {
				scanIdx++
			}
			return total, 0
		},
		memory:  func() (int64, error) { return 0, nil },
		running: func() (bool, error) { return true, nil },
		notify: func(title, body string) error {
			notified = append(notified, title+": "+body)
			return nil
		},
		// Each now() call advances one minute, so consecutive observations sit
		// exactly one minute apart and MB-per-minute math is trivial to reason about.
		now: func() time.Time {
			ts := base.Add(time.Duration(timeIdx) * time.Minute)
			timeIdx++
			return ts
		},
		tick: tick,
		stop: stop,
	}
	go func() {
		for range totals[1:] {
			tick <- time.Time{}
		}
		close(stop)
	}()
	return deps, &notified
}

func TestRunWatchPrintsOnLevelChangeOnly(t *testing.T) {
	// 0 → +200MB/min for two ticks: LOW baseline, then HIGH, then stays HIGH.
	deps, notified := scriptedDeps(t, []int64{0, 200 << 20, 400 << 20})
	var buf bytes.Buffer
	rec := runWatch(&buf, false, "Codex", "codex", monitor.DefaultThresholds(), deps)

	out := buf.String()
	// Count the event-line form "risk HIGH" — the session summary says
	// "Peak risk: HIGH", which deliberately doesn't match this substring.
	if strings.Count(out, "risk HIGH") != 1 {
		t.Errorf("'risk HIGH' should be printed exactly once (level change), got:\n%s", out)
	}
	if len(*notified) != 1 {
		t.Errorf("want exactly 1 notification on escalation to HIGH, got %v", *notified)
	}
	if rec.Action != "watch" || rec.Risk != "HIGH" {
		t.Errorf("receipt = %+v", rec)
	}
}

func TestRunWatchNoNotifyBelowHigh(t *testing.T) {
	deps, notified := scriptedDeps(t, []int64{0, 30 << 20}) // ~30MB/min → MEDIUM
	var buf bytes.Buffer
	runWatch(&buf, false, "Codex", "codex", monitor.DefaultThresholds(), deps)
	if len(*notified) != 0 {
		t.Errorf("MEDIUM must not notify, got %v", *notified)
	}
}

// TestWatchInterval pins the --interval safety clamp: non-positive values fall
// back to the configured default, and anything below the documented 5s floor
// is raised to 5s — a watchdog must never itself hammer the machine by
// busy-looping process-table scans, nor panic on a negative ticker duration.
func TestWatchInterval(t *testing.T) {
	cfg := config.Default()
	cases := []struct {
		name string
		flag time.Duration
		want time.Duration
	}{
		{"negative falls back to config", -5 * time.Second, cfg.PollInterval()},
		{"zero falls back to config", 0, cfg.PollInterval()},
		{"1ns clamps to 5s floor", time.Nanosecond, 5 * time.Second},
		{"4s clamps to 5s floor", 4 * time.Second, 5 * time.Second},
		{"5s stays at 5s", 5 * time.Second, 5 * time.Second},
		{"45s passes through unchanged", 45 * time.Second, 45 * time.Second},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := watchInterval(c.flag, cfg)
			if got != c.want {
				t.Errorf("watchInterval(%v) = %v, want %v", c.flag, got, c.want)
			}
		})
	}
}

// scriptedDepsBaselineWAL sets up a single-observation session (no ticks, stop
// closed already) whose one scan reports a WAL file at walBytes — used to pin
// the deliberate behavior that even a baseline (first-ever) sample that is
// already HIGH/CRITICAL fires exactly one notification, since escalation is
// judged against the -1 sentinel "no risk observed yet", not a prior tick.
func scriptedDepsBaselineWAL(t *testing.T, totalBytes, walBytes int64) (watchDeps, *[]string) {
	t.Helper()
	stop := make(chan struct{})
	close(stop) // baseline only: no ticks needed
	var notified []string
	base := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	deps := watchDeps{
		scan: func() (int64, int64) {
			return totalBytes, walBytes
		},
		memory:  func() (int64, error) { return 0, nil },
		running: func() (bool, error) { return true, nil },
		notify: func(title, body string) error {
			notified = append(notified, title+": "+body)
			return nil
		},
		now:  func() time.Time { return base },
		tick: make(chan time.Time), // never fires
		stop: stop,
	}
	return deps, &notified
}

// TestRunWatchPrintsBehaviorEvents pins the watch-side wiring for Phase 4
// behavioral tracking: when observeBehavior reports a newly-noticed entry,
// runWatch prints one plain "noticed:" line for it — independent of whether
// the risk level changed, since watchDeps.observeBehavior is now injectable
// just like every other loop effect.
func TestRunWatchPrintsBehaviorEvents(t *testing.T) {
	deps, _ := scriptedDeps(t, []int64{0, 10 << 20})
	deps.observeBehavior = func(agentRunning bool, now time.Time) []behavior.Event {
		if !agentRunning {
			t.Fatalf("observeBehavior called with agentRunning=false; scriptedDeps.running always returns true")
		}
		return []behavior.Event{{Time: now, Tool: "codex", Entry: "cache-v2"}}
	}
	var buf bytes.Buffer
	runWatch(&buf, false, "Codex", "codex", monitor.DefaultThresholds(), deps)

	out := buf.String()
	want := `noticed: "cache-v2" appeared in ~/.codex while Codex was running`
	if !strings.Contains(out, want) {
		t.Errorf("missing behavior notice line, want to contain %q, got:\n%s", want, out)
	}
}

// TestRunWatchNilObserveBehaviorIsSafe guards the "best-effort, never
// disturbs the loop" promise for the case ProvenancePath failed at startup:
// deps.observeBehavior is left nil, and the loop must run exactly as before.
func TestRunWatchNilObserveBehaviorIsSafe(t *testing.T) {
	deps, _ := scriptedDeps(t, []int64{0, 200 << 20, 400 << 20})
	var buf bytes.Buffer
	rec := runWatch(&buf, false, "Codex", "codex", monitor.DefaultThresholds(), deps)
	if rec.Action != "watch" {
		t.Errorf("nil observeBehavior should not disturb the loop; receipt = %+v", rec)
	}
}

func TestRunWatchBaselineHighWALNotifiesOnce(t *testing.T) {
	th := monitor.DefaultThresholds()
	// WAL at exactly the HIGH threshold on the very first (baseline) sample.
	deps, notified := scriptedDepsBaselineWAL(t, 0, th.HighWALSizeMB*1024*1024)
	var buf bytes.Buffer
	runWatch(&buf, false, "Codex", "codex", th, deps)

	out := buf.String()
	if strings.Count(out, "risk HIGH") != 1 {
		t.Errorf("want exactly one 'risk HIGH' line for a baseline-HIGH sample, got:\n%s", out)
	}
	if len(*notified) != 1 {
		t.Errorf("want exactly one notification for a baseline-HIGH sample, got %v", *notified)
	}
}

// TestRunWatchClaudeToolLabels pins the tool-aware surfaces: the JSON "tool"
// field, the display name in notification bodies, and the receipt Action.
func TestRunWatchClaudeToolLabels(t *testing.T) {
	base := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	tick := make(chan time.Time, 1)
	stop := make(chan struct{})
	var notified []string
	sizes := []int64{0, 600 * 1024 * 1024} // 600 MiB in one 30s tick → alarming rate
	step := 0
	now := base
	// sampled synchronizes on the baseline observe() round finishing its
	// reads of step/now: runWatch's baseline sample runs in its own
	// goroutine with no other happens-before edge to this goroutine, so
	// without this signal the mutations below race ahead of that read.
	// runWatch calls deps.now() once for its own "start" timestamp before
	// the first observe() round, so the first call is skipped — only the
	// now() call inside observe() (which runs after that round's scan())
	// signals readiness.
	sampled := make(chan struct{}, 1)
	nowCalls := 0
	deps := watchDeps{
		scan:    func() (int64, int64) { s := sizes[step]; return s, 0 },
		memory:  func() (int64, error) { return 0, nil },
		running: func() (bool, error) { return true, nil },
		notify:  func(title, body string) error { notified = append(notified, body); return nil },
		now: func() time.Time {
			n := now
			nowCalls++
			if nowCalls > 1 {
				select {
				case sampled <- struct{}{}:
				default:
				}
			}
			return n
		},
		tick: tick,
		stop: stop,
	}
	var out strings.Builder
	done := make(chan recorder.Receipt, 1)
	go func() { done <- runWatch(&out, true, "Claude Code", "claude", testThresholds(), deps) }()
	<-sampled // wait for the baseline observe() round to finish reading step/now
	// advance one tick with a huge write burst
	step = 1
	now = base.Add(30 * time.Second)
	tick <- now
	// let the observe goroutine drain, then stop
	time.Sleep(50 * time.Millisecond)
	close(stop)
	rec := <-done

	if !strings.Contains(out.String(), `"tool":"claude"`) {
		t.Fatalf("json output missing tool field:\n%s", out.String())
	}
	if len(notified) == 0 || !strings.Contains(notified[0], "Claude Code") {
		t.Fatalf("notification body should name Claude Code, got %q", notified)
	}
	if rec.Action != "watch --tool claude" {
		t.Fatalf("receipt Action = %q, want %q", rec.Action, "watch --tool claude")
	}
}

func testThresholds() monitor.Thresholds {
	return monitor.Thresholds{MediumMBPerMin: 25, HighMBPerMin: 100, CriticalMBPerMin: 500,
		HighWALSizeMB: 1024, CriticalWALSizeMB: 8192, HighMemMB: 2048, CriticalMemMB: 6144}
}
