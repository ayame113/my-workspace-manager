package app_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestListOrdersByGitActivity(t *testing.T) {
	a, root := realApp(t)
	remote := filepath.Join(t.TempDir(), "r")
	makeRemote(t, remote, "main")
	if _, err := a.Clone(context.Background(), "file://"+remote, "r"); err != nil {
		t.Fatal(err)
	}
	for _, task := range []string{"old", "new"} {
		if _, err := a.NewTaskWithWorktrees(context.Background(), task, []string{"r"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.NewTask("empty"); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(root, "tasks/empty"), past, past); err != nil {
		t.Fatal(err)
	}
	oldTask, err := a.ListWorktrees(context.Background(), "old")
	if err != nil {
		t.Fatal(err)
	}
	// 古い task の worktree で git を操作すると、その task が先頭に来る
	runGit(t, filepath.Join(root, "tasks/old", oldTask[0].Name), "commit", "--quiet", "--allow-empty", "-m", "touch")

	names, err := a.TaskNames()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"old", "new", "empty"}; !slices.Equal(names, want) {
		t.Errorf("TaskNames = %q, want %q", names, want)
	}
	summaries, err := a.ListTasks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summaries[0].Name != "old" || summaries[2].Name != "empty" {
		t.Errorf("ListTasks = %+v", summaries)
	}
	before := summaries[0].Updated
	if _, err := a.ListTasks(context.Background()); err != nil {
		t.Fatal(err)
	}
	again, err := a.ListTasks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !again[0].Updated.Equal(before) {
		t.Error("listing must not change the activity time")
	}
}
