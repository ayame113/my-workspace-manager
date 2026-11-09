package app_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetDescriptionKeepsUnknownKeys(t *testing.T) {
	t.Parallel()
	a, root := homeApp(t)
	if _, err := a.NewTask("job"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "ws/tasks/job/.ws-task.json")
	if err := os.WriteFile(path, []byte(`{"future": {"x": 1}, "description": "old"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	stored, err := a.SetDescription("job", "  line one\n  line two ")
	if err != nil {
		t.Fatal(err)
	}
	if stored != "line one line two" {
		t.Errorf("stored = %q", stored)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]any
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatal(err)
	}
	if meta["description"] != "line one line two" || meta["future"] == nil {
		t.Errorf("meta = %v", meta)
	}
	if comment, _ := meta["_comment"].(string); !strings.Contains(comment, "ws describe --task job") {
		t.Errorf("_comment = %q", comment)
	}
	if got, err := a.Description("job"); err != nil || got != "line one line two" {
		t.Errorf("Description = %q, %v", got, err)
	}
}

func TestSetDescriptionEmptyRemovesFileWhenNothingElseIsStored(t *testing.T) {
	t.Parallel()
	a, root := homeApp(t)
	if _, err := a.NewTask("job"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetDescription("job", "something"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetDescription("job", " "); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "ws/tasks/job/.ws-task.json")); !os.IsNotExist(err) {
		t.Errorf("file must be removed: %v", err)
	}
	if got, err := a.Description("job"); err != nil || got != "" {
		t.Errorf("Description = %q, %v", got, err)
	}
}

func TestBrokenTaskMetaIsReported(t *testing.T) {
	t.Parallel()
	a, root := homeApp(t)
	if _, err := a.NewTask("job"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ws/tasks/job/.ws-task.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetDescription("job", "x"); err == nil {
		t.Error("want an error instead of overwriting a broken file")
	}
}
