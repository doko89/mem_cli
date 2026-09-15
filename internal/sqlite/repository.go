package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"mem_cli/internal/app"
	"mem_cli/internal/domain"
)

type Repository struct {
	db *sql.DB
}

type Namespaces struct {
	repository *Repository
}

type Memories struct {
	repository *Repository
}

func (r *Repository) NamespaceRepository() *Namespaces {
	return &Namespaces{repository: r}
}

func (r *Repository) MemoryRepository() *Memories {
	return &Memories{repository: r}
}

func (r *Namespaces) Create(ctx context.Context, path string) (domain.Namespace, bool, error) {
	return r.repository.createNamespace(ctx, path)
}

func (r *Namespaces) List(ctx context.Context, prefix string) ([]domain.Namespace, error) {
	return r.repository.listNamespaces(ctx, prefix)
}

func (r *Namespaces) Get(ctx context.Context, target string) (domain.Namespace, error) {
	return r.repository.getNamespaceByTarget(ctx, target)
}

func (r *Namespaces) Delete(ctx context.Context, target string, recursive bool) error {
	return r.repository.deleteNamespace(ctx, target, recursive)
}

func (r *Memories) Create(ctx context.Context, memory domain.Memory) (domain.Memory, error) {
	return r.repository.createMemory(ctx, memory)
}

func (r *Memories) Get(ctx context.Context, id string, includeExpired bool) (domain.Memory, error) {
	return r.repository.getMemory(ctx, id, includeExpired)
}

func (r *Memories) Update(ctx context.Context, memory domain.Memory) (domain.Memory, error) {
	return r.repository.updateMemory(ctx, memory)
}

func (r *Memories) Delete(ctx context.Context, id string) error {
	return r.repository.deleteMemory(ctx, id)
}

func (r *Memories) List(ctx context.Context, filter app.MemoryFilter) ([]domain.Memory, error) {
	return r.repository.listMemories(ctx, filter)
}

func (r *Memories) ResolveID(ctx context.Context, target string) (string, error) {
	return r.repository.resolveMemoryID(ctx, target)
}

func (r *Memories) GetDetails(ctx context.Context, id string, includeExpired bool) (domain.MemoryDetails, error) {
	return r.repository.getMemoryDetails(ctx, id, includeExpired)
}

func (r *Memories) ReplaceLinks(ctx context.Context, fromID string, targetIDs []string, relation string) ([]domain.MemoryLink, error) {
	return r.repository.replaceMemoryLinks(ctx, fromID, targetIDs, relation)
}

func (r *Memories) History(ctx context.Context, id string) ([]domain.MemoryVersionSummary, error) {
	return r.repository.memoryHistory(ctx, id)
}

func (r *Memories) Version(ctx context.Context, id string, version int64) (domain.MemoryVersionSnapshot, error) {
	return r.repository.memoryVersion(ctx, id, version)
}

func (r *Memories) Search(ctx context.Context, filter app.SearchFilter) ([]domain.SearchResult, error) {
	return r.repository.searchMemories(ctx, filter)
}

func Open(path string) (*Repository, error) {
	if strings.TrimSpace(path) == "" {
		return nil, domain.NewInvalidArgumentError("database path is required")
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, wrapDatabase("unable to create database directory", err)
		}
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, wrapDatabase("unable to open database", err)
	}
	database.SetMaxOpenConns(1)
	repository := &Repository{db: database}
	if err := repository.backupBeforeMigration(path); err != nil {
		_ = database.Close()
		return nil, err
	}
	if err := repository.migrate(); err != nil {
		_ = database.Close()
		return nil, err
	}
	return repository, nil
}

func (r *Repository) backupBeforeMigration(path string) error {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		if err != nil && !os.IsNotExist(err) {
			return wrapDatabase("unable to inspect database before migration", err)
		}
		return nil
	}
	var version int
	if err := r.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return wrapDatabase("unable to read database schema version", err)
	}
	if version >= 2 {
		return nil
	}
	backup := path + ".bak-" + time.Now().UTC().Format("20060102T150405.000000000Z")
	if _, err := r.db.Exec(`VACUUM INTO ?`, backup); err != nil {
		return wrapDatabase("unable to create pre-migration backup", err)
	}
	return nil
}

