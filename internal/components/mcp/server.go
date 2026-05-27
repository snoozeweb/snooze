package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
)

// JSON-RPC 2.0 + MCP constants.
const (
	jsonrpcVersion = "2.0"

	// supportedProtocolVersion is the latest MCP protocol revision this
	// server implements. Confirmed against the MCP specification
	// (modelcontextprotocol.io, revision 2025-06-18). The initialize handler
	// echoes back the client's requested version when present (per the spec's
	// version-negotiation rule: "if the server supports the requested version
	// it MUST respond with the same version"), falling back to this value.
	supportedProtocolVersion = "2025-06-18"

	serverName = "snooze-mcp"
)

// JSON-RPC standard error codes (subset used by this server).
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// rpcRequest is the inbound JSON-RPC 2.0 envelope. ID is captured as
// json.RawMessage because the spec allows string, number, or null and we
// must echo it back verbatim. A nil/absent ID marks a notification, to which
// we send no response.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// isNotification reports whether the request omitted an id. JSON-RPC
// notifications get no response.
func (r rpcRequest) isNotification() bool {
	return len(r.ID) == 0 || string(r.ID) == "null"
}

// rpcResponse is the outbound JSON-RPC 2.0 envelope. Exactly one of Result /
// Error is set on any given response.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is the JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Server is the pure JSON-RPC engine. It owns no I/O of its own: Handle takes
// a single raw request and returns the raw response bytes (nil for
// notifications), which makes it trivially unit-testable. The Daemon wires it
// to stdin/stdout in daemon.go.
type Server struct {
	api    snoozeAPI
	logger *slog.Logger

	// version is the build version reported in initialize's serverInfo.
	version string
}

// NewServer builds a Server over the given snooze API. logger may be nil
// (slog.Default is used). version is surfaced in the initialize response.
func NewServer(api snoozeAPI, version string, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	if version == "" {
		version = "dev"
	}
	return &Server{api: api, logger: logger, version: version}
}

// Handle processes one JSON-RPC message and returns the response bytes to
// write back, or nil when no response is due (notifications, or an
// unparseable notification). It never returns an error: protocol problems
// are encoded as JSON-RPC error responses so the caller's loop stays simple.
func (s *Server) Handle(ctx context.Context, raw []byte) []byte {
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		// We can't recover the id, so reply with a null-id parse error per
		// the JSON-RPC spec.
		return s.encode(rpcResponse{
			JSONRPC: jsonrpcVersion,
			ID:      json.RawMessage("null"),
			Error:   &rpcError{Code: codeParseError, Message: "parse error: " + err.Error()},
		})
	}

	switch req.Method {
	case "initialize":
		return s.reply(req, s.handleInitialize(req.Params), nil)
	case "notifications/initialized", "initialized":
		// Notification — no response.
		return nil
	case "ping":
		// MCP ping → empty result object. Harmless to support.
		return s.reply(req, map[string]any{}, nil)
	case "tools/list":
		return s.reply(req, s.handleToolsList(), nil)
	case "tools/call":
		result, rpcErr := s.handleToolsCall(ctx, req.Params)
		return s.reply(req, result, rpcErr)
	default:
		if req.isNotification() {
			// Unknown notification — silently ignore per JSON-RPC.
			s.logger.Debug("mcp: ignoring unknown notification", slog.String("method", req.Method))
			return nil
		}
		return s.reply(req, nil, &rpcError{
			Code:    codeMethodNotFound,
			Message: "method not found: " + req.Method,
		})
	}
}

// reply assembles the response envelope. For notifications it returns nil so
// the transport writes nothing. When rpcErr is non-nil it wins over result.
func (s *Server) reply(req rpcRequest, result any, rpcErr *rpcError) []byte {
	if req.isNotification() {
		return nil
	}
	resp := rpcResponse{JSONRPC: jsonrpcVersion, ID: req.ID}
	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		resp.Result = result
	}
	return s.encode(resp)
}

// encode marshals resp to a single compact JSON line (no trailing newline —
// the transport appends the delimiter). On the unlikely marshal failure it
// returns a hand-built internal-error envelope so the caller always has
// something to write.
func (s *Server) encode(resp rpcResponse) []byte {
	out, err := json.Marshal(resp)
	if err != nil {
		s.logger.Error("mcp: failed to marshal response", slog.Any("err", err))
		return []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"internal error: response marshal failed"}}`)
	}
	return out
}

// initializeResult is the MCP initialize response payload.
type initializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ServerInfo      serverInfo     `json:"serverInfo"`
}

// serverInfo identifies this server to the MCP client.
type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// handleInitialize negotiates the protocol version and advertises the tools
// capability. Per the spec we echo back the client's requested version when
// it sent one (maximising interop with clients pinned to an older revision)
// and otherwise advertise the latest revision we support.
func (s *Server) handleInitialize(params json.RawMessage) initializeResult {
	version := supportedProtocolVersion
	if len(params) > 0 {
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if err := json.Unmarshal(params, &p); err == nil && p.ProtocolVersion != "" {
			version = p.ProtocolVersion
		}
	}
	return initializeResult{
		ProtocolVersion: version,
		Capabilities: map[string]any{
			"tools": map[string]any{},
		},
		ServerInfo: serverInfo{Name: serverName, Version: s.version},
	}
}
