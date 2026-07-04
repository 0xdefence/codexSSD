package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/0xdefence/codexssd/internal/codex"
	"github.com/0xdefence/codexssd/internal/monitor"
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
		scan: func() codex.LogReport {
			r := codex.LogReport{DirExists: true, TotalBytes: totals[scanIdx]}
			if scanIdx < len(totals)-1 {
				scanIdx++
			}
			return r
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
	rec := runWatch(&buf, false, monitor.DefaultThresholds(), deps)

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
	runWatch(&buf, false, monitor.DefaultThresholds(), deps)
	if len(*notified) != 0 {
		t.Errorf("MEDIUM must not notify, got %v", *notified)
	}
}
