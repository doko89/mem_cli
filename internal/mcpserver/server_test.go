package mcpserver_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mem_cli/internal/mcpserver"
)

// newSession starts an in-memory MCP session against a temp database so the
// tools can be exercised exactly like a real client would.
func newSession(t *testing.T) *mcp.ClientSession {
	t.Helper()
	database := filepath.Join(t.TempDir(), "mem.db")
	testMemBinary = buildMemBinary(t)
	testDatabasePaths["current"] = database
	ctx := context.Background()
	server := mcpserver.New(database)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	transport1, transport2 := mcp.NewInMemoryTransports()
	// The server must already be serving when the client connects, otherwise
	// the initialize request is never read and Connect deadlocks.
	go func() { _ = server.Run(ctx, transport1) }()
	session, err := client.Connect(ctx, transport2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	testDatabasePaths["current"] = database
	return session
}

func callTool(t *testing.T, session *mcp.ClientSession, name string, arguments any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      name,
		Arguments: arguments,
	})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	return result
}

func mustText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if result.IsError {
		t.Fatalf("tool returned error: %v", result.Content)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content part, got %d", len(result.Content))
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected text content, got %T", result.Content[0])
	}
	return text.Text
}

func TestMCPContractWithCLI(t *testing.T) {
	session := newSession(t)

	// 1. Add a memory through MCP.
	mustText(t, callTool(t, session, "memory_add", map[string]any{
		"namespace": "work/infra",
		"subject":   "topology",
		"content":   "Production cluster uses 3 control-plane nodes with Cilium CNI.",
		"type":      "fact",
		"reason":    "onboarding note",
		"tags":      []string{"k8s"},
	}))

	// 2. The exact same row must be visible through the CLI envelope.
	out := runCLI(t, "search", "--namespace", "work/infra", "--query", "cilium")
	if !contains(out, `"ok":true`) || !contains(out, "topology") {
		t.Fatalf("CLI search does not see MCP-written memory: %s", out)
	}

	// 3. Search through MCP and fetch the full memory.
	search := mustText(t, callTool(t, session, "memory_search", map[string]any{
		"namespace": "work/infra",
		"query":     "cilium",
	}))
	if !contains(search, "topology") {
		t.Fatalf("MCP search result missing subject: %s", search)
	}
	var payload struct {
		Memories []struct {
			ID string `json:"id"`
		} `json:"memories"`
	}
	if err := json.Unmarshal([]byte(search), &payload); err != nil || len(payload.Memories) != 1 {
		t.Fatalf("bad search payload: %s err=%v", search, err)
	}
	id := payload.Memories[0].ID

	got := mustText(t, callTool(t, session, "memory_get", map[string]any{"id": id}))
	if !contains(got, "control-plane") {
		t.Fatalf("memory_get content mismatch: %s", got)
	}

	// 4. Update through MCP bumps the version; history + revert work.
	mustText(t, callTool(t, session, "memory_update", map[string]any{
		"id":      id,
		"content": "Production cluster uses 5 control-plane nodes with Cilium CNI.",
	}))
	history := mustText(t, callTool(t, session, "memory_history", map[string]any{"id": id}))
	if !contains(history, "version") {
		t.Fatalf("history payload unexpected: %s", history)
	}
	reverted := mustText(t, callTool(t, session, "memory_revert", map[string]any{"id": id, "version": 1}))
	if !contains(reverted, "3 control-plane") {
		t.Fatalf("revert did not restore version 1: %s", reverted)
	}

	// 5. List + context round-trip.
	listOut := mustText(t, callTool(t, session, "memory_list", map[string]any{"namespace": "work/infra"}))
	if !contains(listOut, "topology") {
		t.Fatalf("memory_list missing subject: %s", listOut)
	}
	ctxOut := mustText(t, callTool(t, session, "memory_context", map[string]any{"namespace": "work/infra", "query": "cluster nodes"}))
	if !contains(ctxOut, "snippet") {
		t.Fatalf("memory_context payload unexpected: %s", ctxOut)
	}
}

