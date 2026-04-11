// Package mcp provides the MCP HTTP server for sys-mcp-center.
package mcp

import (
	"net/http"
	"strings"
)

// BearerTokenMiddleware rejects requests whose Authorization header does not
// match one of the allowed tokens. Use this to protect the MCP HTTP endpoint.
func BearerTokenMiddleware(tokens []string, next http.Handler) http.Handler {
	set := make(map[string]struct{}, len(tokens))
	for _, t := range tokens {
		set[t] = struct{}{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow unauthenticated only if no tokens configured.
		if len(tokens) == 0 {
			next.ServeHTTP(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		token := strings.TrimPrefix(auth, "Bearer ")
		if _, ok := set[token]; !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
