// Command codexssd is a small, low-write local watchdog for AI coding agents
// (starting with OpenAI's Codex). It watches what an agent does to disk and
// memory, warns in plain language, safely tidies Codex's OWN log files into a
// recoverable recycling bin, and flags other clutter for the user to decide on.
//
// Phase 1 so far implements the read-only `status` command plus `clean` and
// `restore` for Codex's own log files.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/0xdefence/codexssd/internal/agent"
	"github.com/0xdefence/codexssd/internal/cleaner"
	"github.com/0xdefence/codexssd/internal/codex"
	"github.com/0xdefence/codexssd/internal/config"
	"github.com/0xdefence/codexssd/internal/mcpserver"
	"github.com/0xdefence/codexssd/internal/recorder"
	"github.com/0xdefence/codexssd/internal/self"
	"github.com/0xdefence/codexssd/internal/tool"
	"github.com/0xdefence/codexssd/internal/tui"
	"github.com/0xdefence/codexssd/internal/visibility"
)

const usage = `codexssd - a low-write local watchdog for AI coding agents

Usage:
  codexssd <command> [flags]

Commands:
  status         Show Codex's log files and their sizes (read-only)
  report         Show what's using disk inside ~/.codex (read-only)
  watch          Watch a running Codex agent and warn on risky activity
  clean          Move Codex's own logs aside into a recoverable recycling bin
  restore        Move previously cleaned logs back from the recycling bin
  prune          Release recycling-bin backups past their ~2-week hold to the Trash
  install-agent  Write a disk/token-safe AGENTS.md into a repo (--profile, --force, --print)
  self           Report CodexSSD's own footprint
  mcp            Serve read-only CodexSSD tools to AI agents over stdio (MCP)
  help           Show this help

Most commands accept --tool codex|claude (default codex).

Run "codexssd <command> -h" for command-specific flags.
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		if err := tui.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "codexssd: %v\n", err)
			return 1
		}
		return 0
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "status":
		return cmdStatus(rest)
	case "report":
		return cmdReport(rest)
	case "watch":
		return cmdWatch(rest)
	case "clean":
		return cmdClean(rest)
	case "restore":
		return cmdRestore(rest)
	case "prune":
		return cmdPrune(rest)
	case "install-agent":
		return cmdInstallAgent(rest)
	case "self":
		return cmdSelf(rest)
	case "mcp":
		if err := mcpserver.New().Serve(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "codexssd: mcp server error: %v\n", err)
			return 1
		}
		return 0
	case "help", "-h", "--help":
		fmt.Print(usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "codexssd: unknown command %q\n\n%s", cmd, usage)
		return 2
	}
}

// cmdStatus implements `codexssd status`.
//
// SAFETY: this command is 100% READ-ONLY. It only locates ~/.codex and calls
// os.Stat on Codex's known log files. It moves nothing, deletes nothing, and
// writes nothing to disk.
func cmdStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "output the report as JSON")
	toolName := fs.String("tool", "codex", "which AI tool to inspect (codex, claude)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: codexssd status [--json] [--tool codex|claude]\n\n")
		fmt.Fprintf(os.Stderr, "Report the size of a tool's own log/session files (read-only).\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	p, dir, code := resolveTool(*toolName)
	if code != 0 {
		return code
	}

	if p.Name == "codex" {
		// Unchanged Phase-1 path: fixed-file report via codex.ScanLogs(dir).
		report := codex.ScanLogs(dir)
		if *jsonOut {
			return emitJSON(report)
		}
		printStatus(report)
		return 0
	}

	// Glob-profile tools (Claude Code and beyond): status summarizes only what
	// is currently cleanable (stale own files). Fresh own files are left out
	// deliberately — they may still be in active use — so this never nudges
	// the user to look at something clean isn't about to touch anyway.
	cfg := loadConfig()
	cleanable := p.CleanablePaths(dir, time.Now(), cfg.StaleAfter())
	if *jsonOut {
		return emitJSON(newToolStatusReport(p, dir, cleanable))
	}
	printToolStatus(os.Stdout, p, dir, cleanable)
	return 0
}

// resolveTool maps a --tool value to its profile and data dir. Exit-code
// semantics match the rest of main: 2 = bad usage, 1 = environment problem.
func resolveTool(name string) (tool.Profile, string, int) {
	p, err := tool.ByName(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: %v\n", err)
		return tool.Profile{}, "", 2
	}
	dir, err := p.Dir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: could not determine your home directory: %v\n", err)
		return tool.Profile{}, "", 1
	}
	return p, dir, 0
}

// toolStatusFile is one cleanable (stale) file in a glob-profile tool's status
// JSON output.
type toolStatusFile struct {
	Name string `json:"name"`
	Size int64  `json:"size_bytes"`
}

// toolStatusReport is the --json shape for glob-profile tools' `status`:
// only currently-cleanable (stale) own files are listed — see cmdStatus.
type toolStatusReport struct {
	Tool        string           `json:"tool"`
	DisplayName string           `json:"display_name"`
	Dir         string           `json:"dir"`
	Cleanable   []toolStatusFile `json:"cleanable"`
	TotalBytes  int64            `json:"cleanable_bytes"`
}

func newToolStatusReport(p tool.Profile, dir string, cleanable []tool.FoundFile) toolStatusReport {
	r := toolStatusReport{
		Tool:        p.Name,
		DisplayName: p.DisplayName,
		Dir:         dir,
		Cleanable:   make([]toolStatusFile, 0, len(cleanable)),
	}
	for _, f := range cleanable {
		r.Cleanable = append(r.Cleanable, toolStatusFile{Name: f.Rel, Size: f.Size})
		r.TotalBytes += f.Size
	}
	return r
}

// printToolStatus renders a friendly, plain-language status for a glob-profile
// tool: what's currently cleanable, plus a note that fresh own files are
// deliberately left off this list because they may still be in use.
func printToolStatus(w io.Writer, p tool.Profile, dir string, cleanable []tool.FoundFile) {
	fmt.Fprintf(w, "%s directory: %s\n\n", p.DisplayName, dir)

	if len(cleanable) == 0 {
		fmt.Fprintf(w, "Nothing stale to report right now — no cleanable %s files were found.\n\n", p.DisplayName)
	} else {
		fmt.Fprintf(w, "%s files CodexSSD could safely clean up (stale, no longer fresh):\n", p.DisplayName)
		width := 20
		for _, f := range cleanable {
			if len(f.Rel) > width {
				width = len(f.Rel)
			}
		}
		var total int64
		for _, f := range cleanable {
			fmt.Fprintf(w, "  %-*s %10s\n", width, f.Rel, codex.HumanBytes(f.Size))
			total += f.Size
		}
		fmt.Fprintf(w, "  %-*s %10s\n\n", width, "Total", codex.HumanBytes(total))
	}

	fmt.Fprintf(w, "Fresh %s session files aren't listed here on purpose: they're still in\n", p.DisplayName)
	fmt.Fprintln(w, `use (for example, they power "claude --resume"). Run "codexssd clean --tool`)
	fmt.Fprintln(w, `claude" to review and move aside anything that's gone stale.`)
}

