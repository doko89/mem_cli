package domain

import (
	"strings"
)

type MemoryType string

const (
	TypeFact       MemoryType = "fact"
	TypeDoc        MemoryType = "doc"
	TypeDecision   MemoryType = "decision"
	TypePreference MemoryType = "preference"
	TypeTodo       MemoryType = "todo"
	TypeEntity     MemoryType = "entity"
)

var memoryTypes = map[MemoryType]struct{}{
	TypeFact:       {},
	TypeDoc:        {},
	TypeDecision:   {},
	TypePreference: {},
	TypeTodo:       {},
	TypeEntity:     {},
}

type Memory struct {
	ID          string         `json:"id"`
	NamespaceID string         `json:"namespace_id"`
	Namespace   *string        `json:"namespace"`
	Subject     string         `json:"subject"`
	Type        MemoryType     `json:"type"`
	Content     string         `json:"content"`
	Reason      string         `json:"reason"`
	Tags        []string       `json:"tags"`
	Metadata    map[string]any `json:"metadata"`
	Source      map[string]any `json:"source"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
	ExpiresAt   string         `json:"expires_at"`
}

type MemoryLink struct {
	FromID    string  `json:"from_id"`
	ToID      string  `json:"to_id"`
	Relation  string  `json:"relation"`
	CreatedAt string  `json:"created_at"`
	Memory    *Memory `json:"memory"`
	Direction string  `json:"direction"`
}

type MemoryDetails struct {
	Memory
	RelatedOut []MemoryLink `json:"related_out"`
	Backlinks  []MemoryLink `json:"backlinks"`
}

type MemorySummary struct {
	ID          string         `json:"id"`
	NamespaceID string         `json:"namespace_id"`
	Namespace   *string        `json:"namespace"`
	Subject     string         `json:"subject"`
	Type        MemoryType     `json:"type"`
	Snippet     string         `json:"snippet"`
	Reason      string         `json:"reason"`
	Tags        []string       `json:"tags"`
	Metadata    map[string]any `json:"metadata"`
	Source      map[string]any `json:"source"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
	ExpiresAt   string         `json:"expires_at"`
}

type SubjectCandidate struct {
	ID        string `json:"id"`
	Namespace string `json:"namespace"`
	Subject   string `json:"subject"`
}

type MemoryVersion struct {
	Seq       int64  `json:"seq"`
	MemoryID  string `json:"memory_id"`
	Version   int64  `json:"version"`
	CreatedAt string `json:"created_at"`
	Data      string `json:"-"`
}

type MemoryVersionSummary struct {
	Version          int64  `json:"version"`
	CreatedAt        string `json:"created_at"`
	ContentSizeBytes int    `json:"content_size_bytes"`
	ContentSizeAfter int    `json:"content_size_after_bytes"`
}

type MemoryVersionSnapshot struct {
	Version   int64  `json:"version"`
	CreatedAt string `json:"created_at"`
	Memory    Memory `json:"snapshot"`
}

type SearchResult struct {
	ID          string     `json:"id"`
	NamespaceID string     `json:"namespace_id"`
	Namespace   *string    `json:"namespace"`
	Subject     string     `json:"subject"`
	Type        MemoryType `json:"type"`
	Snippet     string     `json:"snippet"`
	Score       float64    `json:"score"`
	CreatedAt   string     `json:"created_at"`
	UpdatedAt   string     `json:"updated_at"`
}

func ValidateMemoryType(input string) (MemoryType, error) {
	memoryType := MemoryType(strings.ToLower(strings.TrimSpace(input)))
	if _, ok := memoryTypes[memoryType]; !ok {
		return "", NewInvalidMemoryTypeError(input)
	}
	return memoryType, nil
}

func ValidMemoryTypes() []string {
	return []string{string(TypeFact), string(TypeDoc), string(TypeDecision), string(TypePreference), string(TypeTodo), string(TypeEntity)}
}
