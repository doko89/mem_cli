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

func TestNamespaceGetAndListUseSnakeCaseJSON(t *testing.T) {
	database := filepath.Join(t.TempDir(), "mem.db")
	createOutput, getOutput, listOutput := &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}
	if code := cli.Run([]string{"--db", database, "ns", "create", "Work//Infra/"}, createOutput, createOutput); code != 0 {
		t.Fatalf("namespace create exit code: %d output=%s", code, createOutput.String())
	}
	if code := cli.Run([]string{"--db", database, "ns", "get", "work/infra"}, getOutput, getOutput); code != 0 {
		t.Fatalf("namespace get exit code: %d output=%s", code, getOutput.String())
	}
	if code := cli.Run([]string{"--db", database, "ns", "list"}, listOutput, listOutput); code != 0 {
		t.Fatalf("namespace list exit code: %d output=%s", code, listOutput.String())
	}

	var getResponse, listResponse map[string]any
	if err := json.Unmarshal(getOutput.Bytes(), &getResponse); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if err := json.Unmarshal(listOutput.Bytes(), &listResponse); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	getData := getResponse["data"].(map[string]any)
	expectedKeys := []string{"id", "name", "normalized_name", "parent_id", "created_at"}
	for _, key := range expectedKeys {
		if _, ok := getData[key]; !ok {
			t.Fatalf("namespace get key %q missing: %s", key, getOutput.String())
		}
	}
	namespaces := listResponse["data"].(map[string]any)["namespaces"].([]any)
	if len(namespaces) != 2 {
		t.Fatalf("expected parent and child namespaces, got %s", listOutput.String())
	}
	for _, item := range namespaces {
		namespace := item.(map[string]any)
		for _, key := range expectedKeys {
			if _, ok := namespace[key]; !ok {
				t.Fatalf("namespace list key %q missing: %s", key, listOutput.String())
			}
		}
	}
}

func TestNamespaceResolveByID(t *testing.T) {
	database := filepath.Join(t.TempDir(), "mem.db")
	createOutput, resolveOutput := &bytes.Buffer{}, &bytes.Buffer{}
	if code := cli.Run([]string{"--db", database, "ns", "create", "ayla/main"}, createOutput, createOutput); code != 0 {
		t.Fatalf("namespace create exit code: %d output=%s", code, createOutput.String())
	}
	var createResponse map[string]any
	if err := json.Unmarshal(createOutput.Bytes(), &createResponse); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	namespaceID := createResponse["data"].(map[string]any)["id"].(string)
	if code := cli.Run([]string{"--db", database, "ns", "resolve", namespaceID}, resolveOutput, resolveOutput); code != 0 {
		t.Fatalf("namespace resolve exit code: %d output=%s", code, resolveOutput.String())
	}
	if !strings.Contains(resolveOutput.String(), `"normalized_name":"ayla/main"`) {
		t.Fatalf("namespace did not resolve to path: %s", resolveOutput.String())
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
