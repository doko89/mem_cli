package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mem_cli/internal/domain"
)

type NamespaceRepository interface {
	Create(ctx context.Context, path string) (domain.Namespace, bool, error)
	List(ctx context.Context, prefix string) ([]domain.Namespace, error)
	Get(ctx context.Context, target string) (domain.Namespace, error)
	Delete(ctx context.Context, target string, recursive bool) error
}

type MemoryRepository interface {
	Create(ctx context.Context, memory domain.Memory) (domain.Memory, error)
	Get(ctx context.Context, id string, includeExpired bool) (domain.Memory, error)
	Update(ctx context.Context, memory domain.Memory) (domain.Memory, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, filter MemoryFilter) ([]domain.Memory, error)
	Search(ctx context.Context, filter SearchFilter) ([]domain.SearchResult, error)
}

type AddInput struct {
	Namespace string
	Subject   string
	Type      string
	Content   string
	Reason    string
	Tags      []string
	Metadata  map[string]any
	Source    string
	ExpiresAt string
}

type UpdateInput struct {
	ID          string
	Subject     *string
	Type        *string
	Content     *string
	Reason      *string
	Tags        []string
	TagsSet     bool
	Metadata    map[string]any
	MetadataSet bool
	Source      *string
	ExpiresAt   *string
	ClearExpiry bool
}

type MemoryFilter struct {
	NamespaceID    string
	Subject        string
	Type           domain.MemoryType
	Tag            string
	Subtree        bool
	IncludeExpired bool
	Limit          int
}

type SearchFilter struct {
	Query       string
	NamespaceID string
	Subject     string
	Type        domain.MemoryType
	Tag         string
	Subtree     bool
	MatchMode   string
	Limit       int
}

type Service struct {
	namespaces NamespaceRepository
	memories   MemoryRepository
	now        func() time.Time
}

func NewService(namespaces NamespaceRepository, memories MemoryRepository) *Service {
	return &Service{namespaces: namespaces, memories: memories, now: time.Now}
}

func (s *Service) CreateNamespace(ctx context.Context, input string) (domain.Namespace, bool, error) {
	normalized, err := domain.ValidateNamespace(input)
	if err != nil {
		return domain.Namespace{}, false, err
	}
	return s.namespaces.Create(ctx, normalized)
}

func (s *Service) ListNamespaces(ctx context.Context, prefix string) ([]domain.Namespace, error) {
	normalized := prefix
	if normalized != "" {
		var err error
		normalized, err = domain.ValidateNamespace(prefix)
		if err != nil {
			return nil, err
		}
	}
	return s.namespaces.List(ctx, normalized)
}

func (s *Service) GetNamespace(ctx context.Context, target string) (domain.Namespace, error) {
	if strings.TrimSpace(target) == "" {
		return domain.Namespace{}, domain.NewInvalidArgumentError("namespace id or path is required")
	}
	return s.namespaces.Get(ctx, target)
}

func (s *Service) DeleteNamespace(ctx context.Context, target string, recursive bool) error {
	if strings.TrimSpace(target) == "" {
		return domain.NewInvalidArgumentError("namespace id or path is required")
	}
	return s.namespaces.Delete(ctx, target, recursive)
}

