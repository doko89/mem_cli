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

func (r *Memories) Get(ctx context.Context, id string) (domain.Memory, error) {
	return r.repository.getMemory(ctx, id)
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
	if err := repository.migrate(); err != nil {
		_ = database.Close()
		return nil, err
	}
	return repository, nil
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

	finalPath := path
	_, finalExists, err := findNamespaceByPath(ctx, tx, finalPath)
	if err != nil {
		return domain.Namespace{}, false, err
	}
	if !finalExists {
		if err := rejectNamespaceTypo(ctx, tx, finalPath); err != nil {
			return domain.Namespace{}, false, err
		}
	}

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

func rejectNamespaceTypo(ctx context.Context, tx *sql.Tx, candidate string) error {
	rows, err := tx.QueryContext(ctx, `SELECT normalized_name FROM namespaces`)
	if err != nil {
		return wrapDatabase("unable to inspect namespace candidates", err)
	}
	defer rows.Close()
	candidates := make([]string, 0)
	for rows.Next() {
		var existing string
		if err := rows.Scan(&existing); err != nil {
			return wrapDatabase("unable to read namespace candidate", err)
		}
		if len(existing)-len(candidate) > 2 || len(candidate)-len(existing) > 2 || levenshtein(candidate, existing) > 2 {
			continue
		}
		candidates = append(candidates, existing)
	}
	if err := rows.Err(); err != nil {
		return wrapDatabase("unable to complete namespace candidate scan", err)
	}
	if len(candidates) == 0 {
		return nil
	}
	return domain.NewPossibleDuplicateNamespaceError(candidates)
}

func levenshtein(left, right string) int {
	if left == right {
		return 0
	}
	previous := make([]int, len(right)+1)
	current := make([]int, len(right)+1)
	for column := range previous {
		previous[column] = column
	}
	for row := 1; row <= len(left); row++ {
		current[0] = row
		for column := 1; column <= len(right); column++ {
			cost := 1
			if left[row-1] == right[column-1] {
				cost = 0
			}
			current[column] = min(current[column-1]+1, min(previous[column]+1, previous[column-1]+cost))
		}
		previous, current = current, previous
	}
	return previous[len(right)]
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

func (r *Repository) getMemory(ctx context.Context, id string) (domain.Memory, error) {
	row := r.db.QueryRowContext(ctx, memorySelect()+` WHERE m.id = ?`, id)
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
	result, err := r.db.ExecContext(ctx, `UPDATE memories SET subject = ?, type = ?, content = ?, reason = ?, tags = ?, metadata = ?, source = ?, updated_at = ?, expires_at = ? WHERE id = ?`,
		memory.Subject, string(memory.Type), memory.Content, nullableText(memory.Reason), tags, metadata, source, memory.UpdatedAt, nullableText(memory.ExpiresAt), memory.ID,
	)
	if err != nil {
		return domain.Memory{}, wrapDatabase("unable to update memory", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return domain.Memory{}, wrapDatabase("unable to verify memory update", err)
	}
	if affected == 0 {
		return domain.Memory{}, domain.NewMemoryNotFoundError(memory.ID)
	}
	return memory, nil
}

func (r *Repository) deleteMemory(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM memories WHERE id = ?`, id)
	if err != nil {
		return wrapDatabase("unable to delete memory", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return wrapDatabase("unable to verify memory delete", err)
	}
	if affected == 0 {
		return domain.NewMemoryNotFoundError(id)
	}
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
	ftsQuery, err := buildFTSQuery(filter.Query)
	if err != nil {
		return nil, err
	}
	query := `SELECT m.id, n.normalized_name, m.subject, m.type, snippet(memories_fts, 2, '', '', '…', 18), bm25(memories_fts), m.created_at, m.updated_at
		FROM memories_fts
		JOIN memories m ON m.id = memories_fts.memory_id
		JOIN namespaces n ON n.id = m.namespace_id
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
		if err := rows.Scan(&result.ID, &result.Namespace, &result.Subject, &result.Type, &result.Snippet, &score, &result.CreatedAt, &result.UpdatedAt); err != nil {
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
	return `SELECT m.id, m.namespace_id, m.subject, m.type, m.content, coalesce(m.reason, ''), coalesce(m.tags, 'null'), coalesce(m.metadata, 'null'), coalesce(m.source, 'null'), m.created_at, m.updated_at, coalesce(m.expires_at, '')
		FROM memories m`
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
	err := row.Scan(&memory.ID, &memory.NamespaceID, &memory.Subject, &memory.Type, &memory.Content, &memory.Reason, &tags, &metadata, &source, &memory.CreatedAt, &memory.UpdatedAt, &memory.ExpiresAt)
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

func buildFTSQuery(query string) (string, error) {
	tokens := ftsSeparators.Split(strings.TrimSpace(query), -1)
	parts := make([]string, 0, len(tokens))
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		parts = append(parts, `"`+strings.ReplaceAll(token, `"`, `""`)+`"`)
	}
	if len(parts) == 0 {
		return "", domain.NewInvalidArgumentError("query must contain searchable text")
	}
	return strings.Join(parts, " AND "), nil
}

func wrapDatabase(message string, err error) error {
	if err == nil {
		return nil
	}
	return &domain.Error{Code: domain.ErrorDatabase, Message: message, Err: err}
}
