// Package notify fires a native desktop notification. It is strictly
// fire-and-forget: callers treat any failure as ignorable, because the
// terminal output is always the source of truth.
package notify

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
)

// ErrUnsupported means this platform has no notification pathway.
var ErrUnsupported = errors.New("desktop notifications not supported on this platform")

// execCommand is a seam so tests never spawn real notification UIs.
var execCommand = exec.Command

// Notify shows a desktop notification with the given title and body.
func Notify(title, body string) error {
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf("display notification %q with title %q", body, title)
		return execCommand("osascript", "-e", script).Run()
	case "linux":
		return execCommand("notify-send", title, body).Run()
	default:
		return ErrUnsupported
	}
}
