// Command codexssd is a small, low-write local watchdog for AI coding agents
// (starting with OpenAI's Codex). It watches what an agent does to disk and
// memory, warns in plain language, safely tidies Codex's OWN log files into a
// recoverable recycling bin, and flags other clutter for the user to decide on.
//
// Phase 1 implements only the read-only `status` command.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/0xdefence/codexssd/internal/codex"
)

const usage = `codexssd - a low-write local watchdog for AI coding agents

Usage:
  codexssd <command> [flags]

Commands:
  status         Show Codex's log files and their sizes (read-only)
  watch          Watch a running Codex agent and warn on risky activity
  clean          Move Codex's own logs aside into a recoverable recycling bin
  install-agent  Write a "please behave" AGENTS.md into the current repo
  self           Report CodexSSD's own footprint
  help           Show this help

Run "codexssd <command> -h" for command-specific flags.
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "status":
		return cmdStatus(rest)
	case "watch":
		return cmdNotImplemented("watch")
	case "clean":
		return cmdNotImplemented("clean")
	case "install-agent":
		return cmdNotImplemented("install-agent")
	case "self":
		return cmdNotImplemented("self")
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