// loadConfig returns the user config, warning (not failing) on a malformed
// file — a broken config must never block a command.
func loadConfig() config.Config {
	cfg, err := config.LoadDefault()
	if err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: note: %v — using default settings.\n", err)
	}
	return cfg
}

// isCodexRunning is the process-check used by the file-mutating commands for
// the default (codex) --tool path. It is a package variable so tests can
// substitute a deterministic stub, making the safety gate (refuse while Codex
// is running) verifiable on any machine without a real Codex process or
// dependence on the host's process table.
var isCodexRunning = codex.IsCodexRunning

// isToolRunning is the general per-profile process-check seam, used for tools
// other than the default. It exists separately from isCodexRunning (rather
// than replacing it) so existing tests that stub isCodexRunning directly keep
// working unmodified.
var isToolRunning = tool.IsRunning

// toolRunning routes the running-check by tool: codex keeps using its
// existing seam (isCodexRunning); other tools go through isToolRunning.
func toolRunning(p tool.Profile) (bool, error) {
	if p.Name == "codex" {
		return isCodexRunning()
	}
	return isToolRunning(p)
}

// appendReceipt records a session receipt. A failed receipt is a note, never
// an error — bookkeeping must not fail the user's action.
var appendReceipt = recorder.Append

func recordReceipt(r recorder.Receipt) {
	if err := appendReceipt(r); err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: note: couldn't record session receipt: %v\n", err)
	}
}

