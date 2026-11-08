package layout_test

import (
	"testing"

	"github.com/ayame113/my-workspace-manager/internal/layout"
)

func TestBranchSlug(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{"feature/ABC-123/add_user_api", "feature-abc-123-add-user-api"},
		{"--release//v1.2--", "release-v1-2"},
		{"日本語", ""},
		{"release/wave3.1", "release-wave3-1"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			if got := layout.BranchSlug(tt.in); got != tt.want {
				t.Errorf("BranchSlug(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNameRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		got  bool
		want bool
	}{
		{"task t-one", layout.IsValidTaskName("t-one"), true},
		{"task double hyphen", layout.IsValidTaskName("a--b"), false},
		{"task leading hyphen", layout.IsValidTaskName("-a"), false},
		{"task upper", layout.IsValidTaskName("Bad_Task"), false},
		{"task empty", layout.IsValidTaskName(""), false},
		{"repo mixed", layout.IsRepoNameShape("example-API.docs_x"), true},
		{"repo leading dot", layout.IsRepoNameShape(".x"), false},
		{"repo empty", layout.IsRepoNameShape(""), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.got != tt.want {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}

func TestWorktreeName(t *testing.T) {
	t.Parallel()
	if got := layout.SplitWorktreeName("plain"); got.Repo != "plain" || got.Slug != "" {
		t.Errorf("SplitWorktreeName(plain) = %+v", got)
	}
	if _, ok := layout.ParseWorktreeName("random-dir"); ok {
		t.Error("random-dir must not parse")
	}
	got, ok := layout.ParseWorktreeName("myrepo--feat-x")
	if !ok || got.Repo != "myrepo" || got.Slug != "feat-x" {
		t.Errorf("ParseWorktreeName = %+v, %v", got, ok)
	}
}

func TestTaskUnder(t *testing.T) {
	t.Parallel()
	if task, ok := layout.TaskUnder("/r/tasks", "/r/tasks/t1/repo--x/sub"); !ok || task != "t1" {
		t.Errorf("got %q, %v", task, ok)
	}
	if _, ok := layout.TaskUnder("/r/tasks", "/r/tasks"); ok {
		t.Error("tasks root itself belongs to no task")
	}
	if _, ok := layout.TaskUnder("/r/tasks", "/elsewhere/t1"); ok {
		t.Error("outside path belongs to no task")
	}
}
