package notify

import (
	"os/exec"
	"runtime"
	"testing"
)

func TestNotifyInvokesPlatformTool(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("notification platforms only")
	}
	var gotName string
	var gotArgs []string
	execCommand = func(name string, args ...string) *exec.Cmd {
		gotName, gotArgs = name, args
		return exec.Command("true") // run something harmless
	}
	t.Cleanup(func() { execCommand = exec.Command })

	if err := Notify("CodexSSD", "WAL is 2 GiB"); err != nil {
		t.Fatal(err)
	}
	switch runtime.GOOS {
	case "darwin":
		if gotName != "osascript" || len(gotArgs) != 2 || gotArgs[0] != "-e" {
			t.Errorf("got %s %v", gotName, gotArgs)
		}
	case "linux":
		if gotName != "notify-send" {
			t.Errorf("got %s %v", gotName, gotArgs)
		}
	}
}
