package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/0xdefence/codexssd/internal/behavior"
	"github.com/0xdefence/codexssd/internal/codex"
	"github.com/0xdefence/codexssd/internal/config"
	"github.com/0xdefence/codexssd/internal/monitor"
	"github.com/0xdefence/codexssd/internal/notify"
	"github.com/0xdefence/codexssd/internal/recorder"
	"github.com/0xdefence/codexssd/internal/tool"
)

// minWatchInterval is the floor below which --interval is clamped, mirroring
// config.Config.PollInterval's own documented 5s floor.
const minWatchInterval = 5 * time.Second

// watchInterval resolves the effective poll interval from the --interval flag
// and config, enforcing a safe floor.
//
// WHY: this is a watchdog meant for non-technical users — it must never
// itself hammer the machine it's supposed to be protecting. A non-positive
// value would reach time.NewTicker and panic; a tiny positive value (e.g.
// 1ns) would busy-loop process-table scans instead. So non-positive values
// fall back to the configured default, and anything below the 5s floor is
// clamped up to it with one friendly note.
func watchInterval(flagVal time.Duration, cfg config.Config) time.Duration {
	if flagVal <= 0 {
		return cfg.PollInterval()
	}
	if flagVal < minWatchInterval {
		fmt.Fprintf(os.Stderr, "codexssd: note: minimum watch interval is %s — using %s.\n",
			minWatchInterval, minWatchInterval)
		return minWatchInterval
	}
	return flagVal
}

// watchDeps injects every effect the watch loop has, so tests can script a
// whole session deterministically.
type watchDeps struct {
	// scan returns the tool's current disk footprint: total bytes plus the
	// WAL size for tools that have one (0 otherwise — the WAL risk checks
	// then simply never fire, no special-casing needed).
	scan    func() (totalBytes, walBytes int64)
	memory  func() (int64, error)
	running func() (bool, error)
	notify  func(title, body string) error
	now     func() time.Time
	tick    <-chan time.Time
	stop    <-chan struct{}

	// observeBehavior is best-effort behavioral tracking: given the current
	// agent-running state and timestamp, it returns any newly-noticed entries.
	// Optional — nil disables tracking entirely (e.g. ProvenancePath failed at
	// startup), and must never itself block, slow, or fail the watch loop.
	observeBehavior func(agentRunning bool, now time.Time) []behavior.Event
}

