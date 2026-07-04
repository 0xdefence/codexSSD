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
	"github.com/0xdefence/codexssd/internal/recorder"
	"github.com/0xdefence/codexssd/internal/self"
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
  help           Show this help

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
		return cmdNotImplemented("watch")
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
	case "help", "-h", "--help":
		fmt.Print(usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "codexssd: unknown command %q\n\n%s", cmd, usage)
		return 2
	}
}

// cmdNotImplemented is a placeholder for Phase 1 commands whose home exists but
// whose behavior has not landed yet. Keeping them in the dispatch makes the
// planned CLI surface visible and testable.
func cmdNotImplemented(name string) int {
	fmt.Fprintf(os.Stderr, "codexssd: %q is planned for Phase 1 but not implemented yet.\n", name)
	return 1
}

// cmdStatus implements `codexssd status`.
//
// SAFETY: this command is 100% READ-ONLY. It only locates ~/.codex and calls
// os.Stat on Codex's known log files. It moves nothing, deletes nothing, and
// writes nothing to disk.
func cmdStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "output the report as JSON")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: codexssd status [--json]\n\n")
		fmt.Fprintf(os.Stderr, "Report the size of Codex's own log files (read-only).\n\n")
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

	report := codex.ScanLogs(dir)

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintf(os.Stderr, "codexssd: failed to encode JSON: %v\n", err)
			return 1
		}
		return 0
	}

	printStatus(report)
	return 0
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

// isCodexRunning is the process-check used by the file-mutating commands. It is
// a package variable so tests can substitute a deterministic stub, making the
// safety gate (refuse while Codex is running) verifiable on any machine without
// a real Codex process or dependence on the host's process table.
var isCodexRunning = codex.IsCodexRunning

// cmdClean implements `codexssd clean`.
//
// Default is a read-only dry run. `--yes` moves Codex's own logs aside into the
// recycling bin, but only after confirming Codex is not running. Nothing is ever
// deleted.
func cmdClean(args []string) int {
	fs := flag.NewFlagSet("clean", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "actually move the logs aside (default is a dry run)")
	jsonOut := fs.Bool("json", false, "output as JSON")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: codexssd clean [--yes] [--json]\n\n")
		fmt.Fprintf(os.Stderr, "Move Codex's own log files aside into a recoverable recycling bin.\n")
		fmt.Fprintf(os.Stderr, "Without --yes this only shows what would happen (read-only).\n\n")
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

	plan, err := cleaner.PlanCodexLogs(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: could not inspect Codex logs: %v\n", err)
		return 1
	}

	running, runErr := isCodexRunning()
	supported := runErr != codex.ErrUnsupportedPlatform

	if !*yes {
		if *jsonOut {
			out := map[string]any{
				"plan":               plan,
				"codex_running":      running,
				"platform_supported": supported,
			}
			if runErr != nil && supported {
				out["check_error"] = runErr.Error()
			}
			return emitJSON(out)
		}
		renderPlan(os.Stdout, plan, running, supported)
		return 0
	}

	// --yes: actually move aside. Refuse unless we can confirm Codex is stopped.
	if !supported {
		fmt.Fprintln(os.Stderr, "codexssd: cannot verify Codex is closed on this platform; refusing to move files.")
		fmt.Fprintln(os.Stderr, "Run without --yes to see what would be moved.")
		return 1
	}
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "codexssd: could not check whether Codex is running: %v\n", runErr)
		return 1
	}
	if running {
		fmt.Fprintln(os.Stderr, "codexssd: Codex appears to be running. Close it first, then try again.")
		return 1
	}
	if plan.Empty() {
		fmt.Println("Nothing to move aside — no Codex log files are present.")
		return 0
	}

	cfg := loadConfig()
	dest, err := plan.ApplyWithHold(time.Now(), cfg.BinHold())
	if err != nil {
		fmt.Fprintf(os.Stderr, "codexssd: clean failed: %v\n", err)
		return 1
	}
	fmt.Printf("Moved %s of Codex logs aside to:\n  %s\n", codex.HumanBytes(plan.TotalBytes), dest)
	fmt.Println("Nothing was deleted. Restore them any time with \"codexssd restore\".")
	return 0
}