// cmdClean implements `codexssd clean`.
//
// Default is a read-only dry run. `--yes` moves Codex's own logs aside into the
// recycling bin, but only after confirming Codex is not running. Nothing is ever
// deleted.
func cmdClean(args []string) int {
	fs := flag.NewFlagSet("clean", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "actually move the logs aside (default is a dry run)")
	jsonOut := fs.Bool("json", false, "output as JSON")
	toolName := fs.String("tool", "codex", "which AI tool to clean up (codex, claude)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: codexssd clean [--yes] [--json] [--tool codex|claude]\n\n")
		fmt.Fprintf(os.Stderr, "Move a tool's own log/session files aside into a recoverable recycling bin.\n")
		fmt.Fprintf(os.Stderr, "Without --yes this only shows what would happen (read-only).\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	p, dir, code := resolveTool(*toolName)
	if code != 0 {
		return code
	}

	// cfg is loaded up front (not just on --yes) because PlanTool needs
	// StaleAfter() to gate glob-listed files; for codex this changes nothing,
	// since its files are all fixed and ignore the stale gate either way.
	cfg := loadConfig()
	plan, err := cleaner.PlanTool(p, dir, time.Now(), cfg.StaleAfter())
	if err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: could not inspect %s's files: %v\n", p.DisplayName, err)
		return 1
	}

	running, runErr := toolRunning(p)
	supported := runErr != tool.ErrUnsupportedPlatform

	if !*yes {
		if *jsonOut {
			out := map[string]any{
				"plan":               plan,
				"codex_running":      running, // key kept for compatibility; means "is <tool> running"
				"platform_supported": supported,
			}
			if runErr != nil && supported {
				out["check_error"] = runErr.Error()
			}
			return emitJSON(out)
		}
		renderPlan(os.Stdout, plan, running, supported, p.DisplayName)
		return 0
	}

	// --yes: actually move aside. Refuse unless we can confirm the tool is stopped.
	if !supported {
		fmt.Fprintf(os.Stderr, "codexssd: cannot verify %s is closed on this platform; refusing to move files.\n", p.DisplayName)
		fmt.Fprintln(os.Stderr, "Run without --yes to see what would be moved.")
		return 1
	}
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "codexssd: could not check whether %s is running: %v\n", p.DisplayName, runErr)
		return 1
	}
	if running {
		fmt.Fprintf(os.Stderr, "codexssd: %s appears to be running. Close it first, then try again.\n", p.DisplayName)
		return 1
	}
	if plan.Empty() {
		if p.Name == "codex" {
			fmt.Println("Nothing to move aside — no Codex log files are present.")
		} else {
			fmt.Printf("Nothing stale to move aside — no cleanable %s files are present.\n", p.DisplayName)
		}
		return 0
	}

	dest, err := plan.ApplyWithHold(time.Now(), cfg.BinHold())
	if err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: clean failed: %v\n", err)
		return 1
	}
	if p.Name == "codex" {
		fmt.Printf("Moved %s of Codex logs aside to:\n  %s\n", codex.HumanBytes(plan.TotalBytes), dest)
	} else {
		fmt.Printf("Moved %s of %s's stale files aside to:\n  %s\n", codex.HumanBytes(plan.TotalBytes), p.DisplayName, dest)
	}
	fmt.Println("Nothing was deleted. Restore them any time with \"codexssd restore\".")
	recordReceipt(recorder.Receipt{At: time.Now(), Action: cleanAction(p), BytesMoved: plan.TotalBytes, FilesChanged: len(plan.Items), BackupID: filepath.Base(dest)})
	return 0
}

