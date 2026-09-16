package mcpgen

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/harshhh28/soapbridge/gateway"
	"github.com/harshhh28/soapbridge/schema"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Server dispatches MCP JSON-RPC requests for one generated gateway's
// tools. Both transports (stdio.go, http.go) call Handle for each request.
type Server struct {
	Name    string
	Version string
	Tools   []Tool
	Client  *gateway.Client
}

func NewServer(name, version string, tools []Tool, client *gateway.Client) *Server {
	return &Server{Name: name, Version: version, Tools: tools, Client: client}
}

// Handle processes one JSON-RPC request and returns its response, or nil
// if req was a notification (no "id", per JSON-RPC 2.0 — no response is
// sent for those).
func (s *Server) Handle(ctx context.Context, req rpcRequest) *rpcResponse {
	isNotification := len(req.ID) == 0
	reply := func(result interface{}, errObj *rpcError) *rpcResponse {
		if isNotification {
			return nil
		}
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result, Error: errObj}
	}

	switch req.Method {
	case "initialize":
		return reply(map[string]interface{}{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo":      map[string]interface{}{"name": s.Name, "version": s.Version},
		}, nil)

	case "notifications/initialized", "notifications/cancelled":
		return nil // no response for notifications

	case "ping":
		return reply(map[string]interface{}{}, nil)

	case "tools/list":
		list := make([]map[string]interface{}, 0, len(s.Tools))
		for _, t := range s.Tools {
			list = append(list, map[string]interface{}{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": t.InputSchema,
			})
		}
		return reply(map[string]interface{}{"tools": list}, nil)

	case "tools/call":
		return reply(s.callTool(ctx, req.Params))

	default:
		return reply(nil, &rpcError{Code: -32601, Message: "method not found: " + req.Method})
	}
}

func (s *Server) callTool(ctx context.Context, rawParams json.RawMessage) (interface{}, *rpcError) {
	var params struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := json.Unmarshal(rawParams, &params); err != nil {
		return nil, &rpcError{Code: -32602, Message: "invalid params: " + err.Error()}
	}

	var tool *Tool
	for i := range s.Tools {
		if s.Tools[i].Name == params.Name {
			tool = &s.Tools[i]
			break
		}
	}
	if tool == nil {
		return nil, &rpcError{Code: -32602, Message: "unknown tool: " + params.Name}
	}

	args := params.Arguments
	if args == nil {
		args = map[string]interface{}{}
	}
	if errs := schema.Validate(tool.InputSchema, args); len(errs) > 0 {
		return toolError(fmt.Sprintf("invalid arguments: %v", errs)), nil
	}

	value, fault, err := s.Client.Call(ctx, tool.op, args, gateway.Credentials{})
	if err != nil {
		return toolError(err.Error()), nil
	}
	if fault != nil {
		return toolError(fmt.Sprintf("SOAP fault [%s]: %s", fault.Code, fault.Message)), nil
	}

	b, _ := json.Marshal(value)
	return map[string]interface{}{
		"content": []map[string]interface{}{{"type": "text", "text": string(b)}},
		"isError": false,
	}, nil
}

func toolError(msg string) map[string]interface{} {
	return map[string]interface{}{
		"content": []map[string]interface{}{{"type": "text", "text": msg}},
		"isError": true,
	}
}