// cmdWatch implements `codexssd watch`: a foreground, read-only monitor.
//
// SAFETY: 100% read-only on Codex's files. Its only writes are one session
// receipt to CodexSSD's own state dir on exit, and (optional) desktop
// notifications — both best-effort.
func cmdWatch(args []string) int {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	interval := fs.Duration("interval", 0, "poll interval (default: from config, 30s)")
	noNotify := fs.Bool("no-notify", false, "disable desktop notifications")
	jsonOut := fs.Bool("json", false, "emit one JSON line per risk-level change")
	toolName := fs.String("tool", "codex", "which AI tool to watch (codex, claude)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: codexssd watch [--interval 30s] [--no-notify] [--json] [--tool codex|claude]\n\n")
		fmt.Fprintf(os.Stderr, "Watch a tool's disk and memory in the foreground; Ctrl-C to stop.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	p, dir, code := resolveTool(*toolName)
	if code != 0 {
		return code
	}
	cfg := loadConfig()
	*interval = watchInterval(*interval, cfg)

	notifier := notify.Notify
	if *noNotify || !cfg.Notifications {
		notifier = func(string, string) error { return nil }
	}

	// Behavioral tracking: notice new top-level entries appearing in dir while
	// watching runs, so `report` can later say "this appeared during a
	// watched session" — a far stronger signal than guessing by name alone.
	// Best-effort throughout: a failure to resolve the provenance path just
	// disables tracking for this session (one warning), never the watch loop.
	// Codex-only this round — Claude Code doesn't yet have a provenance model.
	var trackBehavior func(agentRunning bool, now time.Time) []behavior.Event
	if p.Name == "codex" {
		if provPath, err := behavior.ProvenancePath(); err != nil {
			fmt.Fprintf(os.Stderr, "codexssd: note: couldn't determine provenance path: %v — behavioral tracking disabled for this session.\n", err)
		} else {
			tracker := behavior.NewTracker("codex", provPath, readDirNames(dir))
			trackBehavior = func(agentRunning bool, now time.Time) []behavior.Event {
				return tracker.Observe(readDirNames(dir), agentRunning, now)
			}
		}
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	stop := make(chan struct{})
	go func() { <-sigCh; close(stop) }()

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	deps := watchDeps{
		notify:          notifier,
		now:             time.Now,
		tick:            ticker.C,
		stop:            stop,
		observeBehavior: trackBehavior,
	}
	if p.Name == "codex" {
		// Founding path, byte-for-byte: fixed-file scan with WAL extraction.
		deps.scan = func() (int64, int64) {
			r := codex.ScanLogs(dir)
			var wal int64
			for _, f := range r.Files {
				if f.Name == "logs_2.sqlite-wal" && f.Exists {
					wal = f.Size
				}
			}
			return r.TotalBytes, wal
		}
		deps.memory = codex.ProcessMemory
		deps.running = codex.IsCodexRunning
	} else {
		// Glob-profile tools: whole-dir footprint (no WAL → 0, so WAL risk
		// checks never fire), generic process matching and memory.
		deps.scan = func() (int64, int64) { return tool.ScanDirSize(dir), 0 }
		deps.memory = func() (int64, error) { return tool.ProcessMemory(p) }
		deps.running = func() (bool, error) { return tool.IsRunning(p) }
	}
	rec := runWatch(os.Stdout, *jsonOut, p.DisplayName, p.Name, cfg.MonitorThresholds(), deps)
	recordReceipt(rec)
	return 0
}

// readDirNames lists the top-level entry names in dir for behavioral
// tracking. A read error (e.g. the directory briefly missing) yields no
// names rather than an error — provenance is best-effort and must never
// disturb the watch loop.
func readDirNames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// watchEvent is the JSON line emitted per risk-level change with --json.
type watchEvent struct {
	At      time.Time `json:"at"`
	Tool    string    `json:"tool"`
	Level   string    `json:"level"`
	Reasons []string  `json:"reasons"`
	TotalMB int64     `json:"total_log_mb"`
}

// runWatch is the loop, fully injected for tests. It samples once immediately
// (the baseline), then once per tick, printing only when the risk LEVEL
// changes — never per tick, so a quiet session stays quiet.
func runWatch(w io.Writer, jsonOut bool, label, toolName string, th monitor.Thresholds, deps watchDeps) recorder.Receipt {
	var samples []monitor.Sample
	var last monitor.Risk = -1 // sentinel: baseline always prints
	peak := monitor.RiskLow
	var peakRate float64
	start := deps.now()
	var firstTotal, lastTotal int64

	observe := func() {
		total, wal := deps.scan()
		mem, _ := deps.memory() // best-effort; 0 when unknown
		running, _ := deps.running()
		now := deps.now()
		// Behavioral tracking runs every poll (not gated by the risk-level
		// print-suppression below): a quiet session should stay quiet on risk
		// noise, but a newly-noticed entry is its own distinct, low-frequency
		// signal worth surfacing immediately.
		if deps.observeBehavior != nil {
			for _, ev := range deps.observeBehavior(running, now) {
				fmt.Fprintf(w, "noticed: %q appeared in ~/.codex while Codex was running\n", ev.Entry)
			}
		}
		if len(samples) == 0 {
			firstTotal = total
		}
		lastTotal = total
		samples = monitor.AppendSample(samples, monitor.Sample{
			At: now, TotalBytes: total, WALBytes: wal, MemBytes: mem,
		}, 20)
		a := monitor.Evaluate(samples, running, th)
		if a.RateMBPerMin > peakRate {
			peakRate = a.RateMBPerMin
		}
		if a.Level > peak {
			peak = a.Level
		}
		if a.Level == last {
			return
		}
		escalatedToAlarming := a.Level >= monitor.RiskHigh && a.Level > last
		last = a.Level
		if jsonOut {
			reasons := a.Reasons
			if reasons == nil {
				reasons = []string{} // [] not null
			}
			line, _ := json.Marshal(watchEvent{At: now, Tool: toolName, Level: a.Level.String(), Reasons: reasons, TotalMB: total / (1024 * 1024)})
			fmt.Fprintln(w, string(line))
		} else {
			msg := fmt.Sprintf("[%s] risk %s", now.Format("15:04:05"), a.Level)
			for _, r := range a.Reasons {
				msg += " — " + r
			}
			fmt.Fprintln(w, msg)
		}
		if escalatedToAlarming {
			body := label + " disk/memory activity looks alarming."
			_ = deps.notify("CodexSSD: "+a.Level.String(), body) // fire-and-forget
		}
	}

	observe() // baseline
	for {
		select {
		case <-deps.tick:
			observe()
		case <-deps.stop:
			end := deps.now()
			growth := lastTotal - firstTotal
			if growth < 0 {
				growth = 0
			}
			if !jsonOut {
				fmt.Fprintf(w, "\nWatched for %s. Peak risk: %s. Log growth observed: %s.\n",
					end.Sub(start).Round(time.Second), peak, codex.HumanBytes(growth))
			}
			action := "watch"
			if toolName != "codex" {
				action += " --tool " + toolName
			}
			return recorder.Receipt{
				At: end, Action: action,
				DurationSec:  end.Sub(start).Seconds(),
				DiskWritten:  growth,
				PeakMBPerMin: peakRate,
				Risk:         peak.String(),
			}
		}
	}
}
