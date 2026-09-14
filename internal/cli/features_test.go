package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"mem_cli/internal/cli"
)

func runCommand(t *testing.T, arguments ...string) (map[string]any, int) {
	t.Helper()
	var output bytes.Buffer
	exitCode := cli.Run(arguments, &output, &output)
	var response map[string]any
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatalf("decode response %v: %s", arguments, output.String())
	}
	return response, exitCode
}

func commandData(t *testing.T, response map[string]any) map[string]any {
	t.Helper()
	data, ok := response["data"].(map[string]any)
	if !ok || response["ok"] != true {
		t.Fatalf("unexpected response: %#v", response)
	}
	return data
}

func TestDocumentFileContentImportAndDocFilter(t *testing.T) {
	directory := t.TempDir()
	database := filepath.Join(directory, "mem.db")
	document := filepath.Join(directory, "runbook.md")
	content := "# Runbook\n\n```sh\nmem list --type doc\n```\n"
	if err := os.WriteFile(document, []byte(content), 0o644); err != nil {
		t.Fatalf("write document: %v", err)
	}

	response, exitCode := runCommand(t, "--db", database, "ns", "create", "work/infra")
	if exitCode != 0 {
		t.Fatalf("namespace exit code: %d response=%#v", exitCode, response)
	}
	response, exitCode = runCommand(t, "--db", database, "add", "--namespace", "work/infra", "--subject", "manual", "--file", document, "--metadata", `{"format":"markdown"}`)
	if exitCode != 0 {
		t.Fatalf("add exit code: %d response=%#v", exitCode, response)
	}
	added := commandData(t, response)
	if added["type"] != "fact" {
		t.Fatalf("add without --type: got %#v want fact", added["type"])
	}
	memoryID, _ := added["id"].(string)
	if memoryID == "" {
		t.Fatalf("missing memory id: %#v", added)
	}
	response, _ = runCommand(t, "--db", database, "get", memoryID)
	retrieved := commandData(t, response)
	if retrieved["content"] != content {
		t.Fatalf("content was not byte-identical: got %#v want %q", retrieved["content"], content)
	}

	response, exitCode = runCommand(t, "--db", database, "import", document, "--db", database, "--namespace", "work/infra")
	if exitCode != 0 {
		t.Fatalf("import exit code: %d response=%#v", exitCode, response)
	}
	imported := commandData(t, response)
	if imported["type"] != "doc" || imported["subject"] != "runbook" {
		t.Fatalf("unexpected import metadata: %#v", imported)
	}
	source, _ := imported["source"].(map[string]any)
	if source["path"] != document || source["type"] != "file" {
		t.Fatalf("unexpected import source: %#v", source)
	}
	if imported["content"] != content {
		t.Fatalf("imported content changed: %#v", imported["content"])
	}

	response, _ = runCommand(t, "--db", database, "list", "--namespace", "work/infra")
	listData := commandData(t, response)
	memories, _ := listData["memories"].([]any)
	if len(memories) != 2 {
		t.Fatalf("expected omitted --type to include every type, got %#v", memories)
	}

	response, _ = runCommand(t, "--db", database, "list", "--namespace", "work/infra", "--type", "doc")
	listData = commandData(t, response)
	memories, _ = listData["memories"].([]any)
	if len(memories) != 1 {
		t.Fatalf("expected one document, got %#v", memories)
	}
	for _, item := range memories {
		memory, _ := item.(map[string]any)
		if _, hasFullContent := memory["content"]; hasFullContent {
			t.Fatalf("list returned full content: %#v", memory)
		}
		if snippet, _ := memory["snippet"].(string); len([]rune(snippet)) > 200 || bytes.ContainsRune([]byte(snippet), '\n') {
			t.Fatalf("invalid list snippet: %#v", snippet)
		}
	}
	response, _ = runCommand(t, "--db", database, "list", "--namespace", "work/infra", "--type", "preference")
	listData = commandData(t, response)
	if memories, _ = listData["memories"].([]any); len(memories) != 0 {
		t.Fatalf("expected no preferences, got %#v", memories)
	}
}

func TestAddWithoutTypeDefaultsToFactAndResponseShowsRelatedOut(t *testing.T) {
	database := filepath.Join(t.TempDir(), "mem.db")
	response, exitCode := runCommand(t, "--db", database, "ns", "create", "work/infra")
	if exitCode != 0 {
		t.Fatalf("namespace exit code: %d response=%#v", exitCode, response)
	}
	response, exitCode = runCommand(t, "--db", database, "add", "--namespace", "work/infra", "--subject", "source", "--content", "source")
	if exitCode != 0 {
		t.Fatalf("source add exit code: %d response=%#v", exitCode, response)
	}
	source := commandData(t, response)
	if source["type"] != "fact" {
		t.Fatalf("source type: got %#v want fact", source["type"])
	}
	sourceID, _ := source["id"].(string)

	response, exitCode = runCommand(t, "--db", database, "add", "--namespace", "work/infra", "--subject", "target", "--content", "target", "--related", sourceID)
	if exitCode != 0 {
		t.Fatalf("target add exit code: %d response=%#v", exitCode, response)
	}
	target := commandData(t, response)
	if target["type"] != "fact" {
		t.Fatalf("target type: got %#v want fact", target["type"])
	}
	relatedOut, _ := target["related_out"].([]any)
	if len(relatedOut) != 1 {
		t.Fatalf("add response related_out: %#v", target)
	}
	link, _ := relatedOut[0].(map[string]any)
	linkTarget, _ := link["memory"].(map[string]any)
	if linkTarget["id"] != sourceID {
		t.Fatalf("unexpected related_out target: %#v", link)
	}
}

func TestRevertAcceptsRequiredVersionFlag(t *testing.T) {
	database := filepath.Join(t.TempDir(), "mem.db")
	response, exitCode := runCommand(t, "--db", database, "ns", "create", "work/infra")
	if exitCode != 0 {
		t.Fatalf("namespace exit code: %d response=%#v", exitCode, response)
	}
	response, exitCode = runCommand(t, "--db", database, "add", "--namespace", "work/infra", "--subject", "versioned", "--content", "initial")
	if exitCode != 0 {
		t.Fatalf("add exit code: %d response=%#v", exitCode, response)
	}
	memoryID, _ := commandData(t, response)["id"].(string)
	response, exitCode = runCommand(t, "--db", database, "update", memoryID, "--content", "updated")
	if exitCode != 0 {
		t.Fatalf("update exit code: %d response=%#v", exitCode, response)
	}

	response, exitCode = runCommand(t, "--db", database, "revert", memoryID, "--version", "1")
	if exitCode != 0 {
		t.Fatalf("revert exit code: %d response=%#v", exitCode, response)
	}
	if reverted := commandData(t, response); reverted["content"] != "initial" {
		t.Fatalf("revert content: got %#v", reverted["content"])
	}
	response, _ = runCommand(t, "--db", database, "history", memoryID)
	history := commandData(t, response)
	versions, _ := history["versions"].([]any)
	if len(versions) != 2 {
		t.Fatalf("expected revert to add a version, got %#v", versions)
	}
}
