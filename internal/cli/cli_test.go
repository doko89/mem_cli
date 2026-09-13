package cli_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"mem_cli/internal/cli"
)

func TestNamespaceCreateOutputsStableJSONContract(t *testing.T) {
	database := filepath.Join(t.TempDir(), "mem.db")
	var firstBytes, secondBytes bytes.Buffer

	first, _ := cli.NewRootCommand()
	first.SetOut(&firstBytes)
	first.SetErr(&firstBytes)
	first.SetArgs([]string{"--db", database, "ns", "create", "Work//Infra/"})
	if err := first.Execute(); err != nil {
		t.Fatalf("first namespace create: %v", err)
	}

	second, _ := cli.NewRootCommand()
	second.SetOut(&secondBytes)
	second.SetErr(&secondBytes)
	second.SetArgs([]string{"--db", database, "ns", "create", "work/infra"})
	if err := second.Execute(); err != nil {
		t.Fatalf("second namespace create: %v", err)
	}

	var firstResponse, secondResponse map[string]any
	if err := json.Unmarshal(firstBytes.Bytes(), &firstResponse); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	if err := json.Unmarshal(secondBytes.Bytes(), &secondResponse); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if firstResponse["ok"] != true || secondResponse["ok"] != true {
		t.Fatalf("unexpected responses: first=%s second=%s", firstBytes.String(), secondBytes.String())
	}
	if firstResponse["command"] != "ns.create" || secondResponse["command"] != "ns.create" {
		t.Fatalf("unexpected command names: first=%v second=%v", firstResponse["command"], secondResponse["command"])
	}
	firstData := firstResponse["data"].(map[string]any)
	secondData := secondResponse["data"].(map[string]any)
	if firstData["created"] != true || secondData["created"] != false {
		t.Fatalf("unexpected created flags: first=%v second=%v", firstData["created"], secondData["created"])
	}
}

func TestInvalidArgumentOutputsJSONAndExitCode(t *testing.T) {
	var output bytes.Buffer
	exitCode := cli.Run([]string{"--db", filepath.Join(t.TempDir(), "mem.db"), "add", "--namespace", "work"}, &output, &output)
	if exitCode != 2 {
		t.Fatalf("expected exit code 2, got %d", exitCode)
	}
	if !strings.Contains(output.String(), `"code":"INVALID_ARGUMENT"`) {
		t.Fatalf("expected stable JSON error, got %s", output.String())
	}
}
