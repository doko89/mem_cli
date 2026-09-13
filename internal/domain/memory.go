package domain

import (
	"strings"
)

type MemoryType string

const (
	TypeFact       MemoryType = "fact"
	TypeDecision   MemoryType = "decision"
	TypePreference MemoryType = "preference"
	TypeTodo       MemoryType = "todo"
	TypeEntity     MemoryType = "entity"
)

var memoryTypes = map[MemoryType]struct{}{
	TypeFact:       {},
	TypeDecision:   {},
	TypePreference: {},
	TypeTodo:       {},
	TypeEntity:     {},
}

type Memory struct {
	ID          string         `json:"id"`
	NamespaceID string         `json:"namespace_id"`
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

type SearchResult struct {
	ID        string     `json:"id"`
	Namespace string     `json:"namespace,omitempty"`
	Subject   string     `json:"subject"`
	Type      MemoryType `json:"type"`
	Snippet   string     `json:"snippet"`
	Score     float64    `json:"score"`
	CreatedAt string     `json:"created_at"`
	UpdatedAt string     `json:"updated_at"`
}

func ValidateMemoryType(input string) (MemoryType, error) {
	memoryType := MemoryType(strings.ToLower(strings.TrimSpace(input)))
	if _, ok := memoryTypes[memoryType]; !ok {
		return "", NewInvalidMemoryTypeError(input)
	}
	return memoryType, nil
}

func ValidMemoryTypes() []string {
	return []string{string(TypeFact), string(TypeDecision), string(TypePreference), string(TypeTodo), string(TypeEntity)}
}
