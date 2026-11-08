package app_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ayame113/my-workspace-manager/internal/app"
	"github.com/ayame113/my-workspace-manager/internal/gitexec"
	"github.com/ayame113/my-workspace-manager/internal/layout"
)

type refGit struct {
	refs map[string]bool
}

func (g refGit) Run(ctx context.Context, dir string, args ...string) (gitexec.Result, error) {
	if args[0] == "show-ref" && g.refs[args[len(args)-1]] {
		return gitexec.Result{}, nil
	}
	if args[0] == "show-ref" {
		return gitexec.Result{ExitCode: 1}, nil
	}
	return gitexec.Result{}, nil
}

func sequence(values ...int) func(int) int {
	i := 0
	return func(int) int {
		v := values[i%len(values)]
		i++
		return v
	}
}

func nameFor(values ...int) string {
	return layout.RandomName(sequence(values...))
}

func newApp(t *testing.T, git gitexec.Runner, intn func(int) int) (*app.App, string) {
	t.Helper()
	root := t.TempDir()
	return app.New(app.Config{
		Layout: layout.Layout{Root: root},
		Git:    git,
		Getenv: func(string) string { return "" },
		Getwd:  func() (string, error) { return root, nil },
		IntN:   intn,
	}), root
}

func TestRandomTaskNameRegeneratesOnCollision(t *testing.T) {
	t.Parallel()
	a, root := newApp(t, refGit{}, sequence(0, 0, 1, 1))
	if err := os.MkdirAll(filepath.Join(root, "tasks", nameFor(0, 0)), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := a.RandomTaskName(nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := nameFor(1, 1); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRandomTaskNameGivesUpWhenEverythingCollides(t *testing.T) {
	t.Parallel()
	a, root := newApp(t, refGit{}, sequence(0))
	if err := os.MkdirAll(filepath.Join(root, "tasks", nameFor(0)), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RandomTaskName(nil); err == nil {
		t.Error("want an error")
	}
}

func TestRandomTaskNameUsesRepoPrefix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		repos []string
		want  string
	}{
		{"none", nil, nameFor(0, 0)},
		{"two", []string{"a", "b"}, "a-b-" + nameFor(0, 0)},
		{"one is normalized", []string{"My_Repo.js"}, "my-repo-js-" + nameFor(0, 0)},
		{"nothing usable left", []string{"___"}, nameFor(0, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a, _ := newApp(t, refGit{}, sequence(0))
			got, err := a.RandomTaskName(tt.repos)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want || !layout.IsValidTaskName(got) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRandomTaskNameRegeneratesWithPrefix(t *testing.T) {
	t.Parallel()
	a, root := newApp(t, refGit{}, sequence(0, 0, 1, 1))
	if err := os.MkdirAll(filepath.Join(root, "tasks", "r-"+nameFor(0, 0)), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := a.RandomTaskName([]string{"r"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "r-" + nameFor(1, 1); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRandomBranchNameAvoidsLocalAndOriginInEveryRepo(t *testing.T) {
	t.Parallel()
	git := refGit{refs: map[string]bool{
		"refs/heads/" + nameFor(0, 0):          true,
		"refs/remotes/origin/" + nameFor(1, 1): true,
	}}
	a, root := newApp(t, git, sequence(0, 0, 1, 1, 2, 2))
	for _, repo := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(root, "repos", repo+".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got, err := a.RandomBranchName(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if want := nameFor(2, 2); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func isolateGit(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "a@b")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "a@b")
}

func makeRemote(t *testing.T, dir, branch string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "init", "--quiet", "--initial-branch="+branch)
	runGit(t, dir, "commit", "--quiet", "--allow-empty", "-m", "init")
}

func realApp(t *testing.T, wrap ...func(gitexec.Runner) gitexec.Runner) (*app.App, string) {
	t.Helper()
	isolateGit(t)
	root := filepath.Join(t.TempDir(), "ws")
	git := gitexec.New()
	for _, w := range wrap {
		git = w(git)
	}
	return app.New(app.Config{
		Layout: layout.Layout{Root: root},
		Git:    git,
		Getenv: func(string) string { return "" },
		Getwd:  func() (string, error) { return root, nil },
	}), root
}

func TestNewTaskWithWorktreesCreatesWorktrees(t *testing.T) {
	a, root := realApp(t)
	remotes := t.TempDir()
	for _, repo := range []string{"one", "two"} {
		remote := filepath.Join(remotes, repo)
		makeRemote(t, remote, "main")
		if _, err := a.Clone(context.Background(), "file://"+remote, repo); err != nil {
			t.Fatal(err)
		}
	}
	result, err := a.NewTaskWithWorktrees(context.Background(), "job", []string{"one", "two"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Worktrees) != 2 || result.Dir != filepath.Join(root, "tasks/job") {
		t.Fatalf("result = %+v", result)
	}
	for _, worktree := range result.Worktrees {
		if want := filepath.Join(root, "tasks/job", worktree.Repo+"--"+result.Branch); worktree.Path != want {
			t.Errorf("path = %s, want %s", worktree.Path, want)
		}
		out, err := exec.Command("git", "-C", worktree.Path, "symbolic-ref", "--short", "HEAD").Output()
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(string(out)); got != result.Branch {
			t.Errorf("%s checks out %q, want %q", worktree.Repo, got, result.Branch)
		}
	}
}

func TestNewTaskWithWorktreesWithoutReposOnlyCreatesTask(t *testing.T) {
	a, root := realApp(t)
	result, err := a.NewTaskWithWorktrees(context.Background(), "job", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Worktrees) != 0 || result.Branch != "" {
		t.Errorf("result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "tasks/job")); err != nil {
		t.Error(err)
	}
}

func TestNewTaskWithWorktreesChecksEveryRepoBeforeChanging(t *testing.T) {
	a, root := realApp(t)
	remote := filepath.Join(t.TempDir(), "good")
	makeRemote(t, remote, "main")
	if _, err := a.Clone(context.Background(), "file://"+remote, "good"); err != nil {
		t.Fatal(err)
	}
	noBase := filepath.Join(t.TempDir(), "nobase")
	makeRemote(t, noBase, "trunk")
	if _, err := a.Clone(context.Background(), "file://"+noBase, "nobase"); err != nil {
		t.Fatal(err)
	}
	runGit(t, filepath.Join(root, "repos/nobase.git"), "symbolic-ref", "--delete", "refs/remotes/origin/HEAD")
	_, err := a.NewTaskWithWorktrees(context.Background(), "job", []string{"good", "nobase"})
	if err == nil || !strings.Contains(err.Error(), "nobase") {
		t.Fatalf("err = %v, want one naming nobase", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "tasks/job")); !os.IsNotExist(statErr) {
		t.Error("task directory must not be created")
	}
}

type failingWorktreeAdd struct {
	gitexec.Runner
	bareSuffix string
}

func (g failingWorktreeAdd) Run(ctx context.Context, dir string, args ...string) (gitexec.Result, error) {
	if len(args) > 1 && args[0] == "worktree" && args[1] == "add" && strings.HasSuffix(dir, g.bareSuffix) {
		return gitexec.Result{ExitCode: 128, Stderr: "injected failure"}, nil
	}
	return g.Runner.Run(ctx, dir, args...)
}

func TestNewTaskWithWorktreesKeepsCreatedOnFailure(t *testing.T) {
	a, _ := realApp(t, func(g gitexec.Runner) gitexec.Runner { return failingWorktreeAdd{g, "two.git"} })
	remotes := t.TempDir()
	for _, repo := range []string{"one", "two"} {
		remote := filepath.Join(remotes, repo)
		makeRemote(t, remote, "main")
		if _, err := a.Clone(context.Background(), "file://"+remote, repo); err != nil {
			t.Fatal(err)
		}
	}
	result, err := a.NewTaskWithWorktrees(context.Background(), "job", []string{"one", "two"})
	failure, ok := errors.AsType[*app.WorktreeCreateError](err)
	if !ok || failure.Repo != "two" {
		t.Fatalf("err = %v, want WorktreeCreateError for two; %+v", err, result)
	}
	if len(result.Worktrees) != 1 || result.Worktrees[0].Repo != "one" {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(result.Worktrees[0].Path); err != nil {
		t.Errorf("created worktree must remain: %v", err)
	}
}
