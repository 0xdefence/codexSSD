package mcpserver

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/0xdefence/codexssd/internal/cleaner"
	"github.com/0xdefence/codexssd/internal/codex"
	"github.com/0xdefence/codexssd/internal/config"
	"github.com/0xdefence/codexssd/internal/recorder"
	"github.com/0xdefence/codexssd/internal/self"
	"github.com/0xdefence/codexssd/internal/visibility"
)

// Server holds the tool data sources as fields so tests can stub them.
// Every source is READ-ONLY by construction.
type Server struct {
	status     func() (codex.LogReport, error)
	cleanPlan  func() (any, error)
	backups    func() (any, error)
	selfReport func() (any, error)
	diskReport func() (visibility.Report, error)
}

// New returns a Server wired to the real read-only engine functions.
func New() *Server {
	return &Server{
		status: func() (codex.LogReport, error) {
			dir, err := codex.Dir()
			if err != nil {
				return codex.LogReport{}, err
			}
			return codex.ScanLogs(dir), nil
		},
		cleanPlan: func() (any, error) {
			dir, err := codex.Dir()
			if err != nil {
				return nil, err
			}
			plan, err := cleaner.PlanCodexLogs(dir)
			if err != nil {
				return nil, err
			}
			running, runErr := codex.IsCodexRunning()
			out := map[string]any{
				"plan":          plan,
				"codex_running": running,
				"note":          "dry run only — this server cannot move files; cleaning is human-only",
			}
			if runErr != nil && runErr != codex.ErrUnsupportedPlatform {
				out["check_error"] = runErr.Error()
			}
			return out, nil
		},
		backups: func() (any, error) {
			dir, err := codex.Dir()
			if err != nil {
				return nil, err
			}
			backups, err := cleaner.ListBackups(dir)
			if err != nil {
				return nil, err
			}
			if backups == nil {
				backups = []cleaner.Backup{} // [] not null
			}
			return backups, nil
		},
		selfReport: func() (any, error) {
			dir, err := recorder.Dir()
			if err != nil {
				return nil, err
			}
			rep, err := self.Measure(dir)
			if err != nil {
				return nil, err
			}
			return rep, nil
		},
		diskReport: func() (visibility.Report, error) {
			dir, err := codex.Dir()
			if err != nil {
				return visibility.Report{}, err
			}
			cfg, _ := config.LoadDefault() // malformed config → defaults; never fails
			return visibility.Scan(dir, time.Now(), cfg.StaleAfter()), nil
		},
	}
}

// toolDescriptors lists the five read-only tools. Every inputSchema is an
// empty object: no tool takes arguments, which keeps the surface unabusable.
func toolDescriptors() []map[string]any {
	emptySchema := map[string]any{"type": "object", "properties": map[string]any{}}
	mk := func(name, desc string) map[string]any {
		return map[string]any{"name": name, "description": desc, "inputSchema": emptySchema}
	}
	return []map[string]any{
		mk("codex_status", "Sizes of Codex's own log files under ~/.codex (read-only)."),
		mk("clean_plan", "Dry-run plan of what `codexssd clean` WOULD move aside. This server cannot execute it."),
		mk("list_backups", "Recoverable recycling-bin backups with hold information (read-only)."),
		mk("self_report", "CodexSSD's own footprint and action history (read-only)."),
		mk("disk_report", "What's using disk inside ~/.codex, with stale flags (read-only)."),
	}
}

// callTool runs one named tool and returns its result as pretty JSON text.
func (s *Server) callTool(name string) (string, error) {
	var v any
	var err error
	switch name {
	case "codex_status":
		v, err = s.status()
	case "clean_plan":
		v, err = s.cleanPlan()
	case "list_backups":
		v, err = s.backups()
	case "self_report":
		v, err = s.selfReport()
	case "disk_report":
		v, err = s.diskReport()
	default:
		return "", fmt.Errorf("unknown tool %q (this server is read-only; there are exactly five tools)", name)
	}
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
