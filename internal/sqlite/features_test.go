package sqlite_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"mem_cli/internal/app"
	"mem_cli/internal/domain"
	"mem_cli/internal/sqlite"
)

func addTestMemory(t *testing.T, service *app.Service, subject, content string, input app.AddInput) domain.Memory {
	t.Helper()
	if input.Namespace == "" {
		input.Namespace = "work/infra"
	}
	input.Subject = subject
	input.Content = content
	if input.Type == "" {
		input.Type = "fact"
	}
	memory, err := service.AddMemory(context.Background(), input)
	if err != nil {
		t.Fatalf("add memory %s: %v", subject, err)
	}
	return memory
}

func TestMemoryOutputsResolveCurrentNamespacePath(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "mem.db")
	repository, err := sqlite.Open(databasePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if err := repository.Close(); err != nil {
			t.Fatalf("close sqlite: %v", err)
		}
	})
	service := app.NewService(repository.NamespaceRepository(), repository.MemoryRepository())
	ctx := context.Background()
	namespace, _, err := service.CreateNamespace(ctx, "a/b/c")
	if err != nil {
		t.Fatalf("create nested namespace: %v", err)
	}
	memory, err := service.AddMemory(ctx, app.AddInput{
		Namespace: "a/b/c",
		Subject:   "owner",
		Type:      "fact",
		Content:   "namespace owner content",
	})
	if err != nil {
		t.Fatalf("add memory: %v", err)
	}
	if memory.Namespace == nil || *memory.Namespace != "a/b/c" {
		t.Fatalf("add namespace path: %#v", memory.Namespace)
	}
	target, err := service.AddMemory(ctx, app.AddInput{
		Namespace: "a/b/c",
		Subject:   "target",
		Type:      "fact",
		Content:   "linked target content",
	})
	if err != nil {
		t.Fatalf("add linked memory: %v", err)
	}
	updated, err := service.UpdateMemory(ctx, app.UpdateInput{
		ID: memory.ID, RelatedIDs: []string{target.ID}, RelatedSet: true,
	})
	if err != nil {
		t.Fatalf("update memory: %v", err)
	}
	if updated.Namespace == nil || *updated.Namespace != "a/b/c" {
		t.Fatalf("update namespace path: %#v", updated.Namespace)
	}

	details, err := service.GetMemory(ctx, memory.ID, false)
	if err != nil {
		t.Fatalf("get memory: %v", err)
	}
	if details.Namespace == nil || *details.Namespace != "a/b/c" || details.NamespaceID != namespace.ID {
		t.Fatalf("get namespace resolution: id=%q path=%#v", details.NamespaceID, details.Namespace)
	}
	if len(details.RelatedOut) != 1 || details.RelatedOut[0].Memory == nil || details.RelatedOut[0].Memory.Namespace == nil {
		t.Fatalf("linked namespace resolution: %#v", details.RelatedOut)
	}
	list, err := service.ListMemories(ctx, app.MemoryFilter{NamespaceID: "a/b/c"})
	if err != nil || len(list) != 2 {
		t.Fatalf("list memories: count=%d err=%v", len(list), err)
	}
	for _, item := range list {
		if item.Namespace == nil || *item.Namespace != "a/b/c" || item.NamespaceID == "" {
			t.Fatalf("list namespace resolution: %#v", item)
		}
	}
	search, err := service.SearchMemories(ctx, app.SearchFilter{NamespaceID: "a/b/c", Query: "namespace owner"})
	if err != nil || len(search) != 1 {
		t.Fatalf("search memories: results=%#v err=%v", search, err)
	}
	if search[0].NamespaceID != namespace.ID || search[0].Namespace == nil || *search[0].Namespace != "a/b/c" {
		t.Fatalf("search namespace resolution: %#v", search[0])
	}
	exported, err := service.ExportMemories(ctx, "a/b/c", false)
	if err != nil || len(exported) != 2 {
		t.Fatalf("export memories: count=%d err=%v", len(exported), err)
	}
	for _, item := range exported {
		if item.Namespace == nil || *item.Namespace != "a/b/c" || item.NamespaceID == "" {
			t.Fatalf("export namespace resolution: %#v", item)
		}
	}
	reverted, err := service.RevertMemory(ctx, memory.ID, 1)
	if err != nil {
		t.Fatalf("revert memory: %v", err)
	}
	if reverted.Namespace == nil || *reverted.Namespace != "a/b/c" {
		t.Fatalf("revert namespace path: %#v", reverted.Namespace)
	}

	raw, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatalf("open raw database: %v", err)
	}
	if _, err := raw.Exec(`UPDATE namespaces SET name = 'a/b/renamed', normalized_name = 'a/b/renamed' WHERE id = ?`, namespace.ID); err != nil {
		t.Fatalf("rename namespace: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw database: %v", err)
	}
	renamed, err := service.GetMemory(ctx, memory.ID, false)
	if err != nil {
		t.Fatalf("get renamed memory: %v", err)
	}
	if renamed.Namespace == nil || *renamed.Namespace != "a/b/renamed" {
		t.Fatalf("renamed namespace path: %#v", renamed.Namespace)
	}

	orphan, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatalf("open orphan database: %v", err)
	}
	if _, err := orphan.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatalf("disable foreign keys: %v", err)
	}
	if _, err := orphan.Exec(`INSERT INTO memories(id, namespace_id, subject, type, content, created_at, updated_at)
		VALUES ('mem_orphan', 'ns_orphan', 'orphan', 'fact', 'orphan content', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert orphan memory: %v", err)
	}
	if err := orphan.Close(); err != nil {
		t.Fatalf("close orphan database: %v", err)
	}
	orphanMemory, err := service.GetMemory(ctx, "mem_orphan", true)
	if err != nil {
		t.Fatalf("get orphan memory: %v", err)
	}
	if orphanMemory.NamespaceID != "ns_orphan" || orphanMemory.Namespace != nil {
		t.Fatalf("orphan namespace resolution: id=%q path=%#v", orphanMemory.NamespaceID, orphanMemory.Namespace)
	}
}

func TestMemoryLinksBacklinksAndDeletion(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	if _, _, err := service.CreateNamespace(ctx, "work/infra"); err != nil {
		t.Fatalf("create namespace: %v", err)
	}
	source := addTestMemory(t, service, "source", "Source memory.", app.AddInput{})
	target := addTestMemory(t, service, "target", "Target memory.", app.AddInput{RelatedIDs: []string{"mem_"}, Relation: "related"})
	if target.ID == source.ID {
		t.Fatalf("unexpected identical test ids")
	}

	sourceDetails, err := service.GetMemory(ctx, source.ID, false)
	if err != nil {
		t.Fatalf("get source details: %v", err)
	}
	if len(sourceDetails.Backlinks) != 1 || sourceDetails.Backlinks[0].FromID != target.ID || sourceDetails.Backlinks[0].Direction != "back" {
		t.Fatalf("unexpected source backlinks: %#v", sourceDetails.Backlinks)
	}
	if sourceDetails.Backlinks[0].Memory == nil || sourceDetails.Backlinks[0].Memory.Subject != "target" {
		t.Fatalf("backlink target subject was not loaded: %#v", sourceDetails.Backlinks[0])
	}

	_, err = service.ListMemories(ctx, app.MemoryFilter{NamespaceID: "work/infra", RelatedTo: "mem_"})
	var domainError *domain.Error
	if !errorAs(err, &domainError) || domainError.Code != domain.ErrorAmbiguousID {
		t.Fatalf("expected ambiguous id, got %#v", err)
	}
	connected, err := service.ListMemories(ctx, app.MemoryFilter{NamespaceID: "work/infra", RelatedTo: source.ID})
	if err != nil || len(connected) != 1 || connected[0].ID != target.ID {
		t.Fatalf("connected list: memories=%#v err=%v", connected, err)
	}

	missing := addTestMemory(t, service, "missing", "Missing relation target.", app.AddInput{})
	_, err = service.AddMemory(ctx, app.AddInput{Namespace: "work/infra", Subject: "invalid", Type: "fact", Content: "Invalid relation.", RelatedIDs: []string{missing.ID + "missing"}})
	if !errorAs(err, &domainError) || domainError.Code != domain.ErrorInvalidArgument || !strings.Contains(err.Error(), missing.ID+"missing") {
		t.Fatalf("expected invalid related id, got %#v", err)
	}

	if err := service.ForgetMemory(ctx, target.ID); err != nil {
		t.Fatalf("forget target: %v", err)
	}
	sourceDetails, err = service.GetMemory(ctx, source.ID, false)
	if err != nil || len(sourceDetails.Backlinks) != 0 {
		t.Fatalf("backlink survived forget: details=%#v err=%v", sourceDetails, err)
	}
}

func TestMemoryVersionHistoryAndRevert(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	if _, _, err := service.CreateNamespace(ctx, "work/infra"); err != nil {
		t.Fatalf("create namespace: %v", err)
	}
	memory := addTestMemory(t, service, "playbook", "initial content\nwith newline", app.AddInput{})
	first := "first update"
	second := "second update"
	if _, err := service.UpdateMemory(ctx, app.UpdateInput{ID: memory.ID, Content: &first}); err != nil {
		t.Fatalf("first update: %v", err)
	}
	if _, err := service.UpdateMemory(ctx, app.UpdateInput{ID: memory.ID, Content: &second}); err != nil {
		t.Fatalf("second update: %v", err)
	}

	history, err := service.MemoryHistory(ctx, memory.ID)
	if err != nil || len(history) != 2 {
		t.Fatalf("history: versions=%#v err=%v", history, err)
	}
	if history[0].Version != 1 || history[0].ContentSizeBytes != len(memory.Content) || history[0].ContentSizeAfter != len(first) {
		t.Fatalf("unexpected first history entry: %#v", history[0])
	}
	if history[1].Version != 2 || history[1].ContentSizeBytes != len(first) || history[1].ContentSizeAfter != len(second) {
		t.Fatalf("unexpected second history entry: %#v", history[1])
	}
	snapshot, err := service.MemoryVersion(ctx, memory.ID, 1)
	if err != nil || snapshot.Memory.Content != memory.Content {
		t.Fatalf("first snapshot: snapshot=%#v err=%v", snapshot, err)
	}

	reverted, err := service.RevertMemory(ctx, memory.ID, 1)
	if err != nil || reverted.Content != memory.Content {
		t.Fatalf("revert: memory=%#v err=%v", reverted, err)
	}
	history, err = service.MemoryHistory(ctx, memory.ID)
	if err != nil || len(history) != 3 || history[2].Version != 3 {
		t.Fatalf("history after revert: versions=%#v err=%v", history, err)
	}
	if history[2].ContentSizeBytes != len(second) || history[2].ContentSizeAfter != len(memory.Content) {
		t.Fatalf("unexpected revert history entry: %#v", history[2])
	}
}

func TestRelatedTargetsResolveBySubjectWithIDPriority(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	if _, _, err := service.CreateNamespace(ctx, "work/infra"); err != nil {
		t.Fatalf("create namespace: %v", err)
	}
	base := addTestMemory(t, service, "base", "base target", app.AddInput{})
	addTestMemory(t, service, base.ID, "subject with an id as its name", app.AddInput{})
	addTestMemory(t, service, base.ID[:8], "subject with an id prefix as its name", app.AddInput{})
	unique := addTestMemory(t, service, "unique-target", "unique target", app.AddInput{})

	source := addTestMemory(t, service, "by-subject", "by unique subject", app.AddInput{RelatedIDs: []string{"unique-target"}})
	details, err := service.GetMemory(ctx, source.ID, false)
	if err != nil || len(details.RelatedOut) != 1 || details.RelatedOut[0].ToID != unique.ID {
		t.Fatalf("subject resolution: details=%#v err=%v", details, err)
	}

	source = addTestMemory(t, service, "by-id", "exact id wins over subject", app.AddInput{RelatedIDs: []string{base.ID}})
	details, err = service.GetMemory(ctx, source.ID, false)
	if err != nil || len(details.RelatedOut) != 1 || details.RelatedOut[0].ToID != base.ID {
		t.Fatalf("exact id priority: details=%#v err=%v", details, err)
	}

	source = addTestMemory(t, service, "by-prefix", "unique prefix wins over subject", app.AddInput{RelatedIDs: []string{base.ID[:8]}})
	details, err = service.GetMemory(ctx, source.ID, false)
	if err != nil || len(details.RelatedOut) != 1 || details.RelatedOut[0].ToID != base.ID {
		t.Fatalf("prefix id priority: details=%#v err=%v", details, err)
	}

	updateSource := addTestMemory(t, service, "before-update", "before subject resolution", app.AddInput{})
	updated, err := service.UpdateMemory(ctx, app.UpdateInput{ID: updateSource.ID, RelatedIDs: []string{"unique-target"}, RelatedSet: true})
	if err != nil {
		t.Fatalf("update by subject: %v", err)
	}
	details, err = service.GetMemory(ctx, updated.ID, false)
	if err != nil || len(details.RelatedOut) != 1 || details.RelatedOut[0].ToID != unique.ID {
		t.Fatalf("update subject resolution: details=%#v err=%v", details, err)
	}

	addTestMemory(t, service, "duplicate", "duplicate one", app.AddInput{})
	addTestMemory(t, service, "duplicate", "duplicate two", app.AddInput{})
	_, err = service.AddMemory(ctx, app.AddInput{Namespace: "work/infra", Subject: "ambiguous-source", Type: "fact", Content: "ambiguous source", RelatedIDs: []string{"duplicate"}})
	var domainError *domain.Error
	if !errorAs(err, &domainError) || domainError.Code != domain.ErrorAmbiguousSubject || len(domainError.SubjectCandidates) != 2 {
		t.Fatalf("ambiguous subject: err=%#v", err)
	}
	for _, candidate := range domainError.SubjectCandidates {
		if candidate.Namespace != "work/infra" || candidate.Subject != "duplicate" || candidate.ID == "" {
			t.Fatalf("ambiguous subject candidate: %#v", candidate)
		}
	}

	_, err = service.AddMemory(ctx, app.AddInput{Namespace: "work/infra", Subject: "missing-source", Type: "fact", Content: "missing source", RelatedIDs: []string{"missing"}})
	if !errorAs(err, &domainError) || domainError.Code != domain.ErrorInvalidArgument || domainError.Message != `Related identifier "missing" not found as id or subject in namespace "work/infra".` {
		t.Fatalf("missing related subject: err=%#v", err)
	}
}

func TestSchemaVersionOneMigratesWithBackupAndPreservesData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mem.db")
	repository, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("create new database: %v", err)
	}
	if err := repository.Close(); err != nil {
		t.Fatalf("close new database: %v", err)
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open old database: %v", err)
	}
	defer database.Close()
	if _, err := database.Exec(`DROP TABLE memory_links; DROP TABLE memory_versions; PRAGMA user_version = 1;`); err != nil {
		t.Fatalf("downgrade database schema: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO namespaces(id, name, normalized_name, parent_id, description, created_at)
		VALUES ('ns_old', 'work/infra', 'work/infra', NULL, NULL, '2026-01-01T00:00:00.000Z')`); err != nil {
		t.Fatalf("seed old namespace: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO memories(id, namespace_id, subject, type, content, reason, tags, metadata, source, created_at, updated_at, expires_at)
		VALUES ('mem_old', 'ns_old', 'topology', 'fact', 'old content', NULL, NULL, NULL, NULL, '2026-01-01T00:00:00.000Z', '2026-01-01T00:00:00.000Z', NULL)`); err != nil {
		t.Fatalf("seed old memory: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close old database: %v", err)
	}

	migrated, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	if err := migrated.Close(); err != nil {
		t.Fatalf("close migrated database: %v", err)
	}
	database, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	var userVersion int
	if err := database.QueryRow(`PRAGMA user_version`).Scan(&userVersion); err != nil || userVersion != 2 {
		_ = migrated.Close()
		t.Fatalf("schema version: version=%d err=%v", userVersion, err)
	}
	var subject, content string
	if err := database.QueryRow(`SELECT subject, content FROM memories WHERE id = 'mem_old'`).Scan(&subject, &content); err != nil || subject != "topology" || content != "old content" {
		t.Fatalf("old data: subject=%q content=%q err=%v", subject, content, err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close verification database: %v", err)
	}
	backupMatches, err := filepath.Glob(path + ".bak-*")
	if err != nil || len(backupMatches) != 1 {
		t.Fatalf("pre-migration backup: files=%#v err=%v", backupMatches, err)
	}

	reopened, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("reopen migrated database: %v", err)
	}
	defer reopened.Close()
	backupMatches, err = filepath.Glob(path + ".bak-*")
	if err != nil || len(backupMatches) != 1 {
		t.Fatalf("migration was not idempotent: files=%#v err=%v", backupMatches, err)
	}
}
