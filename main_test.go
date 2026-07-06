package main

import (
	"net/http"
	"net/http/httptest"
	"recipe_importer_ai/api"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServerInitialization tests that the Echo routing, middleware, and
// MCP Server registration are successfully configured without panicking.
func TestServerInitialization(t *testing.T) {
	// Create a minimal Handler. Since the setup only registers endpoints and
	// the MCP server callbacks are not executed during initialization,
	// passing nil dependencies is safe.
	h := &api.Handler{}

	// Verify that setupServer succeeds and sets up the router
	var e interface{}
	require.NotPanics(t, func() {
		e = setupServer(h)
	}, "setupServer should not panic")

	assert.NotNil(t, e)

	// We can also verify BuildMCPServer directly
	mcpServer := api.BuildMCPServer(h)
	assert.NotNil(t, mcpServer)
}

// TestRouting verifies that basic endpoint definitions respond.
func TestRouting(t *testing.T) {
	h := &api.Handler{}
	e := setupServer(h)

	// Perform a mock HTTP request to the root path
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// Since we haven't logged in, or the handler has nil dependencies,
	// we just want to ensure it doesn't panic and returns a response.
	// We expect 200 OK or 302 Redirect for the home route, etc.
	assert.Contains(t, []int{http.StatusOK, http.StatusFound, http.StatusTemporaryRedirect, http.StatusUnauthorized}, rec.Code)
}
