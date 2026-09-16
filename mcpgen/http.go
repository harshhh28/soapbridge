package mcpgen

import (
	"encoding/json"
	"net/http"
)

// HTTPHandler implements the synchronous subset of MCP's Streamable HTTP
// transport: POST one JSON-RPC request, get one JSON-RPC response body
// back. Server-initiated messages / SSE streaming are not implemented in
// v1 (see package doc) — every tool call here is a plain request/response,
// which is sufficient for a remote agent invoking tools synchronously.
func (s *Server) HTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed; MCP Streamable HTTP requires POST", http.StatusMethodNotAllowed)
			return
		}
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeRPC(w, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error: " + err.Error()}})
			return
		}
		resp := s.Handle(r.Context(), req)
		if resp == nil {
			w.WriteHeader(http.StatusAccepted) // notification: nothing to return
			return
		}
		writeRPC(w, *resp)
	}
}

func writeRPC(w http.ResponseWriter, resp rpcResponse) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