// cleanAction builds the receipt action string: "clean" for the default tool,
// "clean --tool <name>" otherwise. The Receipt schema itself doesn't change —
// only this action label grows a suffix — so `self`'s history report still
// reads every receipt CodexSSD has ever written.
func cleanAction(p tool.Profile) string {
	if p.Name == "codex" {
		return "clean"
	}
	return "clean --tool " + p.Name
}

// renderPlan prints a friendly, plain-language dry-run report. displayName is
// variadic so existing (pre-multi-tool) call sites — including main_test.go,
// which this task must leave unmodified — keep compiling unchanged; it
// defaults to "Codex", which reproduces today's exact wording byte-for-byte.
func renderPlan(w io.Writer, p cleaner.Plan, running bool, supported bool, displayName ...string) {
	name := "Codex"
	if len(displayName) > 0 && displayName[0] != "" {
		name = displayName[0]
	}
	isCodex := name == "Codex"

	if p.Empty() {
		if isCodex {
			fmt.Fprintf(w, "Nothing to move aside — no Codex log files are present in %s.\n", p.CodexDir)
		} else {
			fmt.Fprintf(w, "Nothing stale to move aside in %s — %s's fresh files are still in use and left alone.\n", p.CodexDir, name)
		}
		return
	}

	// Item-name column: widened to fit the longest name so nested paths (e.g.
	// Claude transcripts like "projects/-Users-jo-app/s1.jsonl") stay readable.
	// Codex's short fixed names never exceed the original 20, so its column
	// width — and therefore its output — is unchanged.
	width := 20
	for _, it := range p.Items {
		if len(it.Name) > width {
			width = len(it.Name)
		}
	}

	if isCodex {
		fmt.Fprintf(w, "CodexSSD found %s of Codex log files it can safely move aside:\n\n", codex.HumanBytes(p.TotalBytes))
	} else {
		fmt.Fprintf(w, "CodexSSD found %s of stale %s files it can safely move aside:\n\n", codex.HumanBytes(p.TotalBytes), name)
	}
	for _, it := range p.Items {
		fmt.Fprintf(w, "  %-*s %10s\n", width, it.Name, codex.HumanBytes(it.Size))
	}
	fmt.Fprintf(w, "  %-*s %10s\n\n", width, "Total", codex.HumanBytes(p.TotalBytes))

	if isCodex {
		fmt.Fprintln(w, "These are Codex's own logs. Moving them frees the space; Codex makes")
		fmt.Fprintln(w, "fresh ones next time it runs. Nothing is deleted — files go to a")
		fmt.Fprintln(w, "recoverable bin and can be restored.")
	} else {
		fmt.Fprintf(w, "These are %s's own stale session files — old enough that they're not\n", name)
		fmt.Fprintln(w, "expected to still be needed. Nothing is deleted — files go to a")
		fmt.Fprintln(w, "recoverable bin and can be restored.")
	}
	fmt.Fprintln(w)

	switch {
	case !supported:
		fmt.Fprintf(w, "Note: this platform can't check whether %s is running, so --yes is disabled here.\n", name)
	case running:
		fmt.Fprintf(w, "%s appears to be running. Close it before cleaning.\n", name)
	default:
		fmt.Fprintf(w, "%s doesn't appear to be running.\n", name)
		fmt.Fprintln(w, `Run "codexssd clean --yes" to move them aside.`)
	}
}

// emitJSON writes v to stdout as indented JSON. Returns a process exit code.
func emitJSON(v any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: failed to encode JSON: %v\n", err)
		return 1
	}
	return 0
}