// renderPlan prints a friendly, plain-language dry-run report.
func renderPlan(w io.Writer, p cleaner.Plan, running bool, supported bool) {
	if p.Empty() {
		fmt.Fprintf(w, "Nothing to move aside — no Codex log files are present in %s.\n", p.CodexDir)
		return
	}

	fmt.Fprintf(w, "CodexSSD found %s of Codex log files it can safely move aside:\n\n", codex.HumanBytes(p.TotalBytes))
	for _, it := range p.Items {
		fmt.Fprintf(w, "  %-20s %10s\n", it.Name, codex.HumanBytes(it.Size))
	}
	fmt.Fprintf(w, "  %-20s %10s\n\n", "Total", codex.HumanBytes(p.TotalBytes))
	fmt.Fprintln(w, "These are Codex's own logs. Moving them frees the space; Codex makes")
	fmt.Fprintln(w, "fresh ones next time it runs. Nothing is deleted — files go to a")
	fmt.Fprintln(w, "recoverable bin and can be restored.")
	fmt.Fprintln(w)

	switch {
	case !supported:
		fmt.Fprintln(w, "Note: this platform can't check whether Codex is running, so --yes is disabled here.")
	case running:
		fmt.Fprintln(w, "Codex appears to be running. Close it before cleaning.")
	default:
		fmt.Fprintln(w, "Codex doesn't appear to be running.")
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
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: codexssd restore [--json] [backup-id]\n\n")
		fmt.Fprintf(os.Stderr, "With no id, lists recoverable backups. With an id, restores that backup.\n\n")
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

	// Restoring overwrites the live log location, so refuse while Codex runs.
	running, runErr := isCodexRunning()
	if runErr == codex.ErrUnsupportedPlatform {
		fmt.Fprintln(os.Stderr, "codexssd: cannot verify Codex is closed on this platform; refusing to restore.")
		return 1
	}
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "codexssd: could not check whether Codex is running: %v\n", runErr)
		return 1
	}
	if running {
		fmt.Fprintln(os.Stderr, "codexssd: Codex appears to be running. Close it first, then try again.")
		return 1
	}

	id := fs.Arg(0)
	for _, b := range backups {
		if filepath.Base(b.Dir) == id {
			if err := cleaner.Restore(b.Dir); err != nil {
				fmt.Fprintf(os.Stderr, "codexssd: restore failed: %v\n", err)
				return 1
			}
			if *jsonOut {
				return emitJSON(map[string]any{
					"status":    "restored",
					"id":        id,
					"codex_dir": dir,
				})
			}
			fmt.Printf("Restored backup %s to %s.\n", id, dir)
			return 0
		}
	}
	fmt.Fprintf(os.Stderr, "codexssd: no backup with id %q. Run \"codexssd restore\" to list them.\n", id)
	return 1
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
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: codexssd report [--json]\n\n")
		fmt.Fprintf(os.Stderr, "Show what's using disk inside ~/.codex (read-only).\n\n")
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
	rep := visibility.Scan(dir, time.Now(), cfg.StaleAfter())
	if *jsonOut {
		return emitJSON(rep)
	}
	renderVisibility(os.Stdout, rep)
	return 0
}

// renderVisibility prints the disk report in plain language.
func renderVisibility(w io.Writer, r visibility.Report) {
	if !r.DirExists {
		fmt.Fprintf(w, "No Codex directory found at %s — nothing is using disk here.\n", r.Dir)
		return
	}
	fmt.Fprintf(w, "Disk use inside %s (%s total):\n\n", r.Dir, codex.HumanBytes(r.TotalBytes))
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
	fmt.Fprintln(w, "CodexSSD only ever tidies its known Codex log files; the rest is")
	fmt.Fprintln(w, "yours to decide on. Nothing above has been touched.")
}

// cmdPrune implements `codexssd prune`: release backups past their hold to the
// OS Trash. --dry-run lists what would be released (read-only).
func cmdPrune(args []string) int {
	fs := flag.NewFlagSet("prune", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "list what would be released, without moving anything")
	jsonOut := fs.Bool("json", false, "output as JSON")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: codexssd prune [--dry-run] [--json]\n\n")
		fmt.Fprintf(os.Stderr, "Move recycling-bin backups past their ~2-week hold into the OS Trash.\n\n")
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