func (r *Repository) Close() error {
	if err := r.db.Close(); err != nil {
		return wrapDatabase("unable to close database", err)
	}
	return nil
}

func (r *Repository) migrate() error {
	statements := []string{
		`PRAGMA foreign_keys = ON`,
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA journal_mode = WAL`,
		`PRAGMA synchronous = NORMAL`,
		`CREATE TABLE IF NOT EXISTS namespaces (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			normalized_name TEXT NOT NULL UNIQUE,
			parent_id TEXT,
			description TEXT,
			created_at TEXT NOT NULL,
			FOREIGN KEY (parent_id) REFERENCES namespaces(id)
		)`,
		`CREATE TABLE IF NOT EXISTS memories (
			id TEXT PRIMARY KEY,
			namespace_id TEXT NOT NULL,
			subject TEXT NOT NULL,
			type TEXT NOT NULL,
			content TEXT NOT NULL,
			reason TEXT,
			tags TEXT,
			metadata TEXT,
			source TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			expires_at TEXT,
			FOREIGN KEY (namespace_id) REFERENCES namespaces(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_namespace ON memories(namespace_id)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_subject ON memories(subject)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_type ON memories(type)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_updated_at ON memories(updated_at DESC)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
			memory_id UNINDEXED,
			subject,
			content,
			reason,
			tokenize = 'unicode61'
		)`,
		`CREATE TRIGGER IF NOT EXISTS memories_fts_insert AFTER INSERT ON memories BEGIN
			INSERT INTO memories_fts(memory_id, subject, content, reason)
			VALUES (NEW.id, NEW.subject, NEW.content, coalesce(NEW.reason, ''));
		END`,
		`CREATE TRIGGER IF NOT EXISTS memories_fts_delete AFTER DELETE ON memories BEGIN
			DELETE FROM memories_fts WHERE memory_id = OLD.id;
		END`,
		`CREATE TRIGGER IF NOT EXISTS memories_fts_update AFTER UPDATE ON memories BEGIN
			DELETE FROM memories_fts WHERE memory_id = OLD.id;
			INSERT INTO memories_fts(memory_id, subject, content, reason)
			VALUES (NEW.id, NEW.subject, NEW.content, coalesce(NEW.reason, ''));
		END`,
		`CREATE TABLE IF NOT EXISTS memory_links (
			from_id TEXT NOT NULL,
			to_id TEXT NOT NULL,
			relation TEXT NOT NULL DEFAULT 'related',
			created_at TEXT,
			PRIMARY KEY(from_id, to_id, relation)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_memory_links_to_id ON memory_links(to_id)`,
		`CREATE TABLE IF NOT EXISTS memory_versions (
			seq INTEGER PRIMARY KEY AUTOINCREMENT,
			memory_id TEXT NOT NULL,
			version INTEGER NOT NULL,
			data TEXT NOT NULL,
			created_at TEXT NOT NULL,
			UNIQUE(memory_id, version)
		)`,
		`PRAGMA user_version = 2`,
	}
	for _, statement := range statements {
		if _, err := r.db.Exec(statement); err != nil {
			return wrapDatabase("unable to initialize database schema", err)
		}
	}
	return nil
}

func (r *Repository) createNamespace(ctx context.Context, path string) (domain.Namespace, bool, error) {
	segments := strings.Split(path, "/")
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Namespace{}, false, wrapDatabase("unable to begin namespace transaction", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var parentID string
	var finalNamespace domain.Namespace
	created := false
	for index := range segments {
		currentPath := strings.Join(segments[:index+1], "/")
		namespace, exists, err := findNamespaceByPath(ctx, tx, currentPath)
		if err != nil {
			return domain.Namespace{}, false, err
		}
		if exists {
			parentID = namespace.ID
			if index == len(segments)-1 {
				finalNamespace = namespace
			}
			continue
		}
		id, err := newNamespaceID()
		if err != nil {
			return domain.Namespace{}, false, err
		}
		createdAt, err := currentTimestamp(ctx, tx)
		if err != nil {
			return domain.Namespace{}, false, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO namespaces(id, name, normalized_name, parent_id, description, created_at) VALUES (?, ?, ?, ?, NULL, ?)`, id, currentPath, currentPath, nullableID(parentID), createdAt)
		if err != nil {
			return domain.Namespace{}, false, wrapDatabase("unable to create namespace", err)
		}
		finalNamespace = domain.Namespace{ID: id, Name: currentPath, NormalizedName: currentPath, ParentID: parentID, CreatedAt: createdAt}
		parentID = id
		created = true
	}
	if err := tx.Commit(); err != nil {
		return domain.Namespace{}, false, wrapDatabase("unable to commit namespace transaction", err)
	}
	committed = true
	return finalNamespace, created, nil
}

func (r *Repository) listNamespaces(ctx context.Context, prefix string) ([]domain.Namespace, error) {
	query := `SELECT id, name, normalized_name, coalesce(parent_id, ''), coalesce(description, ''), created_at FROM namespaces`
	args := []any{}
	if prefix != "" {
		query += ` WHERE normalized_name = ? OR normalized_name LIKE ? ESCAPE '\'`
		args = append(args, prefix, likePrefix(prefix))
	}
	query += ` ORDER BY normalized_name ASC`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapDatabase("unable to list namespaces", err)
	}
	defer rows.Close()
	namespaces, err := scanNamespaces(rows)
	if err != nil {
		return nil, err
	}
	return namespaces, nil
}

