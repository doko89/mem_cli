package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"mem_cli/internal/domain"
)

func (r *Repository) resolveMemoryID(ctx context.Context, target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", domain.NewMemoryNotFoundError(target)
	}
	var id string
	err := r.db.QueryRowContext(ctx, `SELECT id FROM memories WHERE id = ?`, target).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", wrapDatabase("unable to resolve memory id", err)
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM memories WHERE id LIKE ? ESCAPE '\' ORDER BY id`, likePattern(target))
	if err != nil {
		return "", wrapDatabase("unable to resolve memory id prefix", err)
	}
	defer rows.Close()
	matches := make([]string, 0, 2)
	for rows.Next() {
		var match string
		if err := rows.Scan(&match); err != nil {
			return "", wrapDatabase("unable to read memory id prefix", err)
		}
		matches = append(matches, match)
	}
	if err := rows.Err(); err != nil {
		return "", wrapDatabase("unable to complete memory id prefix scan", err)
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", domain.NewMemoryNotFoundError(target)
	default:
		return "", domain.NewAmbiguousIDError(target)
	}
}

func (r *Memories) SubjectCandidates(ctx context.Context, subject, namespaceID string) ([]domain.SubjectCandidate, error) {
	return r.repository.subjectCandidates(ctx, subject, namespaceID)
}

func (r *Repository) subjectCandidates(ctx context.Context, subject, namespaceID string) ([]domain.SubjectCandidate, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT m.id, n.normalized_name, m.subject
		FROM memories m
		JOIN namespaces n ON n.id = m.namespace_id
		WHERE m.subject = ? AND m.namespace_id = ?
		ORDER BY m.created_at, m.id`,
		subject, namespaceID,
	)
	if err != nil {
		return nil, wrapDatabase("unable to resolve memory subject", err)
	}
	defer rows.Close()
	candidates := []domain.SubjectCandidate{}
	for rows.Next() {
		var candidate domain.SubjectCandidate
		if err := rows.Scan(&candidate.ID, &candidate.Namespace, &candidate.Subject); err != nil {
			return nil, wrapDatabase("unable to scan memory subject candidate", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDatabase("unable to complete memory subject scan", err)
	}
	return candidates, nil
}

func likePattern(prefix string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(prefix) + `%`
}

func (r *Repository) getMemoryDetails(ctx context.Context, id string, includeExpired bool) (domain.MemoryDetails, error) {
	resolvedID, err := r.resolveMemoryID(ctx, id)
	if err != nil {
		return domain.MemoryDetails{}, err
	}
	memory, err := r.getMemory(ctx, resolvedID, includeExpired)
	if err != nil {
		return domain.MemoryDetails{}, err
	}
	links, err := r.memoryLinks(ctx, resolvedID)
	if err != nil {
		return domain.MemoryDetails{}, err
	}
	return domain.MemoryDetails{Memory: memory, RelatedOut: links.out, Backlinks: links.back}, nil
}

type memoryLinkGroups struct {
	out  []domain.MemoryLink
	back []domain.MemoryLink
}

func (r *Repository) memoryLinks(ctx context.Context, id string) (memoryLinkGroups, error) {
	query := `SELECT l.from_id, l.to_id, coalesce(l.relation, 'related'), coalesce(l.created_at, ''),
		m.id, m.namespace_id, n.normalized_name, m.subject, m.type, m.content, coalesce(m.reason, ''),
		coalesce(m.tags, 'null'), coalesce(m.metadata, 'null'), coalesce(m.source, 'null'),
		m.created_at, m.updated_at, coalesce(m.expires_at, '')
	FROM memory_links l
	JOIN memories m ON m.id = CASE WHEN l.from_id = ? THEN l.to_id ELSE l.from_id END
	LEFT JOIN namespaces n ON n.id = m.namespace_id
	WHERE l.from_id = ? OR l.to_id = ?
	ORDER BY coalesce(l.relation, 'related'), coalesce(l.created_at, ''), m.id`
	rows, err := r.db.QueryContext(ctx, query, id, id, id)
	if err != nil {
		return memoryLinkGroups{}, wrapDatabase("unable to read memory links", err)
	}
	defer rows.Close()
	groups := memoryLinkGroups{out: []domain.MemoryLink{}, back: []domain.MemoryLink{}}
	for rows.Next() {
		var link domain.MemoryLink
		var memory domain.Memory
		var tags, metadata, source string
		err := rows.Scan(&link.FromID, &link.ToID, &link.Relation, &link.CreatedAt,
			&memory.ID, &memory.NamespaceID, &memory.Namespace, &memory.Subject, &memory.Type, &memory.Content, &memory.Reason,
			&tags, &metadata, &source, &memory.CreatedAt, &memory.UpdatedAt, &memory.ExpiresAt,
		)
		if err != nil {
			return memoryLinkGroups{}, wrapDatabase("unable to scan memory link", err)
		}
		linked, err := decodeLinkedMemory(memory, tags, metadata, source)
		if err != nil {
			return memoryLinkGroups{}, err
		}
		link.Memory = linked
		if link.FromID == id {
			link.Direction = "out"
			groups.out = append(groups.out, link)
		} else {
			link.Direction = "back"
			groups.back = append(groups.back, link)
		}
	}
	if err := rows.Err(); err != nil {
		return memoryLinkGroups{}, wrapDatabase("unable to complete memory link scan", err)
	}
	return groups, nil
}

func decodeLinkedMemory(memory domain.Memory, tags, metadata, source string) (*domain.Memory, error) {
	if tags != "null" {
		if err := json.Unmarshal([]byte(tags), &memory.Tags); err != nil {
			return nil, wrapDatabase("linked memory contains invalid tags", err)
		}
	}
	if metadata != "null" {
		if err := json.Unmarshal([]byte(metadata), &memory.Metadata); err != nil {
			return nil, wrapDatabase("linked memory contains invalid metadata", err)
		}
	}
	if source != "null" {
		if err := json.Unmarshal([]byte(source), &memory.Source); err != nil {
			return nil, wrapDatabase("linked memory contains invalid source", err)
		}
	}
	return &memory, nil
}

func (r *Repository) replaceMemoryLinks(ctx context.Context, fromID string, targetIDs []string, relation string) ([]domain.MemoryLink, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, wrapDatabase("unable to begin memory link transaction", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_links WHERE from_id = ? AND relation = ?`, fromID, relation); err != nil {
		return nil, wrapDatabase("unable to replace memory links", err)
	}
	links := make([]domain.MemoryLink, 0, len(targetIDs))
	for _, targetID := range targetIDs {
		createdAt := timeNow()
		_, err := tx.ExecContext(ctx, `INSERT INTO memory_links(from_id, to_id, relation, created_at) VALUES (?, ?, ?, ?)`,
			fromID, targetID, relation, createdAt,
		)
		if err != nil {
			return nil, wrapDatabase("unable to create memory link", err)
		}
		links = append(links, domain.MemoryLink{FromID: fromID, ToID: targetID, Relation: relation, CreatedAt: createdAt, Direction: "out"})
	}
	if err := tx.Commit(); err != nil {
		return nil, wrapDatabase("unable to commit memory links", err)
	}
	committed = true
	return links, nil
}

