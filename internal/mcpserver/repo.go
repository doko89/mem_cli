package mcpserver

import (
	"context"

	"mem_cli/internal/sqlite"
)

// openRepository opens the SQLite repository. It exists so tests can pin the
// database path and so the transport layer never imports sqlite directly.
func openRepository(_ context.Context, databasePath string) (*sqlite.Repository, error) {
	return sqlite.Open(databasePath)
}