// cmdRestore implements `codexssd restore`.
//
// With no argument it lists recoverable backups. With a backup id (the timestamp
// directory name) it moves that backup's files back to their original location.
func cmdRestore(args []string) int {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "output the backup list as JSON")
	toolName := fs.String("tool", "codex", "which AI tool's backups to restore (codex, claude)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: codexssd restore [--json] [--tool codex|claude] [backup-id]\n\n")
		fmt.Fprintf(os.Stderr, "With no id, lists recoverable backups. With an id, restores that backup.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	p, dir, code := resolveTool(*toolName)
	if code != 0 {
		return code
	}

	backups, err := cleaner.ListBackups(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: could not read backups: %v\n", err)
		return 1
	}

	// No id: list backups.
	if fs.NArg() == 0 {
		if *jsonOut {
			return emitJSON(backups)
		}
		renderBackups(os.Stdout, backups)
		return 0
	}

	// Restoring overwrites the live log location, so refuse while the tool runs.
	running, runErr := toolRunning(p)
	if runErr == tool.ErrUnsupportedPlatform {
		fmt.Fprintf(os.Stderr, "codexssd: cannot verify %s is closed on this platform; refusing to restore.\n", p.DisplayName)
		return 1
	}
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "codexssd: could not check whether %s is running: %v\n", p.DisplayName, runErr)
		return 1
	}
	if running {
		fmt.Fprintf(os.Stderr, "codexssd: %s appears to be running. Close it first, then try again.\n", p.DisplayName)
		return 1
	}

	id := fs.Arg(0)
	for _, b := range backups {
		if filepath.Base(b.Dir) == id {
			if err := cleaner.Restore(b.Dir); err != nil {
				fmt.Fprintf(os.Stderr, "codexssd: restore failed: %v\n", err)
				return 1
			}
			recordReceipt(recorder.Receipt{At: time.Now(), Action: restoreAction(p), BackupID: id})
			if *jsonOut {
				return emitJSON(map[string]any{
					"status":    "restored",
					"id":        id,
					"codex_dir": dir, // key kept for compatibility; holds the restored tool's dir
				})
			}
			fmt.Printf("Restored backup %s to %s.\n", id, dir)
			return 0
		}
	}
	fmt.Fprintf(os.Stderr, "codexssd: no backup with id %q. Run \"codexssd restore\" to list them.\n", id)
	return 1
}

// restoreAction mirrors cleanAction: "restore" for the default tool,
// "restore --tool <name>" otherwise. No Receipt schema change, just the label.
func restoreAction(p tool.Profile) string {
	if p.Name == "codex" {
		return "restore"
	}
	return "restore --tool " + p.Name
}

// renderBackups prints the recoverable backups in plain language.
func renderBackups(w io.Writer, backups []cleaner.Backup) {
	if len(backups) == 0 {
		fmt.Fprintln(w, "No backups to restore — nothing has been moved aside yet.")
		return
	}
	fmt.Fprintln(w, "Recoverable backups:")
	for _, b := range backups {
		var total int64
		for _, it := range b.Manifest.Items {
			total += it.Size
		}
		fmt.Fprintf(w, "  %-18s %10s   (%d files)\n", filepath.Base(b.Dir), codex.HumanBytes(total), len(b.Manifest.Items))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, `Restore one with "codexssd restore <id>".`)
}

// printStatus renders a friendly, human-readable status report.
func printStatus(r codex.LogReport) {
	if !r.DirExists {
		fmt.Printf("No Codex directory found at %s\n", r.CodexDir)
		fmt.Println("Nothing to report — Codex may not be installed, or it hasn't run yet.")
		return
	}

	fmt.Printf("Codex directory: %s\n\n", r.CodexDir)
	fmt.Println("Codex log files:")

	anyPresent := false
	for _, f := range r.Files {
		if f.Exists {
			anyPresent = true
			fmt.Printf("  %-20s %10s\n", f.Name, codex.HumanBytes(f.Size))
		} else {
			fmt.Printf("  %-20s %10s\n", f.Name, "(absent)")
		}
	}

	fmt.Printf("\n%-20s %10s\n", "Total:", codex.HumanBytes(r.TotalBytes))

	if !anyPresent {
		fmt.Println("\nNo Codex log files are present right now — nothing is using disk here.")
	}
}

