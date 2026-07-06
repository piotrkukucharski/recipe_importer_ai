package main

import (
	"net/http"
	"net/http/httptest"
	"recipe_importer_ai/infrastructure/api"
	"testing"

	mcp_sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerInitialization(t *testing.T) {
	h := &api.ApiHandler{}
	mcpServer := &mcp_sdk.Server{}
	sseHandler := mcp_sdk.NewSSEHandler(func(req *http.Request) *mcp_sdk.Server {
		return mcpServer
	}, nil)

	var e interface{}
	require.NotPanics(t, func() {
		e = api.SetupServer(h, sseHandler)
	}, "SetupServer should not panic")

	assert.NotNil(t, e)
}

func TestRouting(t *testing.T) {
	h := &api.ApiHandler{}
	mcpServer := &mcp_sdk.Server{}
	sseHandler := mcp_sdk.NewSSEHandler(func(req *http.Request) *mcp_sdk.Server {
		return mcpServer
	}, nil)

	e := api.SetupServer(h, sseHandler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Contains(t, []int{http.StatusOK, http.StatusFound, http.StatusTemporaryRedirect, http.StatusUnauthorized}, rec.Code)
}