func (r *Repository) getNamespaceByTarget(ctx context.Context, target string) (domain.Namespace, error) {
	normalized, normalizeErr := domain.NormalizeNamespace(target)
	query := `SELECT id, name, normalized_name, coalesce(parent_id, ''), coalesce(description, ''), created_at FROM namespaces WHERE id = ? OR normalized_name = ?`
	if normalizeErr == nil {
		return r.getNamespace(ctx, query, target, normalized)
	}
	return r.getNamespace(ctx, query, target, target)
}

func (r *Repository) deleteNamespace(ctx context.Context, target string, recursive bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapDatabase("unable to begin namespace delete transaction", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	namespace, err := findNamespace(ctx, tx, target)
	if err != nil {
		return err
	}
	var childCount, memoryCount int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM namespaces WHERE parent_id = ?`, namespace.ID).Scan(&childCount); err != nil {
		return wrapDatabase("unable to inspect namespace children", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM memories WHERE namespace_id = ?`, namespace.ID).Scan(&memoryCount); err != nil {
		return wrapDatabase("unable to inspect namespace memories", err)
	}
	if !recursive && (childCount > 0 || memoryCount > 0) {
		return domain.NewNamespaceConflictError("Namespace has children or memories; use recursive delete.")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM memories WHERE namespace_id IN (
		WITH RECURSIVE descendants(id) AS (
			SELECT id FROM namespaces WHERE id = ?
			UNION ALL
			SELECT n.id FROM namespaces n JOIN descendants d ON n.parent_id = d.id
		)
		SELECT id FROM descendants
	)`, namespace.ID); err != nil {
		return wrapDatabase("unable to delete namespace memories", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_links
		WHERE from_id IN (
			WITH RECURSIVE descendants(id) AS (
				SELECT id FROM namespaces WHERE id = ?
				UNION ALL
				SELECT n.id FROM namespaces n JOIN descendants d ON n.parent_id = d.id
			)
			SELECT id FROM descendants
		)`, namespace.ID); err != nil {
		return wrapDatabase("unable to delete outgoing namespace memory links", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_links
		WHERE to_id IN (
			WITH RECURSIVE descendants(id) AS (
				SELECT id FROM namespaces WHERE id = ?
				UNION ALL
				SELECT n.id FROM namespaces n JOIN descendants d ON n.parent_id = d.id
			)
			SELECT id FROM descendants
		)`, namespace.ID); err != nil {
		return wrapDatabase("unable to delete incoming namespace memory links", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM namespaces WHERE id IN (
		WITH RECURSIVE descendants(id) AS (
			SELECT id FROM namespaces WHERE id = ?
			UNION ALL
			SELECT n.id FROM namespaces n JOIN descendants d ON n.parent_id = d.id
		)
		SELECT id FROM descendants
	)`, namespace.ID); err != nil {
		return wrapDatabase("unable to delete namespaces", err)
	}
	if err := tx.Commit(); err != nil {
		return wrapDatabase("unable to commit namespace delete", err)
	}
	committed = true
	return nil
}

func (r *Repository) createMemory(ctx context.Context, memory domain.Memory) (domain.Memory, error) {
	if memory.ID == "" {
		id, err := newMemoryID()
		if err != nil {
			return domain.Memory{}, err
		}
		memory.ID = id
	}
	tags, metadata, source, err := encodeJSON(memory)
	if err != nil {
		return domain.Memory{}, err
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO memories(
		id, namespace_id, subject, type, content, reason, tags, metadata, source, created_at, updated_at, expires_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		memory.ID, memory.NamespaceID, memory.Subject, string(memory.Type), memory.Content, nullableText(memory.Reason), tags, metadata, source,
		memory.CreatedAt, memory.UpdatedAt, nullableText(memory.ExpiresAt),
	)
	if err != nil {
		return domain.Memory{}, wrapDatabase("unable to create memory", err)
	}
	return memory, nil
}

func (r *Repository) getMemory(ctx context.Context, id string, includeExpired bool) (domain.Memory, error) {
	query := memorySelect() + ` WHERE m.id = ?`
	args := []any{id}
	if !includeExpired {
		query += ` AND (m.expires_at IS NULL OR m.expires_at >= ?)`
		args = append(args, timeNow())
	}
	row := r.db.QueryRowContext(ctx, query, args...)
	memory, err := scanMemory(row)
	if err == sql.ErrNoRows {
		return domain.Memory{}, domain.NewMemoryNotFoundError(id)
	}
	if err != nil {
		return domain.Memory{}, err
	}
	return memory, nil
}

func (r *Repository) updateMemory(ctx context.Context, memory domain.Memory) (domain.Memory, error) {
	tags, metadata, source, err := encodeJSON(memory)
	if err != nil {
		return domain.Memory{}, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Memory{}, wrapDatabase("unable to begin memory update transaction", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	var previous domain.Memory
	var previousJSON string
	row := tx.QueryRowContext(ctx, memorySelect()+` WHERE m.id = ?`, memory.ID)
	previous, err = scanMemory(row)
	if err == sql.ErrNoRows {
		return domain.Memory{}, domain.NewMemoryNotFoundError(memory.ID)
	}
	if err != nil {
		return domain.Memory{}, err
	}
	encoded, err := json.Marshal(previous)
	if err != nil {
		return domain.Memory{}, wrapDatabase("unable to encode memory version", err)
	}
	previousJSON = string(encoded)
	if _, err := tx.ExecContext(ctx, `INSERT INTO memory_versions(memory_id, version, data, created_at)
		SELECT ?, coalesce(max(version), 0) + 1, ?, ?
		FROM memory_versions WHERE memory_id = ?`,
		memory.ID, previousJSON, timeNow(), memory.ID,
	); err != nil {
		return domain.Memory{}, wrapDatabase("unable to create memory version", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE memories SET subject = ?, type = ?, content = ?, reason = ?, tags = ?, metadata = ?, source = ?, updated_at = ?, expires_at = ? WHERE id = ?`,
		memory.Subject, string(memory.Type), memory.Content, nullableText(memory.Reason), tags, metadata, source, memory.UpdatedAt, nullableText(memory.ExpiresAt), memory.ID,
	)
	if err != nil {
		return domain.Memory{}, wrapDatabase("unable to update memory", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Memory{}, wrapDatabase("unable to commit memory update", err)
	}
	committed = true
	return memory, nil
}

func (r *Repository) deleteMemory(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapDatabase("unable to begin memory delete transaction", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	row := tx.QueryRowContext(ctx, memorySelect()+` WHERE m.id = ?`, id)
	memory, err := scanMemory(row)
	if err == sql.ErrNoRows {
		return domain.NewMemoryNotFoundError(id)
	}
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(memory)
	if err != nil {
		return wrapDatabase("unable to encode memory version", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO memory_versions(memory_id, version, data, created_at)
		SELECT ?, coalesce(max(version), 0) + 1, ?, ?
		FROM memory_versions WHERE memory_id = ?`,
		id, string(encoded), timeNow(), id,
	); err != nil {
		return wrapDatabase("unable to create final memory version", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM memories WHERE id = ?`, id); err != nil {
		return wrapDatabase("unable to delete memory", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_links WHERE from_id = ? OR to_id = ?`, id, id); err != nil {
		return wrapDatabase("unable to delete memory links", err)
	}
	if err := tx.Commit(); err != nil {
		return wrapDatabase("unable to commit memory delete", err)
	}
	committed = true
	return nil
}

func (r *Repository) listMemories(ctx context.Context, filter app.MemoryFilter) ([]domain.Memory, error) {
	query := memorySelect() + ` WHERE ` + namespaceScope(filter.NamespaceID, filter.Subtree)
	args := namespaceScopeArgs(filter.NamespaceID, filter.Subtree)
	if filter.Subject != "" {
		query += ` AND m.subject = ?`
		args = append(args, filter.Subject)
	}
	if filter.Type != "" {
		query += ` AND m.type = ?`
		args = append(args, string(filter.Type))
	}
	if filter.Tag != "" {
		query += ` AND EXISTS (SELECT 1 FROM json_each(m.tags) WHERE json_each.value = ?)`
		args = append(args, filter.Tag)
	}
	if filter.RelatedTo != "" {
		query += ` AND (
			EXISTS (SELECT 1 FROM memory_links out_link WHERE out_link.from_id = m.id AND out_link.to_id = ?)
			OR EXISTS (SELECT 1 FROM memory_links back_link WHERE back_link.from_id = ? AND back_link.to_id = m.id)
		)`
		args = append(args, filter.RelatedTo, filter.RelatedTo)
	}
	if !filter.IncludeExpired {
		query += ` AND (m.expires_at IS NULL OR m.expires_at >= ?)`
		args = append(args, timeNow())
	}
	query += ` ORDER BY m.updated_at DESC, m.id ASC LIMIT ?`
	args = append(args, filter.Limit)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapDatabase("unable to list memories", err)
	}
	defer rows.Close()
	return scanMemories(rows)
}

func (r *Repository) searchMemories(ctx context.Context, filter app.SearchFilter) ([]domain.SearchResult, error) {
	ftsQuery, err := buildFTSQuery(filter.Query, filter.MatchMode)
	if err != nil {
		return nil, err
	}
	query := `SELECT m.id, m.namespace_id, n.normalized_name, m.subject, m.type, snippet(memories_fts, 2, '', '', '…', 18), bm25(memories_fts), m.created_at, m.updated_at
		FROM memories_fts
		JOIN memories m ON m.id = memories_fts.memory_id
		LEFT JOIN namespaces n ON n.id = m.namespace_id
		WHERE memories_fts MATCH ? AND ` + namespaceScope(filter.NamespaceID, filter.Subtree) + ` AND (m.expires_at IS NULL OR m.expires_at >= ?)`
	args := append([]any{ftsQuery}, namespaceScopeArgs(filter.NamespaceID, filter.Subtree)...)
	args = append(args, timeNow())
	if filter.Subject != "" {
		query += ` AND m.subject = ?`
		args = append(args, filter.Subject)
	}
	if filter.Type != "" {
		query += ` AND m.type = ?`
		args = append(args, string(filter.Type))
	}
	if filter.Tag != "" {
		query += ` AND EXISTS (SELECT 1 FROM json_each(m.tags) WHERE json_each.value = ?)`
		args = append(args, filter.Tag)
	}
	query += ` ORDER BY bm25(memories_fts) ASC, m.updated_at DESC, m.id ASC LIMIT ?`
	args = append(args, filter.Limit)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapDatabase("unable to search memories", err)
	}
	defer rows.Close()
	results := make([]domain.SearchResult, 0)
	for rows.Next() {
		var result domain.SearchResult
		var score float64
		if err := rows.Scan(&result.ID, &result.NamespaceID, &result.Namespace, &result.Subject, &result.Type, &result.Snippet, &score, &result.CreatedAt, &result.UpdatedAt); err != nil {
			return nil, wrapDatabase("unable to read search result", err)
		}
		result.Score = -score
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDatabase("unable to complete search", err)
	}
	return results, nil
}

func memorySelect() string {
	return `SELECT m.id, m.namespace_id, n.normalized_name, m.subject, m.type, m.content, coalesce(m.reason, ''), coalesce(m.tags, 'null'), coalesce(m.metadata, 'null'), coalesce(m.source, 'null'), m.created_at, m.updated_at, coalesce(m.expires_at, '')
		FROM memories m
		LEFT JOIN namespaces n ON n.id = m.namespace_id`
}

func scanMemories(rows *sql.Rows) ([]domain.Memory, error) {
	memories := make([]domain.Memory, 0)
	for rows.Next() {
		memory, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		memories = append(memories, memory)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDatabase("unable to read memories", err)
	}
	return memories, nil
}

func scanNamespaces(rows *sql.Rows) ([]domain.Namespace, error) {
	namespaces := make([]domain.Namespace, 0)
	for rows.Next() {
		var namespace domain.Namespace
		if err := rows.Scan(&namespace.ID, &namespace.Name, &namespace.NormalizedName, &namespace.ParentID, &namespace.Description, &namespace.CreatedAt); err != nil {
			return nil, wrapDatabase("unable to read namespace", err)
		}
		namespaces = append(namespaces, namespace)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDatabase("unable to complete namespace read", err)
	}
	return namespaces, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMemory(row rowScanner) (domain.Memory, error) {
	var memory domain.Memory
	var tags, metadata, source string
	err := row.Scan(&memory.ID, &memory.NamespaceID, &memory.Namespace, &memory.Subject, &memory.Type, &memory.Content, &memory.Reason, &tags, &metadata, &source, &memory.CreatedAt, &memory.UpdatedAt, &memory.ExpiresAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return domain.Memory{}, domain.NewMemoryNotFoundError("")
		}
		return domain.Memory{}, wrapDatabase("unable to read memory", err)
	}
	if tags != "null" {
		if err := json.Unmarshal([]byte(tags), &memory.Tags); err != nil {
			return domain.Memory{}, wrapDatabase("memory contains invalid tags", err)
		}
	}
	if metadata != "null" {
		if err := json.Unmarshal([]byte(metadata), &memory.Metadata); err != nil {
			return domain.Memory{}, wrapDatabase("memory contains invalid metadata", err)
		}
	}
	if source != "null" {
		if err := json.Unmarshal([]byte(source), &memory.Source); err != nil {
			return domain.Memory{}, wrapDatabase("memory contains invalid source", err)
		}
	}
	return memory, nil
}

func encodeJSON(memory domain.Memory) (*string, *string, *string, error) {
	tags, err := marshalNullable(memory.Tags)
	if err != nil {
		return nil, nil, nil, wrapDatabase("unable to encode tags", err)
	}
	metadata, err := marshalNullable(memory.Metadata)
	if err != nil {
		return nil, nil, nil, wrapDatabase("unable to encode metadata", err)
	}
	source, err := marshalNullable(memory.Source)
	if err != nil {
		return nil, nil, nil, wrapDatabase("unable to encode source", err)
	}
	return textPointer(tags), textPointer(metadata), textPointer(source), nil
}

func marshalNullable(value any) (string, error) {
	if value == nil {
		return "null", nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func textPointer(value string) *string {
	if value == "null" {
		return nil
	}
	return &value
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableID(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func findNamespace(ctx context.Context, querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, target string) (domain.Namespace, error) {
	normalized, normalizeErr := domain.NormalizeNamespace(target)
	query := `SELECT id, name, normalized_name, coalesce(parent_id, ''), coalesce(description, ''), created_at FROM namespaces WHERE id = ?`
	args := []any{target}
	if normalizeErr == nil {
		query += ` OR normalized_name = ?`
		args = append(args, normalized)
	}
	row := querier.QueryRowContext(ctx, query, args...)
	var namespace domain.Namespace
	err := row.Scan(&namespace.ID, &namespace.Name, &namespace.NormalizedName, &namespace.ParentID, &namespace.Description, &namespace.CreatedAt)
	if err == sql.ErrNoRows {
		return domain.Namespace{}, domain.NewNamespaceNotFoundError(target)
	}
	if err != nil {
		return domain.Namespace{}, wrapDatabase("unable to find namespace", err)
	}
	return namespace, nil
}

func findNamespaceByPath(ctx context.Context, tx *sql.Tx, path string) (domain.Namespace, bool, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, name, normalized_name, coalesce(parent_id, ''), coalesce(description, ''), created_at FROM namespaces WHERE normalized_name = ?`, path)
	var namespace domain.Namespace
	err := row.Scan(&namespace.ID, &namespace.Name, &namespace.NormalizedName, &namespace.ParentID, &namespace.Description, &namespace.CreatedAt)
	if err == sql.ErrNoRows {
		return domain.Namespace{}, false, nil
	}
	if err != nil {
		return domain.Namespace{}, false, wrapDatabase("unable to find namespace", err)
	}
	return namespace, true, nil
}

func (r *Repository) getNamespace(ctx context.Context, query string, args ...any) (domain.Namespace, error) {
	row := r.db.QueryRowContext(ctx, query, args...)
	var namespace domain.Namespace
	err := row.Scan(&namespace.ID, &namespace.Name, &namespace.NormalizedName, &namespace.ParentID, &namespace.Description, &namespace.CreatedAt)
	if err == sql.ErrNoRows {
		return domain.Namespace{}, domain.NewNamespaceNotFoundError(fmt.Sprint(args[0]))
	}
	if err != nil {
		return domain.Namespace{}, wrapDatabase("unable to find namespace", err)
	}
	return namespace, nil
}

func currentTimestamp(ctx context.Context, tx *sql.Tx) (string, error) {
	var timestamp string
	err := tx.QueryRowContext(ctx, `SELECT strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`).Scan(&timestamp)
	if err != nil {
		return "", wrapDatabase("unable to generate timestamp", err)
	}
	return timestamp, nil
}

func timeNow() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func likePrefix(prefix string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(prefix) + `/%`
}

func namespaceScope(namespace string, subtree bool) string {
	if subtree {
		return `EXISTS (
			SELECT 1 FROM namespaces scope
			WHERE scope.id = m.namespace_id
			  AND (scope.normalized_name = ? OR scope.normalized_name LIKE ? ESCAPE '\')
		)`
	}
	return `EXISTS (
		SELECT 1 FROM namespaces scope
		WHERE scope.id = m.namespace_id AND scope.normalized_name = ?
	)`
}

func namespaceScopeArgs(namespace string, subtree bool) []any {
	if subtree {
		return []any{namespace, likePrefix(namespace)}
	}
	return []any{namespace}
}

var ftsSeparators = regexp.MustCompile(`[^\p{L}\p{N}_]+`)

func buildFTSQuery(query string, matchMode string) (string, error) {
	query = strings.TrimSpace(query)
	parts := make([]string, 0)
	for _, term := range strings.Fields(query) {
		prefix := false
		if strings.HasSuffix(term, "*") {
			term = strings.TrimSuffix(term, "*")
			prefix = true
		}
		if strings.Contains(term, "*") {
			return "", domain.NewInvalidArgumentError(`wildcard "*" is only supported at the end of a term`)
		}
		tokens := ftsSeparators.Split(term, -1)
		for index, token := range tokens {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			tokenPrefix := ""
			if prefix && index == len(tokens)-1 {
				parts = append(parts, token+"*")
				continue
			}
			parts = append(parts, `"`+token+`"`+tokenPrefix)
		}
	}
	if len(parts) == 0 {
		return "", domain.NewInvalidArgumentError("query must contain searchable text")
	}
	if matchMode == "any" {
		return strings.Join(parts, " OR "), nil
	}
	return strings.Join(parts, " AND "), nil
}

func wrapDatabase(message string, err error) error {
	if err == nil {
		return nil
	}
	return &domain.Error{Code: domain.ErrorDatabase, Message: message, Err: err}
}
