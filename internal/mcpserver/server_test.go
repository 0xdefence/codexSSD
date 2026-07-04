package mcpserver

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/0xdefence/codexssd/internal/codex"
	"github.com/0xdefence/codexssd/internal/visibility"
)

// serve runs the given newline-delimited requests through a stubbed server and
// returns one decoded JSON object per response line.
func serve(t *testing.T, requests ...string) []map[string]any {
	t.Helper()
	s := New()
	// Stub every data source: protocol tests must not read the real ~/.codex.
	s.status = func() (codex.LogReport, error) {
		return codex.LogReport{CodexDir: "/tmp/x/.codex", DirExists: true, Files: []codex.LogFile{}, TotalBytes: 42}, nil
	}
	s.cleanPlan = func() (any, error) { return map[string]any{"total_bytes": 42}, nil }
	s.backups = func() (any, error) { return []any{}, nil }
	s.selfReport = func() (any, error) { return map[string]any{"mode": "low-write"}, nil }
	s.diskReport = func() (visibility.Report, error) {
		return visibility.Scan("/nonexistent-for-test", time.Now(), time.Hour), nil
	}

	in := strings.NewReader(strings.Join(requests, "\n") + "\n")
	var out bytes.Buffer
	if err := s.Serve(in, &out); err != nil {
		t.Fatal(err)
	}
	var resps []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("bad response line %q: %v", line, err)
		}
		resps = append(resps, m)
	}
	return resps
}

func TestInitializeHandshake(t *testing.T) {
	resps := serve(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`,
	)
	if len(resps) != 1 {
		t.Fatalf("want 1 response, got %d", len(resps))
	}
	result := resps[0]["result"].(map[string]any)
	if result["protocolVersion"] != "2025-06-18" {
		t.Errorf("protocolVersion = %v", result["protocolVersion"])
	}
	caps := result["capabilities"].(map[string]any)
	if _, ok := caps["tools"]; !ok {
		t.Error("must advertise tools capability")
	}
}

func TestToolsListAndCall(t *testing.T) {
	resps := serve(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"codex_status","arguments":{}}}`,
	)
	if len(resps) != 3 { // the notification gets NO response
		t.Fatalf("want 3 responses, got %d", len(resps))
	}
	tools := resps[1]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 5 {
		t.Fatalf("want 5 tools, got %d", len(tools))
	}
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"codex_status", "clean_plan", "list_backups", "self_report", "disk_report"} {
		if !names[want] {
			t.Errorf("missing tool %s", want)
		}
	}
	callResult := resps[2]["result"].(map[string]any)
	content := callResult["content"].([]any)[0].(map[string]any)
	if content["type"] != "text" || !strings.Contains(content["text"].(string), "42") {
		t.Errorf("bad tool result: %v", content)
	}
}

func TestErrors(t *testing.T) {
	resps := serve(t,
		`{"jsonrpc":"2.0","id":1,"method":"no/such/method"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"delete_everything"}}`,
		`this is not json`,
		`{"jsonrpc":"2.0","id":3,"method":"ping"}`,
	)
	if len(resps) != 4 {
		t.Fatalf("want 4 responses (incl. parse error), got %d", len(resps))
	}
	if code := resps[0]["error"].(map[string]any)["code"].(float64); code != -32601 {
		t.Errorf("unknown method code = %v, want -32601", code)
	}
	if code := resps[1]["error"].(map[string]any)["code"].(float64); code != -32602 {
		t.Errorf("unknown tool code = %v, want -32602", code)
	}
	if code := resps[2]["error"].(map[string]any)["code"].(float64); code != -32700 {
		t.Errorf("parse error code = %v, want -32700", code)
	}
	if _, ok := resps[3]["result"]; !ok {
		t.Error("ping must return an empty result")
	}
}