func (s *Service) AddMemory(ctx context.Context, input AddInput) (domain.Memory, error) {
	namespace, err := s.namespaces.Get(ctx, input.Namespace)
	if err != nil {
		return domain.Memory{}, err
	}
	memoryType, err := domain.ValidateMemoryType(input.Type)
	if err != nil {
		return domain.Memory{}, err
	}
	if err := validateMemoryFields(input.Subject, input.Content); err != nil {
		return domain.Memory{}, err
	}
	tags, err := normalizeTags(input.Tags)
	if err != nil {
		return domain.Memory{}, err
	}
	expiresAt, err := normalizeTimestamp(input.ExpiresAt, false)
	if err != nil {
		return domain.Memory{}, err
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	source := sourceMap(input.Source)
	memory := domain.Memory{
		NamespaceID: namespace.ID,
		Subject:     strings.TrimSpace(input.Subject),
		Type:        memoryType,
		Content:     input.Content,
		Reason:      input.Reason,
		Tags:        tags,
		Metadata:    input.Metadata,
		Source:      source,
		CreatedAt:   now,
		UpdatedAt:   now,
		ExpiresAt:   expiresAt,
	}
	return s.memories.Create(ctx, memory)
}

func (s *Service) GetMemory(ctx context.Context, id string, includeExpired bool) (domain.Memory, error) {
	if strings.TrimSpace(id) == "" {
		return domain.Memory{}, domain.NewMemoryNotFoundError(id)
	}
	return s.memories.Get(ctx, id, includeExpired)
}

func (s *Service) ListMemories(ctx context.Context, filter MemoryFilter) ([]domain.Memory, error) {
	if strings.TrimSpace(filter.NamespaceID) == "" {
		return nil, domain.NewInvalidArgumentError("namespace is required")
	}
	if filter.Limit <= 0 {
		filter.Limit = 100
	}
	if filter.Limit > 10000 {
		filter.Limit = 10000
	}
	namespace, err := s.namespaces.Get(ctx, filter.NamespaceID)
	if err != nil {
		return nil, err
	}
	filter.NamespaceID = namespace.NormalizedName
	return s.memories.List(ctx, filter)
}

func (s *Service) SearchMemories(ctx context.Context, filter SearchFilter) ([]domain.SearchResult, error) {
	if strings.TrimSpace(filter.Query) == "" {
		return nil, domain.NewInvalidArgumentError("query is required")
	}
	if strings.TrimSpace(filter.NamespaceID) == "" {
		return nil, domain.NewInvalidArgumentError("namespace is required")
	}
	if filter.Limit <= 0 {
		filter.Limit = 10
	}
	if filter.Limit > 1000 {
		filter.Limit = 1000
	}
	if filter.MatchMode == "" {
		filter.MatchMode = "all"
	}
	if filter.MatchMode != "all" && filter.MatchMode != "any" {
		return nil, domain.NewInvalidArgumentError("match mode must be all or any")
	}
	namespace, err := s.namespaces.Get(ctx, filter.NamespaceID)
	if err != nil {
		return nil, err
	}
	filter.NamespaceID = namespace.NormalizedName
	return s.memories.Search(ctx, filter)
}

func (s *Service) UpdateMemory(ctx context.Context, input UpdateInput) (domain.Memory, error) {
	memory, err := s.memories.Get(ctx, input.ID, false)
	if err != nil {
		return domain.Memory{}, err
	}
	if input.Subject != nil {
		memory.Subject = strings.TrimSpace(*input.Subject)
	}
	if input.Content != nil {
		memory.Content = *input.Content
	}
	if input.Type != nil {
		memoryType, typeErr := domain.ValidateMemoryType(*input.Type)
		if typeErr != nil {
			return domain.Memory{}, typeErr
		}
		memory.Type = memoryType
	}
	if input.Reason != nil {
		memory.Reason = *input.Reason
	}
	if input.TagsSet {
		memory.Tags, err = normalizeTags(input.Tags)
		if err != nil {
			return domain.Memory{}, err
		}
	}
	if input.MetadataSet {
		memory.Metadata = input.Metadata
	}
	if input.Source != nil {
		memory.Source = sourceMap(*input.Source)
	}
	if input.ClearExpiry {
		memory.ExpiresAt = ""
	} else if input.ExpiresAt != nil {
		memory.ExpiresAt, err = normalizeTimestamp(*input.ExpiresAt, false)
		if err != nil {
			return domain.Memory{}, err
		}
	}
	if err := validateMemoryFields(memory.Subject, memory.Content); err != nil {
		return domain.Memory{}, err
	}
	memory.UpdatedAt = s.now().UTC().Format(time.RFC3339Nano)
	return s.memories.Update(ctx, memory)
}

func (s *Service) ForgetMemory(ctx context.Context, id string) error {
	return s.memories.Delete(ctx, id)
}

func (s *Service) ImportFile(ctx context.Context, path, namespace, subject, memoryType string, tags []string, metadata map[string]any, expiresAt string) (domain.Memory, error) {
	if strings.TrimSpace(path) == "" {
		return domain.Memory{}, domain.NewImportError("file path is required", nil)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return domain.Memory{}, domain.NewImportError("unable to read imported file", err)
	}
	if strings.TrimSpace(memoryType) == "" {
		memoryType = string(domain.TypeFact)
	}
	return s.AddMemory(ctx, AddInput{
		Namespace: namespace,
		Subject:   subject,
		Type:      memoryType,
		Content:   string(content),
		Tags:      tags,
		Metadata:  metadata,
		ExpiresAt: expiresAt,
		Source:    path,
	})
}

func (s *Service) ExportMemories(ctx context.Context, namespace string, subtree bool) ([]domain.Memory, error) {
	target, err := s.namespaces.Get(ctx, namespace)
	if err != nil {
		return nil, err
	}
	return s.memories.List(ctx, MemoryFilter{NamespaceID: target.NormalizedName, Subtree: subtree, IncludeExpired: true, Limit: 10000})
}

func validateMemoryFields(subject, content string) error {
	if strings.TrimSpace(subject) == "" {
		return domain.NewInvalidArgumentError("subject is required")
	}
	if strings.TrimSpace(content) == "" {
		return domain.NewInvalidArgumentError("content is required")
	}
	return nil
}

func normalizeTags(tags []string) ([]string, error) {
	seen := make(map[string]struct{}, len(tags))
	normalized := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if strings.ContainsAny(tag, ",[]{}") {
			return nil, domain.NewInvalidArgumentError("tags cannot contain commas or JSON delimiters")
		}
		if _, exists := seen[tag]; exists {
			continue
		}
		seen[tag] = struct{}{}
		normalized = append(normalized, tag)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func normalizeTimestamp(value string, required bool) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		if required {
			return "", domain.NewInvalidArgumentError("timestamp is required")
		}
		return "", nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return "", domain.NewInvalidArgumentError("timestamp must use RFC3339 format")
	}
	return parsed.UTC().Format(time.RFC3339Nano), nil
}

func sourceMap(source string) map[string]any {
	if strings.TrimSpace(source) == "" {
		return nil
	}
	sourceType := "manual"
	if path := filepath.Clean(source); !strings.Contains(path, "://") {
		if _, err := os.Stat(path); err == nil {
			sourceType = "file"
		}
	}
	return map[string]any{"type": sourceType, "path": source}
}

func wrapDatabase(message string, err error) error {
	if err == nil {
		return nil
	}
	var domainErr *domain.Error
	if errors.As(err, &domainErr) {
		return err
	}
	return &domain.Error{Code: domain.ErrorDatabase, Message: message, Err: fmt.Errorf("%w", err)}
}