func TestMCPMetadataMerge(t *testing.T) {
	session := newSession(t)

	mustText(t, callTool(t, session, "memory_add", map[string]any{
		"namespace": "test/merge",
		"subject":   "merge check",
		"content":   "entry for metadata merge regression",
		"metadata":  map[string]any{"alpha": "1", "beta": "2"},
	}))
	search := mustText(t, callTool(t, session, "memory_search", map[string]any{
		"namespace": "test/merge",
		"query":     "merge regression",
	}))
	var payload struct {
		Memories []struct {
			ID string `json:"id"`
		} `json:"memories"`
	}
	if err := json.Unmarshal([]byte(search), &payload); err != nil || len(payload.Memories) != 1 {
		t.Fatalf("bad search payload: %s err=%v", search, err)
	}
	id := payload.Memories[0].ID

	// Merge mode (metadata without metadata_set): new key is added, old keys kept.
	got := mustText(t, callTool(t, session, "memory_update", map[string]any{
		"id":       id,
		"metadata": map[string]any{"gamma": "3"},
	}))
	for _, key := range []string{`"alpha":"1"`, `"beta":"2"`, `"gamma":"3"`} {
		if !contains(got, key) {
			t.Fatalf("metadata merge lost %s in: %s", key, got)
		}
	}

	// Replace mode (metadata_set=true) must drop keys not included.
	got = mustText(t, callTool(t, session, "memory_update", map[string]any{
		"id":            id,
		"metadata":      map[string]any{"delta": "4"},
		"metadata_set":  true,
	}))
	if contains(got, `"gamma":"3"`) || !contains(got, `"delta":"4"`) {
		t.Fatalf("metadata replace mode unexpected result: %s", got)
	}
}

func TestMCPForget(t *testing.T) {
	session := newSession(t)

	mustText(t, callTool(t, session, "memory_add", map[string]any{
		"namespace": "test/forget",
		"subject":   "forget me",
		"content":   "entry to be deleted via MCP",
	}))
	search := mustText(t, callTool(t, session, "memory_search", map[string]any{
		"namespace": "test/forget",
		"query":     "deleted via MCP",
	}))
	var payload struct {
		Memories []struct {
			ID string `json:"id"`
		} `json:"memories"`
	}
	if err := json.Unmarshal([]byte(search), &payload); err != nil || len(payload.Memories) != 1 {
		t.Fatalf("bad search payload: %s err=%v", search, err)
	}
	id := payload.Memories[0].ID

	got := mustText(t, callTool(t, session, "memory_forget", map[string]any{"id": id}))
	if !contains(got, `"deleted":true`) {
		t.Fatalf("unexpected forget result: %s", got)
	}

	result := callTool(t, session, "memory_get", map[string]any{"id": id})
	if !result.IsError {
		t.Fatal("expected memory_get to fail after forget")
	}
	text := mustAllowError(t, result)
	if !contains(text, "NOT_FOUND") {
		t.Fatalf("expected not-found error, got: %s", text)
	}
}

func TestMCPImportInline(t *testing.T) {
	session := newSession(t)

	imported := mustText(t, callTool(t, session, "memory_import", map[string]any{
		"namespace": "docs",
		"filename":  "PRD.md",
		"content":   "# PRD\n\nLocal-first persistent memory layer.",
	}))
	if !contains(imported, `"subject":"PRD"`) {
		t.Fatalf("import did not derive subject from filename: %s", imported)
	}
	if !contains(imported, `mcp:PRD.md`) {
		t.Fatalf("import provenance missing: %s", imported)
	}
}

func TestMCPErrorMapping(t *testing.T) {
	session := newSession(t)

	result := callTool(t, session, "memory_search", map[string]any{
		"namespace": "does/not/exist",
		"query":     "anything",
	})
	if !result.IsError {
		t.Fatal("expected tool error for unknown namespace")
	}
	text := mustAllowError(t, result)
	if !contains(text, "NAMESPACE_NOT_FOUND") {
		t.Fatalf("expected domain error code in output, got: %s", text)
	}
	if contains(text, "goroutine") || contains(text, ".go:") {
		t.Fatalf("stack/internal detail leaked: %s", text)
	}
}

func mustAllowError(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content part, got %d", len(result.Content))
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected text content, got %T", result.Content[0])
	}
	return text.Text
}
