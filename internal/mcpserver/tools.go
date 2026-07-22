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
	"github.com/0xdefence/codexssd/internal/tool"
	"github.com/0xdefence/codexssd/internal/visibility"
)

// Server holds the tool data sources as fields so tests can stub them.
// Every source is READ-ONLY by construction. Profile-taking sources receive
// the profile resolved from the request's optional {"tool": ...} argument.
type Server struct {
	status     func(p tool.Profile) (any, error)
	cleanPlan  func(p tool.Profile) (any, error)
	backups    func(p tool.Profile) (any, error)
	selfReport func() (any, error)
	diskReport func(p tool.Profile) (visibility.Report, error)
}

// New returns a Server wired to the real read-only engine functions.
func New() *Server {
	return &Server{
		status: func(p tool.Profile) (any, error) {
			dir, err := p.Dir()
			if err != nil {
				return nil, err
			}
			if p.Name == "codex" {
				// Founding path, unchanged shape.
				return codex.ScanLogs(dir), nil
			}
			// Glob-profile tools: what's cleanable (stale) right now — same
			// deliberate framing as `status --tool claude` on the CLI.
			cfg, _ := config.LoadDefault()
			cleanable := p.CleanablePaths(dir, time.Now(), cfg.StaleAfter())
			var total int64
			files := make([]map[string]any, 0, len(cleanable))
			for _, f := range cleanable {
				files = append(files, map[string]any{"name": f.Rel, "size_bytes": f.Size})
				total += f.Size
			}
			return map[string]any{
				"tool": p.Name, "dir": dir,
				"cleanable_stale_files": files, "cleanable_total_bytes": total,
				"note": "fresh session files are excluded on purpose — they may still be in use",
			}, nil
		},
		cleanPlan: func(p tool.Profile) (any, error) {
			dir, err := p.Dir()
			if err != nil {
				return nil, err
			}
			if p.Name == "codex" {
				// Unchanged codex path, including the codex_running key.
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
			}
			cfg, _ := config.LoadDefault()
			plan, err := cleaner.PlanTool(p, dir, time.Now(), cfg.StaleAfter())
			if err != nil {
				return nil, err
			}
			running, runErr := tool.IsRunning(p)
			out := map[string]any{
				"plan":         plan,
				"tool_running": running,
				"note":         "dry run only — this server cannot move files; cleaning is human-only",
			}
			if runErr != nil && runErr != tool.ErrUnsupportedPlatform {
				out["check_error"] = runErr.Error()
			}
			return out, nil
		},
		backups: func(p tool.Profile) (any, error) {
			dir, err := p.Dir()
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
		diskReport: func(p tool.Profile) (visibility.Report, error) {
			dir, err := p.Dir()
			if err != nil {
				return visibility.Report{}, err
			}
			cfg, _ := config.LoadDefault() // malformed config → defaults; never fails
			return visibility.Scan(dir, time.Now(), cfg.StaleAfter()), nil
		},
	}
}

// toolDescriptors lists the five read-only tools. Each accepts one OPTIONAL
// argument — {"tool": "codex"|"claude"} — defaulting to codex; nothing else,
// which keeps the surface unabusable. Names are historical (codex_status is
// named for the founding profile) and stable: renaming would break existing
// client configs.
func toolDescriptors() []map[string]any {
	toolSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tool": map[string]any{
				"type": "string", "enum": []string{"codex", "claude"},
				"description": "Which AI tool to inspect (default codex).",
			},
		},
	}
	mk := func(name, desc string) map[string]any {
		return map[string]any{"name": name, "description": desc, "inputSchema": toolSchema}
	}
	return []map[string]any{
		mk("codex_status", "Sizes of the selected tool's own files (read-only). Named for the founding profile; pass {\"tool\":\"claude\"} for Claude Code."),
		mk("clean_plan", "Dry-run plan of what `codexssd clean` WOULD move aside for the selected tool. This server cannot execute it."),
		mk("list_backups", "The selected tool's recoverable recycling-bin backups with hold information (read-only)."),
		mk("self_report", "CodexSSD's own footprint and action history (read-only; the tool argument is accepted and ignored)."),
		mk("disk_report", "What's using disk inside the selected tool's directory, with stale flags (read-only)."),
	}
}

// parseToolArg resolves the optional {"tool": ...} argument. Absent/empty
// means codex (the founding default); an unknown value is an explicit error,
// never a silent fallback.
func parseToolArg(raw json.RawMessage) (tool.Profile, error) {
	if len(raw) == 0 {
		return tool.Codex(), nil
	}
	var args struct {
		Tool string `json:"tool"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return tool.Profile{}, fmt.Errorf("invalid arguments: %v", err)
	}
	if args.Tool == "" {
		return tool.Codex(), nil
	}
	return tool.ByName(args.Tool)
}

// callTool runs one named tool and returns its result as pretty JSON text.
func (s *Server) callTool(name string, rawArgs json.RawMessage) (string, error) {
	p, err := parseToolArg(rawArgs)
	if err != nil {
		return "", err
	}
	var v any
	switch name {
	case "codex_status":
		v, err = s.status(p)
	case "clean_plan":
		v, err = s.cleanPlan(p)
	case "list_backups":
		v, err = s.backups(p)
	case "self_report":
		v, err = s.selfReport() // tool-agnostic; argument accepted and ignored
	case "disk_report":
		v, err = s.diskReport(p)
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