// cmdReport implements `codexssd report`.
//
// SAFETY: 100% read-only, and scoped to ~/.codex ONLY. It reports and points;
// it never acts and never suggests CodexSSD act on anything beyond its own
// known log files.
func cmdReport(args []string) int {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "output the report as JSON")
	toolName := fs.String("tool", "codex", "which AI tool to report on (codex, claude)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: codexssd report [--json] [--tool codex|claude]\n\n")
		fmt.Fprintf(os.Stderr, "Show what's using disk inside a tool's own directory (read-only).\n\n")
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
	rep := visibility.Scan(dir, time.Now(), cfg.StaleAfter())
	if *jsonOut {
		return emitJSON(rep)
	}
	renderVisibility(os.Stdout, rep, p.DisplayName)
	return 0
}

// renderVisibility prints the disk report in plain language. displayName is
// variadic so existing (pre-multi-tool) call sites — including main_test.go,
// which this task must leave unmodified — keep compiling unchanged; it
// defaults to "Codex", which reproduces today's exact wording byte-for-byte.
func renderVisibility(w io.Writer, r visibility.Report, displayName ...string) {
	name := "Codex"
	if len(displayName) > 0 && displayName[0] != "" {
		name = displayName[0]
	}
	isCodex := name == "Codex"

	if !r.DirExists {
		if isCodex {
			fmt.Fprintf(w, "No Codex directory found at %s — nothing is using disk here.\n", r.Dir)
		} else {
			fmt.Fprintf(w, "No %s directory found at %s — nothing is using disk here.\n", name, r.Dir)
		}
		return
	}
	if isCodex {
		fmt.Fprintf(w, "Disk use inside %s (%s total):\n\n", r.Dir, codex.HumanBytes(r.TotalBytes))
	} else {
		fmt.Fprintf(w, "%s disk use inside %s (%s total):\n\n", name, r.Dir, codex.HumanBytes(r.TotalBytes))
	}
	for _, e := range r.Entries {
		line := fmt.Sprintf("  %-24s %10s  (%d files)", e.Name, codex.HumanBytes(e.TotalBytes), e.FileCount)
		if e.Stale {
			line += fmt.Sprintf(" — untouched since %s", e.NewestMod.Format("January 2006"))
		}
		if e.IsOurs {
			line += "  [CodexSSD's own recycling bin]"
		}
		if e.ReadError != "" {
			line += "  (couldn't read everything here)"
		}
		fmt.Fprintln(w, line)
	}
	fmt.Fprintln(w)
	if isCodex {
		fmt.Fprintln(w, "CodexSSD only ever tidies its known Codex log files; the rest is")
	} else {
		fmt.Fprintf(w, "CodexSSD only ever tidies its known %s files; the rest is\n", name)
	}
	fmt.Fprintln(w, "yours to decide on. Nothing above has been touched.")
}

// cmdPrune implements `codexssd prune`: release backups past their hold to the
// OS Trash. --dry-run lists what would be released (read-only).
func cmdPrune(args []string) int {
	fs := flag.NewFlagSet("prune", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "list what would be released, without moving anything")
	jsonOut := fs.Bool("json", false, "output as JSON")
	toolName := fs.String("tool", "codex", "which AI tool's backups to prune (codex, claude)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: codexssd prune [--dry-run] [--json] [--tool codex|claude]\n\n")
		fmt.Fprintf(os.Stderr, "Move recycling-bin backups past their ~2-week hold into the OS Trash.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	p, dir, code := resolveTool(*toolName)
	if code != 0 {
		return code
	}

	if *dryRun {
		backups, err := cleaner.ListBackups(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "codexssd: could not read backups: %v\n", err)
			return 1
		}
		expired := cleaner.Expired(backups, time.Now())
		ids := make([]string, 0, len(expired))
		for _, b := range expired {
			ids = append(ids, filepath.Base(b.Dir))
		}
		if *jsonOut {
			return emitJSON(map[string]any{"would_release": ids})
		}
		if len(ids) == 0 {
			fmt.Println("Nothing past its hold — nothing to release.")
			return 0
		}
		fmt.Printf("%d backup(s) past their hold would be released to the Trash:\n", len(ids))
		for _, id := range ids {
			fmt.Printf("  %s\n", id)
		}
		return 0
	}

	released, err := cleaner.ReleaseExpired(dir, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: prune failed: %v\n", err)
		return 1
	}
	if len(released) > 0 {
		recordReceipt(recorder.Receipt{At: time.Now(), Action: pruneAction(p), BackupIDs: released})
	}
	if *jsonOut {
		if released == nil {
			released = []string{} // emit [] not null for empty
		}
		return emitJSON(map[string]any{"released": released})
	}
	if len(released) == 0 {
		fmt.Println("Nothing past its hold — nothing to release.")
		return 0
	}
	fmt.Printf("Released %d backup(s) to the Trash (recoverable until you empty it).\n", len(released))
	return 0
}

