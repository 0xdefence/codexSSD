// Package mcpserver is CodexSSD's read-only MCP (Model Context Protocol)
// server, spoken over stdio as newline-delimited JSON-RPC 2.0.
//
// SAFETY (hard product line): every tool is READ-ONLY. An AI agent connected
// to this server can see everything CodexSSD sees and touch NOTHING. There
// are no mutating tools, and none may ever be added — cleaning stays a human
// action. Implemented with the standard library only.
package mcpserver

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// protocolVersion is the MCP revision this server implements. Pinned
// deliberately: a read-only server would rather be honestly versioned than
// silently wrong.
const protocolVersion = "2025-06-18"

const serverName = "codexssd"
const serverVersion = "0.1.0"

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Serve reads newline-delimited JSON-RPC requests from r and writes responses
// to w until EOF. Notifications (no id) get no response, per JSON-RPC 2.0.
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // generous line cap
	enc := json.NewEncoder(w)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			// Parse errors respond with a null id, per spec.
			_ = enc.Encode(rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"),
				Error: &rpcError{Code: -32700, Message: "parse error"}})
			continue
		}
		resp, respond := s.handle(req)
		if respond {
			if err := enc.Encode(resp); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

// handle dispatches one request. respond=false for notifications.
func (s *Server) handle(req rpcRequest) (rpcResponse, bool) {
	isNotification := len(req.ID) == 0
	ok := func(result any) (rpcResponse, bool) {
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}, !isNotification
	}
	fail := func(code int, msg string) (rpcResponse, bool) {
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: code, Message: msg}}, !isNotification
	}

	switch req.Method {
	case "initialize":
		return ok(map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": serverName, "version": serverVersion},
			"instructions": "CodexSSD watches OpenAI Codex's disk and memory footprint. " +
				"All tools are read-only: you can inspect, never modify. " +
				"Cleaning is a human-only action via the codexssd CLI.",
		})
	case "notifications/initialized":
		return rpcResponse{}, false
	case "ping":
		return ok(map[string]any{})
	case "tools/list":
		return ok(map[string]any{"tools": toolDescriptors()})
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return fail(-32602, "invalid params")
		}
		text, err := s.callTool(params.Name)
		if err != nil {
			return fail(-32602, err.Error())
		}
		return ok(map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
			"isError": false,
		})
	default:
		return fail(-32601, fmt.Sprintf("method %q not found", req.Method))
	}
}