func (r *Repository) memoryHistory(ctx context.Context, id string) ([]domain.MemoryVersionSummary, error) {
	current, err := r.getMemory(ctx, id, true)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT seq, memory_id, version, data, created_at FROM memory_versions WHERE memory_id = ? ORDER BY version`, id)
	if err != nil {
		return nil, wrapDatabase("unable to read memory history", err)
	}
	defer rows.Close()
	versions := make([]domain.MemoryVersion, 0)
	for rows.Next() {
		var version domain.MemoryVersion
		if err := rows.Scan(&version.Seq, &version.MemoryID, &version.Version, &version.Data, &version.CreatedAt); err != nil {
			return nil, wrapDatabase("unable to scan memory version", err)
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDatabase("unable to complete memory history scan", err)
	}
	summaries := make([]domain.MemoryVersionSummary, 0, len(versions))
	for index, version := range versions {
		summary := domain.MemoryVersionSummary{Version: version.Version, CreatedAt: version.CreatedAt, ContentSizeAfter: len(current.Content)}
		summary.ContentSizeBytes, err = snapshotContentSize(version.Data)
		if err != nil {
			return nil, err
		}
		if index+1 < len(versions) {
			summary.ContentSizeAfter, err = snapshotContentSize(versions[index+1].Data)
			if err != nil {
				return nil, err
			}
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}

func (r *Repository) memoryVersion(ctx context.Context, id string, version int64) (domain.MemoryVersionSnapshot, error) {
	var record domain.MemoryVersion
	err := r.db.QueryRowContext(ctx, `SELECT seq, memory_id, version, data, created_at FROM memory_versions WHERE memory_id = ? AND version = ?`, id, version).
		Scan(&record.Seq, &record.MemoryID, &record.Version, &record.Data, &record.CreatedAt)
	if err == sql.ErrNoRows {
		return domain.MemoryVersionSnapshot{}, domain.NewInvalidArgumentError(fmt.Sprintf("version %d does not exist for memory %q", version, id))
	}
	if err != nil {
		return domain.MemoryVersionSnapshot{}, wrapDatabase("unable to read memory version", err)
	}
	var snapshot domain.MemoryVersionSnapshot
	if err := json.Unmarshal([]byte(record.Data), &snapshot.Memory); err != nil {
		return domain.MemoryVersionSnapshot{}, wrapDatabase("memory version contains invalid data", err)
	}
	snapshot.Version = record.Version
	snapshot.CreatedAt = record.CreatedAt
	return snapshot, nil
}

func snapshotContentSize(data string) (int, error) {
	var memory domain.Memory
	if err := json.Unmarshal([]byte(data), &memory); err != nil {
		return 0, wrapDatabase("memory version contains invalid data", err)
	}
	return len(memory.Content), nil
}
