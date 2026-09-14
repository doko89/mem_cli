// Package mcpserver exposes the memory engine as MCP tools.
//
// This package is a pure adapter: every tool delegates to *app.Service and
// contains no business logic of its own. The same *mcp.Server can be served
// over stdio or HTTP transports (see the transport package).
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mem_cli/internal/app"
	"mem_cli/internal/domain"
)

const serverVersion = "0.1.0"

// textResult builds a CallToolResult with a single text payload plus the
// structured content so both text-only and structured-aware clients work.
func textResult(v any) (*mcp.CallToolResult, any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}},
	}, v, nil
}

// toolError converts domain errors into clean, non-leaking tool errors.
func toolError(err error) error {
	var domainErr *domain.Error
	if errorsAs(err, &domainErr) {
		return fmt.Errorf("%s: %s", domainErr.Code, domainErr.Message)
	}
	return fmt.Errorf("internal error")
}

// errorsAs is a tiny wrapper so the import list stays tidy.
func errorsAs(err error, target **domain.Error) bool {
	for err != nil {
		if e, ok := err.(*domain.Error); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func validType(memoryType string) (string, error) {
	switch strings.TrimSpace(memoryType) {
	case "":
		return string(domain.TypeFact), nil
	case string(domain.TypeFact), string(domain.TypeDoc), string(domain.TypeDecision),
		string(domain.TypePreference), string(domain.TypeTodo), string(domain.TypeEntity):
		return memoryType, nil
	default:
		return "", fmt.Errorf("invalid_argument: type must be one of fact, doc, decision, preference, todo, entity")
	}
}

// --- input types -----------------------------------------------------------

type addArgs struct {
	Namespace  string         `json:"namespace" jsonschema:"namespace path, e.g. nova/projects/mem_cli (required)"`
	Subject    string         `json:"subject" jsonschema:"short subject label for the memory (required)"`
	Content    string         `json:"content" jsonschema:"full memory content, stored verbatim (required)"`
	Type       string         `json:"type,omitempty" jsonschema:"one of: fact, doc, decision, preference, todo, entity (default fact)"`
	Reason     string         `json:"reason,omitempty" jsonschema:"why this memory exists"`
	Tags       []string       `json:"tags,omitempty" jsonschema:"optional tags"`
	Metadata   map[string]any `json:"metadata,omitempty" jsonschema:"optional JSON metadata"`
	ExpiresAt  string         `json:"expires_at,omitempty" jsonschema:"optional expiry, RFC3339"`
	RelatedIDs []string       `json:"related_ids,omitempty" jsonschema:"IDs of memories to link"`
	Relation   string         `json:"relation,omitempty" jsonschema:"relation label for links (default related)"`
}

type getArgs struct {
	ID             string `json:"id" jsonschema:"memory ID (mem_...) (required)"`
	IncludeExpired bool   `json:"include_expired,omitempty" jsonschema:"also match expired memories"`
}

type exportArgs struct {
	Namespace string `json:"namespace" jsonschema:"filter by namespace path"`
	Subtree   bool   `json:"subtree,omitempty" jsonschema:"include descendant namespaces (default false)"`
}

type namespaceListArgs struct {
	Prefix string `json:"prefix,omitempty" jsonschema:"filter by namespace path prefix"`
}

type searchArgs struct {
	Namespace string `json:"namespace" jsonschema:"namespace path to search in (required)"`
	Query     string `json:"query" jsonschema:"FTS5 query, keywords or quoted phrases (required)"`
	Subject   string `json:"subject,omitempty" jsonschema:"filter by subject"`
	Type      string `json:"type,omitempty" jsonschema:"filter by memory type"`
	Tag       string `json:"tag,omitempty" jsonschema:"filter by tag"`
	Subtree   bool   `json:"subtree,omitempty" jsonschema:"include descendant namespaces (default false)"`
	MatchMode string `json:"match_mode,omitempty" jsonschema:"term matching: all (default) or any"`
	Limit     int    `json:"limit,omitempty" jsonschema:"max results, 1-1000 (default 10)"`
}

type listArgs struct {
	Namespace string `json:"namespace" jsonschema:"namespace path (required)"`
	Subject   string `json:"subject,omitempty" jsonschema:"filter by subject"`
	Type      string `json:"type,omitempty" jsonschema:"filter by memory type"`
	Tag       string `json:"tag,omitempty" jsonschema:"filter by tag"`
	Subtree   bool   `json:"subtree,omitempty" jsonschema:"include descendant namespaces (default false)"`
	RelatedTo string `json:"related_to,omitempty" jsonschema:"only memories linked to this memory id"`
	Limit     int    `json:"limit,omitempty" jsonschema:"max memories (default 100)"`
}

type updateArgs struct {
	ID             string         `json:"id" jsonschema:"memory ID (required)"`
	Subject        string         `json:"subject,omitempty" jsonschema:"new subject (empty = unchanged)"`
	Type           string         `json:"type,omitempty" jsonschema:"new memory type (empty = unchanged)"`
	Content        string         `json:"content,omitempty" jsonschema:"new content (empty = unchanged)"`
	Reason         string         `json:"reason,omitempty" jsonschema:"new reason (empty = unchanged)"`
	Tags           []string       `json:"tags,omitempty" jsonschema:"replace tags when replace_tags is true"`
	ReplaceTags    bool           `json:"replace_tags,omitempty" jsonschema:"replace tags with the provided list"`
	Metadata       map[string]any `json:"metadata,omitempty" jsonschema:"merge metadata"`
	MetadataSet    bool           `json:"metadata_set,omitempty" jsonschema:"apply the metadata object"`
	ExpiresAt      string         `json:"expires_at,omitempty" jsonschema:"new expiry RFC3339 (empty = unchanged)"`
	ClearExpiry    bool           `json:"clear_expiry,omitempty" jsonschema:"remove the expiry"`
	RelatedIDs     []string       `json:"related_ids,omitempty" jsonschema:"replace related memory IDs when replace_related is true"`
	ReplaceRelated bool           `json:"replace_related,omitempty" jsonschema:"replace links with related_ids"`
	Relation       string         `json:"relation,omitempty" jsonschema:"relation label when replacing links"`
}

type forgetArgs struct {
	ID string `json:"id" jsonschema:"memory ID (mem_...) to delete permanently"`
}

type importArgs struct {
	Namespace  string         `json:"namespace" jsonschema:"namespace path (required)"`
	Filename   string         `json:"filename,omitempty" jsonschema:"original file name, used for the default subject and provenance"`
	Subject    string         `json:"subject,omitempty" jsonschema:"subject override (default: filename without extension)"`
	Content    string         `json:"content" jsonschema:"full document content sent inline (required)"`
	Type       string         `json:"type,omitempty" jsonschema:"memory type (default doc)"`
	Tags       []string       `json:"tags,omitempty" jsonschema:"optional tags"`
	Metadata   map[string]any `json:"metadata,omitempty" jsonschema:"optional JSON metadata"`
	ExpiresAt  string         `json:"expires_at,omitempty" jsonschema:"optional expiry, RFC3339"`
	RelatedIDs []string       `json:"related_ids,omitempty" jsonschema:"IDs of memories to link"`
	Relation   string         `json:"relation,omitempty" jsonschema:"relation label for links (default related)"`
}

type historyArgs struct {
	ID      string `json:"id" jsonschema:"memory ID (required)"`
	Version int64  `json:"version,omitempty" jsonschema:"show one full version instead of the summary list"`
}

type revertArgs struct {
	ID      string `json:"id" jsonschema:"memory ID (required)"`
	Version int64  `json:"version" jsonschema:"version number to restore (required)"`
}

type contextArgs struct {
	Namespace string `json:"namespace" jsonschema:"namespace path (required)"`
	Query     string `json:"query" jsonschema:"natural-language or keyword query (required)"`
	Subject   string `json:"subject,omitempty" jsonschema:"filter by subject"`
	Type      string `json:"type,omitempty" jsonschema:"filter by memory type"`
	Tag       string `json:"tag,omitempty" jsonschema:"filter by tag"`
	Subtree   bool   `json:"subtree,omitempty" jsonschema:"include descendant namespaces"`
	Limit     int    `json:"limit,omitempty" jsonschema:"max context items (default 10)"`
}

// --- server assembly -------------------------------------------------------

// New creates an MCP server exposing every memory-engine operation.
func New(databasePath string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "mem_cli",
		Title:   "mem — local-first memory for AI agents",
		Version: serverVersion,
	}, nil)

	registerTools(server, databasePath)
	return server
}

