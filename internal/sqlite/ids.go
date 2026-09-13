package sqlite

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

func randomID(prefix string, bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return prefix + hex.EncodeToString(value), nil
}

func newNamespaceID() (string, error) {
	return randomID("ns_", 8)
}

func newMemoryID() (string, error) {
	return randomID("mem_", 12)
}
