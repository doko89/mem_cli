package domain

import (
	"strings"
)

type Namespace struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	NormalizedName string `json:"normalized_name"`
	ParentID       string `json:"parent_id"`
	Description    string `json:"description,omitempty"`
	CreatedAt      string `json:"created_at"`
}

func NormalizeNamespace(input string) (string, error) {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(input), "\\", "/"))
	segments := make([]string, 0, 4)
	for segment := range strings.SplitSeq(normalized, "/") {
		segment = strings.TrimSpace(segment)
		switch segment {
		case "", ".":
			continue
		case "..":
			return "", NewInvalidNamespaceError(input)
		default:
			segments = append(segments, segment)
		}
	}
	if len(segments) == 0 {
		return "", NewInvalidNamespaceError(input)
	}
	return strings.Join(segments, "/"), nil
}

func ValidateNamespace(input string) (string, error) {
	normalized, err := NormalizeNamespace(input)
	if err != nil {
		return "", err
	}
	for _, segment := range strings.Split(normalized, "/") {
		if strings.ContainsAny(segment, " \t\r\n") {
			return "", NewInvalidNamespaceError(input)
		}
	}
	return normalized, nil
}
