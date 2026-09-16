package mcpgen

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// ServeStdio runs the MCP stdio transport: newline-delimited JSON-RPC
// requests on r, newline-delimited JSON-RPC responses on w. Blocks until r
// is exhausted (EOF) or a read error occurs.
func (s *Server) ServeStdio(ctx context.Context, r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	enc := json.NewEncoder(w)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			if encErr := enc.Encode(rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error: " + err.Error()}}); encErr != nil {
				return fmt.Errorf("writing MCP parse-error response: %w", encErr)
			}
			continue
		}
		resp := s.Handle(ctx, req)
		if resp == nil {
			continue // notification: no response
		}
		if err := enc.Encode(resp); err != nil {
			return fmt.Errorf("writing MCP response: %w", err)
		}
	}
	return scanner.Err()
}
