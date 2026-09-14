package mcpserver_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var (
	buildOnce sync.Once
	binPath   string
	buildErr  error

	// testDatabasePaths carries the current per-test database so the CLI
	// subprocess reads the exact rows the MCP session wrote.
	testDatabasePaths = map[string]string{}
	testMemBinary     string
)

// buildMemBinary builds the mem CLI once per test process so contract tests
// can verify MCP writes are visible through the real binary.
func buildMemBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "mem-bin")
		if err != nil {
			buildErr = err
			return
		}
		binPath = filepath.Join(dir, "mem")
		cmd := exec.Command("go", "build", "-o", binPath, ".")
		cmd.Dir = repoRoot(nil)
		buildErr = cmd.Run()
	})
	if buildErr != nil {
		t.Fatalf("build mem binary: %v", buildErr)
	}
	return binPath
}

func repoRoot(_ *testing.T) string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return filepath.Dir(filepath.Dir(wd)) // internal/mcpserver -> repo root
}

// runCLI executes the built mem binary against the same database the MCP
// session uses, returning raw stdout (the CLI JSON envelope).
func runCLI(t *testing.T, args ...string) string {
	t.Helper()
	full := append([]string{"--db", testDatabasePath()}, args...)
	cmd := exec.Command(buildMemBinary(t), full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("mem %v: %v\nstderr: %s", args, err, stderr.String())
	}
	return stdout.String()
}

func testDatabasePath() string { return testDatabasePaths["current"] }

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

func contains(haystack, needle string) bool {
	return len(needle) == 0 || bytes.Contains([]byte(haystack), []byte(needle))
}
