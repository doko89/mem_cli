package sqlite_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mem_cli/internal/app"
	"mem_cli/internal/domain"
	"mem_cli/internal/sqlite"
)

func newTestService(t *testing.T) *app.Service {
	t.Helper()
	repository, err := sqlite.Open(filepath.Join(t.TempDir(), "mem.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if err := repository.Close(); err != nil {
			t.Fatalf("close sqlite: %v", err)
		}
	})
	return app.NewService(repository.NamespaceRepository(), repository.MemoryRepository())
}

func TestNamespaceCreateIsNormalizedAndIdempotent(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()

	first, created, err := service.CreateNamespace(ctx, "Work//Infra/")
	if err != nil {
		t.Fatalf("create namespace: %v", err)
	}
	if !created || first.NormalizedName != "work/infra" || first.ParentID == "" {
		t.Fatalf("unexpected first create: created=%v namespace=%+v", created, first)
	}
	second, created, err := service.CreateNamespace(ctx, "work/infra")
	if err != nil {
		t.Fatalf("create namespace again: %v", err)
	}
	if created || second.ID != first.ID {
		t.Fatalf("unexpected idempotent create: created=%v first=%+v second=%+v", created, first, second)
	}
}

func TestSimilarNamespaceCreatesWithoutFuzzyGuard(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	first, _, err := service.CreateNamespace(ctx, "work/infra")
	if err != nil {
		t.Fatalf("seed namespace: %v", err)
	}
	second, created, err := service.CreateNamespace(ctx, "works/infra")
	if err != nil {
		t.Fatalf("create similar namespace: %v", err)
	}
	if !created || first.ID == second.ID {
		t.Fatalf("similar namespace was not separate: first=%+v second=%+v", first, second)
	}
}

