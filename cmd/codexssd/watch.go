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

	"github.com/0xdefence/codexssd/internal/codex"
	"github.com/0xdefence/codexssd/internal/monitor"
	"github.com/0xdefence/codexssd/internal/notify"
	"github.com/0xdefence/codexssd/internal/recorder"
)

// watchDeps injects every effect the watch loop has, so tests can script a
// whole session deterministically.
type watchDeps struct {
	scan    func() codex.LogReport
	memory  func() (int64, error)
	running func() (bool, error)
	notify  func(title, body string) error
	now     func() time.Time
	tick    <-chan time.Time
	stop    <-chan struct{}
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
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: codexssd watch [--interval 30s] [--no-notify] [--json]\n\n")
		fmt.Fprintf(os.Stderr, "Watch Codex's logs and memory in the foreground; Ctrl-C to stop.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	dir, err := codex.Dir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: could not determine your home directory: %v\n", err)
		return 1
	}
	cfg := loadConfig()
	if *interval == 0 {
		*interval = cfg.PollInterval()
	}

	notifier := notify.Notify
	if *noNotify || !cfg.Notifications {
		notifier = func(string, string) error { return nil }
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	stop := make(chan struct{})
	go func() { <-sigCh; close(stop) }()

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	deps := watchDeps{
		scan:    func() codex.LogReport { return codex.ScanLogs(dir) },
		memory:  func() (int64, error) { return codex.ProcessMemory() },
		running: codex.IsCodexRunning,
		notify:  notifier,
		now:     time.Now,
		tick:    ticker.C,
		stop:    stop,
	}
	rec := runWatch(os.Stdout, *jsonOut, cfg.MonitorThresholds(), deps)
	recordReceipt(rec)
	return 0
}

// watchEvent is the JSON line emitted per risk-level change with --json.
type watchEvent struct {
	At      time.Time `json:"at"`
	Level   string    `json:"level"`
	Reasons []string  `json:"reasons"`
	TotalMB int64     `json:"total_log_mb"`
}

// runWatch is the loop, fully injected for tests. It samples once immediately
// (the baseline), then once per tick, printing only when the risk LEVEL
// changes — never per tick, so a quiet session stays quiet.
func runWatch(w io.Writer, jsonOut bool, th monitor.Thresholds, deps watchDeps) recorder.Receipt {
	var samples []monitor.Sample
	var last monitor.Risk = -1 // sentinel: baseline always prints
	peak := monitor.RiskLow
	var peakRate float64
	start := deps.now()
	var firstTotal, lastTotal int64

	observe := func() {
		report := deps.scan()
		mem, _ := deps.memory() // best-effort; 0 when unknown
		running, _ := deps.running()
		now := deps.now()
		if len(samples) == 0 {
			firstTotal = report.TotalBytes
		}
		lastTotal = report.TotalBytes
		var wal int64
		for _, f := range report.Files {
			if f.Name == "logs_2.sqlite-wal" && f.Exists {
				wal = f.Size
			}
		}
		samples = monitor.AppendSample(samples, monitor.Sample{
			At: now, TotalBytes: report.TotalBytes, WALBytes: wal, MemBytes: mem,
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
			line, _ := json.Marshal(watchEvent{At: now, Level: a.Level.String(), Reasons: reasons, TotalMB: report.TotalBytes / (1024 * 1024)})
			fmt.Fprintln(w, string(line))
		} else {
			msg := fmt.Sprintf("[%s] risk %s", now.Format("15:04:05"), a.Level)
			for _, r := range a.Reasons {
				msg += " — " + r
			}
			fmt.Fprintln(w, msg)
		}
		if escalatedToAlarming {
			body := "Codex disk/memory activity looks alarming."
			if len(a.Reasons) > 0 {
				body = a.Reasons[0]
			}
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
			return recorder.Receipt{
				At: end, Action: "watch",
				DurationSec:  end.Sub(start).Seconds(),
				DiskWritten:  growth,
				PeakMBPerMin: peakRate,
				Risk:         peak.String(),
			}
		}
	}
}
