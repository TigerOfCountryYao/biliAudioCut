package projects

import (
	"encoding/json"
	"testing"
)

func TestEmptyProjectListEncodesAsJSONArray(t *testing.T) {
	projects := make([]Project, 0)
	encoded, err := json.Marshal(map[string]any{"projects": projects})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if string(encoded) != `{"projects":[]}` {
		t.Fatalf("empty list JSON = %s, want projects array", encoded)
	}
}
