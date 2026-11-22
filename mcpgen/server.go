package mcpgen

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

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

	// SchemaMode picks full or compact tool schemas for tools/list; the zero
	// value means SchemaAuto. Set it before serving the first request.
	SchemaMode SchemaMode

	once         sync.Once
	compact      bool
	compactTools map[string]compactTool
	describeName string
}

type compactTool struct {
	schema      *schema.JSONSchema
	description string
}

func NewServer(name, version string, tools []Tool, client *gateway.Client) *Server {
	return &Server{Name: name, Version: version, Tools: tools, Client: client}
}

// resolve decides, once, whether tools/list advertises compact schemas and,
// if so, precomputes them.
func (s *Server) resolve() {
	s.once.Do(func() {
		switch s.SchemaMode {
		case SchemaFull:
			s.compact = false
		case SchemaCompact:
			s.compact = true
		default:
			s.compact = ListSize(s.Tools) > autoCompactBytes
		}
		if !s.compact {
			return
		}
		taken := make(map[string]bool, len(s.Tools))
		for _, t := range s.Tools {
			taken[t.Name] = true
		}
		s.describeName = describeToolName
		for taken[s.describeName] {
			s.describeName += "_"
		}
		s.compactTools = make(map[string]compactTool, len(s.Tools))
		for _, t := range s.Tools {
			cs, changed := CompactSchema(t.InputSchema)
			desc := t.Description
			if changed {
				desc += " (Nested fields are summarized; call " + s.describeName + " for the full input schema.)"
			}
			s.compactTools[t.Name] = compactTool{schema: cs, description: desc}
		}
	})
}

// Compact reports whether tools/list advertises compact schemas.
func (s *Server) Compact() bool {
	s.resolve()
	return s.compact
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
		s.resolve()
		list := make([]map[string]interface{}, 0, len(s.Tools)+1)
		for _, t := range s.Tools {
			if s.compact {
				ct := s.compactTools[t.Name]
				list = append(list, toolEntry(t, ct.schema, ct.description))
			} else {
				list = append(list, toolEntry(t, t.InputSchema, t.Description))
			}
		}
		if s.compact {
			list = append(list, s.describeEntry())
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

	s.resolve()
	if s.compact && params.Name == s.describeName {
		return s.describe(params.Arguments)
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

// describeEntry is the tools/list entry for the describe tool (compact mode).
func (s *Server) describeEntry() map[string]interface{} {
	f, t := false, true
	return map[string]interface{}{
		"name":        s.describeName,
		"description": "Returns the full JSON input schema of one operation. Call it before using an operation whose nested fields are summarized in its listing.",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string", "description": "Operation (tool) name."},
			},
			"required": []string{"name"},
		},
		"annotations": &Annotations{ReadOnlyHint: &t, IdempotentHint: &t, OpenWorldHint: &f},
	}
}

func (s *Server) describe(args map[string]interface{}) (interface{}, *rpcError) {
	name, _ := args["name"].(string)
	if name == "" {
		return toolError(`missing required argument "name" (an operation name)`), nil
	}
	for i := range s.Tools {
		if s.Tools[i].Name == name {
			return describeResult(&s.Tools[i])
		}
	}
	return toolError("unknown operation: " + name), nil
}