func TestMemoryCRUDAndFTSUpdate(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	if _, _, err := service.CreateNamespace(ctx, "work/infra"); err != nil {
		t.Fatalf("create namespace: %v", err)
	}
	memory, err := service.AddMemory(ctx, app.AddInput{
		Namespace: "work/infra",
		Subject:   "topology",
		Type:      "fact",
		Content:   "Workers spread across availability zones.",
		Tags:      []string{"production", "kubernetes"},
		Metadata:  map[string]any{"cluster": "production"},
	})
	if err != nil {
		t.Fatalf("add memory: %v", err)
	}
	results, err := service.SearchMemories(ctx, app.SearchFilter{Query: "availability zones", NamespaceID: "work/infra"})
	if err != nil {
		t.Fatalf("search memory: %v", err)
	}
	if len(results) != 1 || results[0].ID != memory.ID {
		t.Fatalf("unexpected search result: %#v", results)
	}
	newContent := "Ingress uses nginx."
	if _, err := service.UpdateMemory(ctx, app.UpdateInput{ID: memory.ID, Content: &newContent}); err != nil {
		t.Fatalf("update memory: %v", err)
	}
	stale, err := service.SearchMemories(ctx, app.SearchFilter{Query: "availability zones", NamespaceID: "work/infra"})
	if err != nil {
		t.Fatalf("search stale content: %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("stale FTS content remained: %#v", stale)
	}
	fresh, err := service.SearchMemories(ctx, app.SearchFilter{Query: "nginx", NamespaceID: "work/infra"})
	if err != nil || len(fresh) != 1 {
		t.Fatalf("fresh search: results=%#v err=%v", fresh, err)
	}
	if err := service.ForgetMemory(ctx, memory.ID); err != nil {
		t.Fatalf("forget memory: %v", err)
	}
	if _, err := service.GetMemory(ctx, memory.ID, false); getErrorCode(err) != domain.ErrorMemoryNotFound {
		t.Fatalf("expected memory not found, got %#v", err)
	}
}

func TestSearchSupportsExplicitSuffixWildcardOnly(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	if _, _, err := service.CreateNamespace(ctx, "work/infra"); err != nil {
		t.Fatalf("create namespace: %v", err)
	}
	memory, err := service.AddMemory(ctx, app.AddInput{
		Namespace: "work/infra",
		Subject:   "topology",
		Type:      "fact",
		Content:   "Workers spread across availability zones.",
	})
	if err != nil {
		t.Fatalf("add memory: %v", err)
	}
	results, err := service.SearchMemories(ctx, app.SearchFilter{Query: "availability*", NamespaceID: "work/infra"})
	if err != nil || len(results) != 1 || results[0].ID != memory.ID {
		t.Fatalf("suffix wildcard search: results=%#v err=%v", results, err)
	}
	results, err = service.SearchMemories(ctx, app.SearchFilter{Query: "availab*", NamespaceID: "work/infra"})
	if err != nil || len(results) != 1 || results[0].ID != memory.ID {
		t.Fatalf("partial prefix wildcard search: results=%#v err=%v", results, err)
	}
	_, err = service.SearchMemories(ctx, app.SearchFilter{Query: "availab*le", NamespaceID: "work/infra"})
	var domainError *domain.Error
	if !errorAs(err, &domainError) || domainError.Code != domain.ErrorInvalidArgument || !strings.Contains(domainError.Message, `wildcard "*"`) {
		t.Fatalf("invalid wildcard: err=%#v", err)
	}
}

func TestInvalidTypeFilterExplainsValidChoices(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	if _, _, err := service.CreateNamespace(ctx, "work/infra"); err != nil {
		t.Fatalf("create namespace: %v", err)
	}
	_, err := service.ListMemories(ctx, app.MemoryFilter{NamespaceID: "work/infra", Type: "note", Limit: 10})
	var domainError *domain.Error
	if !errorAs(err, &domainError) || domainError.Code != domain.ErrorInvalidMemoryType {
		t.Fatalf("invalid list type: err=%#v", err)
	}
	for _, validType := range domain.ValidMemoryTypes() {
		if !strings.Contains(domainError.Message, validType) {
			t.Fatalf("valid type %s missing from %s", validType, domainError.Message)
		}
	}
	_, err = service.SearchMemories(ctx, app.SearchFilter{NamespaceID: "work/infra", Query: "anything", Type: "note"})
	if !errorAs(err, &domainError) || domainError.Code != domain.ErrorInvalidMemoryType {
		t.Fatalf("invalid search type: err=%#v", err)
	}
}

func TestRetrievalIncludesAllTypesByDefault(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	if _, _, err := service.CreateNamespace(ctx, "work/infra"); err != nil {
		t.Fatalf("create namespace: %v", err)
	}
	for _, memoryType := range domain.ValidMemoryTypes() {
		_, err := service.AddMemory(ctx, app.AddInput{
			Namespace: "work/infra",
			Subject:   "type-" + memoryType,
			Type:      memoryType,
			Content:   "shared retrieval content",
		})
		if err != nil {
			t.Fatalf("add %s memory: %v", memoryType, err)
		}
	}

	memories, err := service.ListMemories(ctx, app.MemoryFilter{NamespaceID: "work/infra"})
	if err != nil || len(memories) != len(domain.ValidMemoryTypes()) {
		t.Fatalf("default list retrieval: count=%d err=%v", len(memories), err)
	}
	results, err := service.SearchMemories(ctx, app.SearchFilter{NamespaceID: "work/infra", Query: "shared retrieval"})
	if err != nil || len(results) != len(domain.ValidMemoryTypes()) {
		t.Fatalf("default search retrieval: count=%d err=%v", len(results), err)
	}
}

func TestExpiredMemoriesAreHiddenFromNormalRetrieval(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	if _, _, err := service.CreateNamespace(ctx, "work/infra"); err != nil {
		t.Fatalf("create namespace: %v", err)
	}
	expiresAt := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	memory, err := service.AddMemory(ctx, app.AddInput{Namespace: "work/infra", Subject: "incident", Type: "fact", Content: "Expired Kubernetes incident.", ExpiresAt: expiresAt})
	if err != nil {
		t.Fatalf("add expired memory: %v", err)
	}
	if _, err := service.GetMemory(ctx, memory.ID, false); getErrorCode(err) != domain.ErrorMemoryNotFound {
		t.Fatalf("expired memory should be hidden from normal get, got %#v", err)
	}
	if _, err := service.GetMemory(ctx, memory.ID, true); err != nil {
		t.Fatalf("expired memory should remain available for audit: %v", err)
	}
	list, err := service.ListMemories(ctx, app.MemoryFilter{NamespaceID: "work/infra"})
	if err != nil || len(list) != 0 {
		t.Fatalf("expired memory listed: memories=%#v err=%v", list, err)
	}
	search, err := service.SearchMemories(ctx, app.SearchFilter{Query: "expired", NamespaceID: "work/infra"})
	if err != nil || len(search) != 0 {
		t.Fatalf("expired memory searched: results=%#v err=%v", search, err)
	}
}

func TestHierarchicalNamespaceQueriesIncludeSubtree(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	if _, _, err := service.CreateNamespace(ctx, "proj/kriskris"); err != nil {
		t.Fatalf("create parent namespace: %v", err)
	}
	if _, _, err := service.CreateNamespace(ctx, "proj/kriskris/api"); err != nil {
		t.Fatalf("create child namespace: %v", err)
	}
	parentMemory, err := service.AddMemory(ctx, app.AddInput{
		Namespace: "proj/kriskris",
		Subject:   "payments",
		Type:      "fact",
		Content:   "Xendit handles payment callbacks.",
	})
	if err != nil {
		t.Fatalf("add parent memory: %v", err)
	}
	childMemory, err := service.AddMemory(ctx, app.AddInput{
		Namespace: "proj/kriskris/api",
		Subject:   "quality",
		Type:      "fact",
		Content:   "API coverage is enforced at 90 percent.",
	})
	if err != nil {
		t.Fatalf("add child memory: %v", err)
	}

	list, err := service.ListMemories(ctx, app.MemoryFilter{NamespaceID: "proj/kriskris", Subtree: true, Limit: 10})
	if err != nil {
		t.Fatalf("hierarchical list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected parent and child memories, got %#v", list)
	}

	search, err := service.SearchMemories(ctx, app.SearchFilter{Query: "coverage", NamespaceID: "proj/kriskris", Subtree: true})
	if err != nil {
		t.Fatalf("hierarchical search: %v", err)
	}
	if len(search) != 1 || search[0].ID != childMemory.ID {
		t.Fatalf("expected child search hit, got %#v", search)
	}
	strict, err := service.SearchMemories(ctx, app.SearchFilter{Query: "xendit coverage", NamespaceID: "proj/kriskris", Subtree: true, MatchMode: "all"})
	if err != nil {
		t.Fatalf("strict search: %v", err)
	}
	if len(strict) != 0 {
		t.Fatalf("expected strict natural-language query to miss, got %#v", strict)
	}
	relaxed, err := service.SearchMemories(ctx, app.SearchFilter{Query: "bagaimana coverage?", NamespaceID: "proj/kriskris", Subtree: true, MatchMode: "any"})
	if err != nil {
		t.Fatalf("relaxed search: %v", err)
	}
	if len(relaxed) != 1 || relaxed[0].ID != childMemory.ID {
		t.Fatalf("expected relaxed query to find child hit, got %#v", relaxed)
	}

	export, err := service.ExportMemories(ctx, "proj/kriskris", true)
	if err != nil {
		t.Fatalf("hierarchical export: %v", err)
	}
	if len(export) != 2 {
		t.Fatalf("expected export to include subtree, got %#v", export)
	}

	exact, err := service.ListMemories(ctx, app.MemoryFilter{NamespaceID: "proj/kriskris", Subtree: false, Limit: 10})
	if err != nil {
		t.Fatalf("exact list: %v", err)
	}
	if len(exact) != 1 || exact[0].ID != parentMemory.ID {
		t.Fatalf("expected exact parent-only list, got %#v", exact)
	}
}

func TestNamespaceDeleteRequiresExplicitRecursion(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	if _, _, err := service.CreateNamespace(ctx, "work/infra/kubernetes"); err != nil {
		t.Fatalf("create namespace hierarchy: %v", err)
	}
	if err := service.DeleteNamespace(ctx, "work", false); getErrorCode(err) != domain.ErrorNamespaceConflict {
		t.Fatalf("expected conflict without recursive, got %#v", err)
	}
	if err := service.DeleteNamespace(ctx, "work", true); err != nil {
		t.Fatalf("recursive delete: %v", err)
	}
	namespaces, err := service.ListNamespaces(ctx, "")
	if err != nil || len(namespaces) != 0 {
		t.Fatalf("namespace hierarchy remained: namespaces=%#v err=%v", namespaces, err)
	}
}

func getErrorCode(err error) string {
	var domainError *domain.Error
	if errorAs(err, &domainError) {
		return domainError.Code
	}
	return ""
}
