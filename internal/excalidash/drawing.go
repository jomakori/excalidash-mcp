package excalidash

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Summary is the compact drawing form returned by listing and write tools.
type Summary struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	CollectionID *string `json:"collectionId"`
	ElementCount int     `json:"elementCount"`
	CreatedAt    string  `json:"createdAt"`
	UpdatedAt    string  `json:"updatedAt"`
}

// Summarise reduces a drawing to its compact form, dropping the preview and the scene.
func Summarise(drawing map[string]any) Summary {
	id, _ := drawing["id"].(string)
	name, _ := drawing["name"].(string)
	createdAt, _ := drawing["createdAt"].(string)
	updatedAt, _ := drawing["updatedAt"].(string)

	summary := Summary{
		ID:        id,
		Name:      name,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
	if collectionID, ok := drawing["collectionId"].(string); ok {
		summary.CollectionID = &collectionID
	}
	if elements, ok := drawing["elements"].([]any); ok {
		summary.ElementCount = len(elements)
	}
	return summary
}

// Elements normalises a scene supplied as a JSON string, a decoded array, or nothing.
func Elements(value any) ([]any, error) {
	switch typed := value.(type) {
	case nil:
		return []any{}, nil
	case string:
		if typed == "" {
			return []any{}, nil
		}
		var decoded []any
		if err := json.Unmarshal([]byte(typed), &decoded); err != nil {
			return nil, fmt.Errorf("elements must be a JSON array or a JSON-encoded array: %w", err)
		}
		return decoded, nil
	case []any:
		if typed == nil {
			return []any{}, nil
		}
		return typed, nil
	default:
		return nil, errors.New("elements must be a JSON array or a JSON-encoded array")
	}
}