// addTool registers one typed tool handler on the server.
func addTool[In any](server *mcp.Server, name, description string, fn mcp.ToolHandlerFor[In, any]) {
	mcp.AddTool(server, &mcp.Tool{Name: name, Description: description}, fn)
}

func registerTools(server *mcp.Server, databasePath string) {
	// The handler closures open a fresh repository/service per call so both
	// stdio (one long-lived process) and HTTP (concurrent requests) share the
	// same safe, WAL-backed database semantics as the CLI.
	open := func(ctx context.Context) (*app.Service, func(), error) {
		repository, err := openRepository(ctx, databasePath)
		if err != nil {
			return nil, nil, err
		}
		service := app.NewService(repository.NamespaceRepository(), repository.MemoryRepository())
		return service, func() { repository.Close() }, nil
	}

	// ensureNamespace applies the PRD's mkdir -p semantics for MCP writers so
	// agents never need a separate bootstrap step. Creation is idempotent.
	ensureNamespace := func(ctx context.Context, service *app.Service, path string) error {
		_, _, err := service.CreateNamespace(ctx, path)
		return err
	}

	addTool(server, "memory_add", "Store a memory in a namespace. Use for facts, decisions, preferences, todos, entities, or docs. The namespace is created automatically if it does not exist yet (mkdir -p semantics).", func(ctx context.Context, _ *mcp.CallToolRequest, in addArgs) (*mcp.CallToolResult, any, error) {
		memoryType, err := validType(in.Type)
		if err != nil {
			return nil, nil, err
		}
		service, done, err := open(ctx)
		if err != nil {
			return nil, nil, toolError(err)
		}
		defer done()
		if err := ensureNamespace(ctx, service, in.Namespace); err != nil {
			return nil, nil, toolError(err)
		}
		memory, err := service.AddMemory(ctx, app.AddInput{
			Namespace:  in.Namespace,
			Subject:    in.Subject,
			Type:       memoryType,
			Content:    in.Content,
			Reason:     in.Reason,
			Tags:       in.Tags,
			Metadata:   in.Metadata,
			ExpiresAt:  in.ExpiresAt,
			RelatedIDs: in.RelatedIDs,
			Relation:   in.Relation,
		})
		if err != nil {
			return nil, nil, toolError(err)
		}
		return textResult(memory)
	})

	addTool(server, "memory_forget", "Remove a memory by ID. Use to delete outdated facts or sensitive data. Returns whether the memory was found and removed.", func(ctx context.Context, _ *mcp.CallToolRequest, in forgetArgs) (*mcp.CallToolResult, any, error) {
		service, done, err := open(ctx)
		if err != nil {
			return nil, nil, toolError(err)
		}
		defer done()
		if err := service.ForgetMemory(ctx, in.ID); err != nil {
			return nil, nil, toolError(err)
		}
		return textResult(map[string]any{"ok": true, "id": in.ID, "deleted": true})
	})

	addTool(server, "memory_export", "Export visible memories as a JSON array for portability. Supports filtering by namespace and time range.", func(ctx context.Context, _ *mcp.CallToolRequest, in exportArgs) (*mcp.CallToolResult, any, error) {
		service, done, err := open(ctx)
		if err != nil {
			return nil, nil, toolError(err)
		}
		defer done()
		memories, err := service.ExportMemories(ctx, in.Namespace, in.Subtree)
		if err != nil {
			return nil, nil, toolError(err)
		}
		return textResult(map[string]any{"count": len(memories), "memories": memories})
	})

	addTool(server, "memory_namespace_list", "List namespaces, optionally filtered by a path prefix.", func(ctx context.Context, _ *mcp.CallToolRequest, in namespaceListArgs) (*mcp.CallToolResult, any, error) {
		service, done, err := open(ctx)
		if err != nil {
			return nil, nil, toolError(err)
		}
		defer done()
		namespaces, err := service.ListNamespaces(ctx, in.Prefix)
		if err != nil {
			return nil, nil, toolError(err)
		}
		return textResult(map[string]any{"count": len(namespaces), "namespaces": namespaces})
	})

	addTool(server, "memory_get", "Get one full memory by ID, including links and backlinks.", func(ctx context.Context, _ *mcp.CallToolRequest, in getArgs) (*mcp.CallToolResult, any, error) {
		service, done, err := open(ctx)
		if err != nil {
			return nil, nil, toolError(err)
		}
		defer done()
		details, err := service.GetMemory(ctx, in.ID, in.IncludeExpired)
		if err != nil {
			return nil, nil, toolError(err)
		}
		return textResult(details)
	})

	addTool(server, "memory_search", "Full-text search (FTS5) with dynamic snippets. Use this to recall memories.", func(ctx context.Context, _ *mcp.CallToolRequest, in searchArgs) (*mcp.CallToolResult, any, error) {
		service, done, err := open(ctx)
		if err != nil {
			return nil, nil, toolError(err)
		}
		defer done()
		results, err := service.SearchMemories(ctx, app.SearchFilter{
			Query:       in.Query,
			NamespaceID: in.Namespace,
			Subject:     in.Subject,
			Type:        domain.MemoryType(in.Type),
			Tag:         in.Tag,
			Subtree:     in.Subtree,
			MatchMode:   in.MatchMode,
			Limit:       in.Limit,
		})
		if err != nil {
			return nil, nil, toolError(err)
		}
		return textResult(map[string]any{"memories": results})
	})

	addTool(server, "memory_list", "List memories in a namespace (summaries, content truncated).", func(ctx context.Context, _ *mcp.CallToolRequest, in listArgs) (*mcp.CallToolResult, any, error) {
		service, done, err := open(ctx)
		if err != nil {
			return nil, nil, toolError(err)
		}
		defer done()
		list, err := service.ListMemories(ctx, app.MemoryFilter{
			NamespaceID: in.Namespace,
			Subject:     in.Subject,
			Type:        domain.MemoryType(in.Type),
			Tag:         in.Tag,
			Subtree:     in.Subtree,
			RelatedTo:   in.RelatedTo,
			Limit:       in.Limit,
		})
		if err != nil {
			return nil, nil, toolError(err)
		}
		return textResult(map[string]any{"memories": list})
	})

	addTool(server, "memory_context", "Compact, ranked context for an agent: search + snippets in one call.", func(ctx context.Context, _ *mcp.CallToolRequest, in contextArgs) (*mcp.CallToolResult, any, error) {
		service, done, err := open(ctx)
		if err != nil {
			return nil, nil, toolError(err)
		}
		defer done()
		results, err := service.SearchMemories(ctx, app.SearchFilter{
			Query:       in.Query,
			NamespaceID: in.Namespace,
			Subject:     in.Subject,
			Type:        domain.MemoryType(in.Type),
			Tag:         in.Tag,
			Subtree:     in.Subtree,
			MatchMode:   "any",
			Limit:       in.Limit,
		})
		if err != nil {
			return nil, nil, toolError(err)
		}
		return textResult(map[string]any{"memories": results})
	})

	addTool(server, "memory_update", "Update mutable fields of a memory. Creates a new version automatically.", func(ctx context.Context, _ *mcp.CallToolRequest, in updateArgs) (*mcp.CallToolResult, any, error) {
		if _, err := validType(in.Type); in.Type != "" && err != nil {
			return nil, nil, err
		}
		service, done, err := open(ctx)
		if err != nil {
			return nil, nil, toolError(err)
		}
		defer done()
		// Merge semantics: when the client sends `metadata` without
		// `metadata_set`, merge the new keys into the existing metadata
		// (matching the tool schema description "merge metadata").
		if in.Metadata != nil && !in.MetadataSet {
			existing, err := service.GetMemory(ctx, in.ID, false)
			if err != nil {
				return nil, nil, toolError(err)
			}
			merged := make(map[string]any, len(existing.Metadata)+len(in.Metadata))
			for k, v := range existing.Metadata {
				merged[k] = v
			}
			for k, v := range in.Metadata {
				merged[k] = v
			}
			in.Metadata = merged
			in.MetadataSet = true
		}
		input := app.UpdateInput{
			ID:          in.ID,
			Subject:     optionalString(in.Subject),
			Type:        optionalString(in.Type),
			Content:     optionalString(in.Content),
			Reason:      optionalString(in.Reason),
			Tags:        in.Tags,
			TagsSet:     in.ReplaceTags,
			Metadata:    in.Metadata,
			MetadataSet: in.MetadataSet,
			ExpiresAt:   optionalString(in.ExpiresAt),
			ClearExpiry: in.ClearExpiry,
			RelatedIDs:  in.RelatedIDs,
			RelatedSet:  in.ReplaceRelated,
			Relation:    in.Relation,
		}
		memory, err := service.UpdateMemory(ctx, input)
		if err != nil {
			return nil, nil, toolError(err)
		}
		return textResult(memory)
	})

	addTool(server, "memory_forget", "Delete one memory permanently by ID (irreversible; versions are removed too). Prefer memory_update when you only want to change fields.", func(ctx context.Context, _ *mcp.CallToolRequest, in forgetArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(in.ID) == "" {
			return nil, nil, fmt.Errorf("invalid_argument: id is required")
		}
		service, done, err := open(ctx)
		if err != nil {
			return nil, nil, toolError(err)
		}
		defer done()
		if err := service.ForgetMemory(ctx, in.ID); err != nil {
			return nil, nil, toolError(err)
		}
		return textResult(map[string]any{"ok": true, "id": in.ID, "deleted": true})
	})

	addTool(server, "memory_import", "Import one document as one memory. Send the full content inline; do NOT pass a file path. The namespace is created automatically if it does not exist yet (mkdir -p semantics). The default subject is the filename without extension.", func(ctx context.Context, _ *mcp.CallToolRequest, in importArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(in.Content) == "" {
			return nil, nil, fmt.Errorf("invalid_argument: content is required")
		}
		memoryType := in.Type
		if strings.TrimSpace(memoryType) == "" {
			memoryType = string(domain.TypeDoc)
		} else if _, err := validType(memoryType); err != nil {
			return nil, nil, err
		}
		subject := in.Subject
		if strings.TrimSpace(subject) == "" {
			subject = defaultSubjectFromFilename(in.Filename)
		}
		service, done, err := open(ctx)
		if err != nil {
			return nil, nil, toolError(err)
		}
		defer done()
		if err := ensureNamespace(ctx, service, in.Namespace); err != nil {
			return nil, nil, toolError(err)
		}
		memory, err := service.AddMemory(ctx, app.AddInput{
			Namespace:  in.Namespace,
			Subject:    subject,
			Type:       memoryType,
			Content:    in.Content,
			Tags:       in.Tags,
			Metadata:   in.Metadata,
			ExpiresAt:  in.ExpiresAt,
			Source:     "mcp:" + in.Filename,
			RelatedIDs: in.RelatedIDs,
			Relation:   in.Relation,
		})
		if err != nil {
			return nil, nil, toolError(err)
		}
		return textResult(memory)
	})

	addTool(server, "memory_history", "List version history of a memory, or show one full version.", func(ctx context.Context, _ *mcp.CallToolRequest, in historyArgs) (*mcp.CallToolResult, any, error) {
		service, done, err := open(ctx)
		if err != nil {
			return nil, nil, toolError(err)
		}
		defer done()
		if in.Version > 0 {
			snapshot, err := service.MemoryVersion(ctx, in.ID, in.Version)
			if err != nil {
				return nil, nil, toolError(err)
			}
			return textResult(map[string]any{"id": in.ID, "version": snapshot})
		}
		versions, err := service.MemoryHistory(ctx, in.ID)
		if err != nil {
			return nil, nil, toolError(err)
		}
		return textResult(map[string]any{"id": in.ID, "versions": versions})
	})

	addTool(server, "memory_revert", "Restore a memory from a saved version.", func(ctx context.Context, _ *mcp.CallToolRequest, in revertArgs) (*mcp.CallToolResult, any, error) {
		if in.Version <= 0 {
			return nil, nil, fmt.Errorf("invalid_argument: version is required")
		}
		service, done, err := open(ctx)
		if err != nil {
			return nil, nil, toolError(err)
		}
		defer done()
		memory, err := service.RevertMemory(ctx, in.ID, in.Version)
		if err != nil {
			return nil, nil, toolError(err)
		}
		return textResult(memory)
	})
}

func optionalString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func defaultSubjectFromFilename(filename string) string {
	base := filename
	if idx := strings.LastIndexAny(base, "/\\"); idx >= 0 {
		base = base[idx+1:]
	}
	if dot := strings.LastIndex(base, "."); dot > 0 {
		base = base[:dot]
	}
	if strings.TrimSpace(base) == "" {
		return "imported"
	}
	return base
}