// pruneAction mirrors cleanAction/restoreAction: "prune" for the default
// tool, "prune --tool <name>" otherwise. No Receipt schema change.
func pruneAction(p tool.Profile) string {
	if p.Name == "codex" {
		return "prune"
	}
	return "prune --tool " + p.Name
}

// cmdSelf implements `codexssd self`: report CodexSSD's own footprint.
func cmdSelf(args []string) int {
	fs := flag.NewFlagSet("self", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "output the report as JSON")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: codexssd self [--json]\n\n")
		fmt.Fprintf(os.Stderr, "Report CodexSSD's own footprint (read-only).\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	dir, err := recorder.Dir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: could not determine your home directory: %v\n", err)
		return 1
	}
	rep, err := self.Measure(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: could not measure footprint: %v\n", err)
		return 1
	}

	if *jsonOut {
		return emitJSON(rep)
	}
	fmt.Println("CodexSSD's own footprint:")
	fmt.Printf("  mode:     %s\n", rep.Mode)
	fmt.Printf("  storage:  %s  (%s)\n", codex.HumanBytes(rep.HistoryBytes), rep.StateDir)
	if rep.Records > 0 {
		fmt.Printf("  history:  %d recorded action(s), last: %s\n", rep.Records, rep.LastAction)
	} else {
		fmt.Println("  history:  no recorded actions yet")
	}
	return 0
}

// cmdInstallAgent implements `codexssd install-agent`.
//
// It writes a disk/token-safe AGENTS.md into a repo. It refuses to overwrite an
// existing AGENTS.md unless --force; --print previews the rules without writing.
func cmdInstallAgent(args []string) int {
	fs := flag.NewFlagSet("install-agent", flag.ContinueOnError)
	profileName := fs.String("profile", string(agent.ProfileBalanced), "rule profile: balanced, strict, repo-only, disk-token-safe")
	force := fs.Bool("force", false, "overwrite an existing AGENTS.md")
	printOnly := fs.Bool("print", false, "print the rules to stdout instead of writing a file")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: codexssd install-agent [--profile <name>] [--force] [--print] [dir]\n\n")
		fmt.Fprintf(os.Stderr, "Write a disk/token-safe AGENTS.md into dir (default \".\").\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	p, err := agent.Parse(*profileName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: %v\n", err)
		return 2
	}

	dir := "."
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}

	if *printOnly {
		fmt.Print(agent.Content(p))
		return 0
	}

	path, replacedForeign, err := agent.Install(dir, p, *force)
	if err != nil {
		if errors.Is(err, agent.ErrExists) {
			fmt.Fprintf(os.Stderr, "codexssd: %s already exists — leaving it untouched.\n", filepath.Join(dir, agent.FileName))
			fmt.Fprintln(os.Stderr, "Re-run with --force to overwrite, or --print to preview.")
			return 1
		}
		fmt.Fprintf(os.Stderr, "codexssd: could not write AGENTS.md: %v\n", err)
		return 1
	}
	if replacedForeign {
		fmt.Printf("Note: replaced an existing AGENTS.md that CodexSSD didn't create.\n")
	}
	fmt.Printf("Wrote %s (%s profile).\n", path, p)
	return 0
}
