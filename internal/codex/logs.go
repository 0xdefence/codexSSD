package codex

import (
	"os"
	"path/filepath"

	"github.com/0xdefence/codexssd/internal/tool"
)

// LogFileNames are Codex's OWN local SQLite log files, relative to ~/.codex.
// The canonical definition now lives in the Codex tool profile; this alias
// keeps existing callers and the documented safety rule pointing at one list.
var LogFileNames = tool.Codex().OwnFixedFiles

// LogFile is a read-only snapshot of one Codex log file's presence and size.
type LogFile struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Size   int64  `json:"size_bytes"`
}

// LogReport is a read-only summary of the Codex log files under a directory.
type LogReport struct {
	CodexDir   string    `json:"codex_dir"`
	DirExists  bool      `json:"dir_exists"`
	Files      []LogFile `json:"files"`
	TotalBytes int64     `json:"total_bytes"`
}

// ScanLogs inspects the known Codex log files inside codexDir and returns a
// read-only report of which exist and how large they are.
//
// SAFETY: ScanLogs only ever calls os.Stat. It opens nothing, writes nothing,
// and touches only the known LogFileNames — never arbitrary directory contents.
func ScanLogs(codexDir string) LogReport {
	report := LogReport{
		CodexDir:  codexDir,
		DirExists: DirExists(codexDir),
		Files:     make([]LogFile, 0, len(LogFileNames)),
	}

	for _, name := range LogFileNames {
		path := filepath.Join(codexDir, name)
		f := LogFile{Name: name, Path: path}

		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			f.Exists = true
			f.Size = info.Size()
			report.TotalBytes += f.Size
		}

		report.Files = append(report.Files, f)
	}

	return report
}
