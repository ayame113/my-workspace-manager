package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"charm.land/huh/v2"

	"github.com/ayame113/my-workspace-manager/internal/app"
	"github.com/ayame113/my-workspace-manager/internal/cli"
	"github.com/ayame113/my-workspace-manager/internal/gitexec"
)

type fakeGit struct {
	mu       sync.Mutex
	commands []string
}

func (g *fakeGit) Run(ctx context.Context, dir string, args ...string) (gitexec.Result, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.commands = append(g.commands, strings.Join(args, " "))
	if args[0] == "show-ref" {
		return gitexec.Result{ExitCode: 1}, nil
	}
	if args[0] == "check-ref-format" {
		return gitexec.Result{Stdout: args[len(args)-1]}, nil
	}
	return gitexec.Result{}, nil
}

type fakePrompter struct {
	addCalls    []app.AddOptions
	renameCalls []app.RenameOptions
	addResult   app.AddOptions
	renameOut   app.RenameOptions
	err         error
}

func (p *fakePrompter) Add(ctx context.Context, partial app.AddOptions) (app.AddOptions, error) {
	p.addCalls = append(p.addCalls, partial)
	return p.addResult, p.err
}

func (p *fakePrompter) Rename(ctx context.Context, partial app.RenameOptions) (app.RenameOptions, error) {
	p.renameCalls = append(p.renameCalls, partial)
	return p.renameOut, p.err
}

type harness struct {
	root     string
	git      *fakeGit
	prompter *fakePrompter
	stdout   bytes.Buffer
	stderr   bytes.Buffer
	env      cli.Env
}

type envOptions struct {
	tty        bool
	claudeCode bool
	cwd        string
}

func newHarness(t *testing.T, opts envOptions) *harness {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"tasks/t1", "tasks/t2", "repos/r.git"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h := &harness{root: root, git: &fakeGit{}, prompter: &fakePrompter{}}
	cwd := opts.cwd
	if cwd == "" {
		cwd = root
	} else {
		cwd = filepath.Join(root, cwd)
	}
	h.env = cli.Env{
		Stdin:  strings.NewReader(""),
		Stdout: &h.stdout,
		Stderr: &h.stderr,
		Getenv: func(name string) string {
			switch {
			case name == "WS_ROOT":
				return root
			case name == "CLAUDECODE" && opts.claudeCode:
				return "1"
			}
			return ""
		},
		Getwd:            func() (string, error) { return cwd, nil },
		StdinIsTerminal:  opts.tty,
		StderrIsTerminal: opts.tty,
		Git:              h.git,
		Prompter:         h.prompter,
	}
	return h
}

func (h *harness) run(args ...string) int {
	return cli.Run(context.Background(), args, h.env)
}

func TestExitCodes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"no arguments", nil, 2},
		{"unknown command", []string{"bogus"}, 2},
		{"new without task", []string{"new"}, 2},
		{"new with extra", []string{"new", "a", "b"}, 2},
		{"clone without url", []string{"clone"}, 2},
		{"compact with task", []string{"compact", "--task", "t"}, 2},
		{"unknown flag", []string{"worktree", "add", "--repo", "r", "--bogus", "--branch", "b"}, 2},
		{"add without repo", []string{"worktree", "add", "--branch", "x"}, 2},
		{"add with positional", []string{"worktree", "add", "--repo", "r", "--branch", "x", "extra"}, 2},
		{"base without create", []string{"worktree", "add", "--repo", "r", "--branch", "x", "--base", "origin/review"}, 2},
		{"create without base", []string{"worktree", "add", "--repo", "r", "--branch", "x", "-c"}, 2},
		{"readonly with create", []string{"worktree", "add", "--repo", "r", "--branch", "x", "--readonly", "-c", "--base", "o"}, 2},
		{"dash branch", []string{"worktree", "add", "--repo", "r", "--branch", "-x"}, 2},
		{"rename without to", []string{"worktree", "rename", "--repo", "r", "--branch", "a"}, 2},
		{"ls with flag", []string{"ls", "--all"}, 2},
		{"lint with task", []string{"lint", "--task", "t"}, 2},
		{"worktree alone", []string{"worktree"}, 2},
		{"worktree unknown", []string{"worktree", "bogus"}, 2},
		{"task flag swallowing flag", []string{"worktree", "lint", "--task", "--bogus"}, 2},
		{"help", []string{"--help"}, 0},
		{"subcommand help", []string{"worktree", "add", "--help"}, 0},
		{"version", []string{"-V"}, 0},
		{"new", []string{"new", "t3"}, 0},
		{"new invalid", []string{"new", "Bad_Task"}, 1},
		{"ls", []string{"ls"}, 0},
		{"worktree ls outside task", []string{"worktree", "ls"}, 1},
		{"worktree ls unknown task", []string{"worktree", "ls", "--task", "nope"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, envOptions{})
			if got := h.run(tt.args...); got != tt.want {
				t.Errorf("exit code = %d, want %d\nstdout: %s\nstderr: %s", got, tt.want, h.stdout.String(), h.stderr.String())
			}
		})
	}
}

func TestInterruptedContext(t *testing.T) {
	t.Parallel()
	h := newHarness(t, envOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := cli.Run(ctx, []string{"ls"}, h.env); got != cli.ExitInterrupt {
		t.Errorf("exit code = %d, want %d", got, cli.ExitInterrupt)
	}
}

func TestAddPrompt(t *testing.T) {
	t.Parallel()
	chosen := app.AddOptions{Task: "t1", Repo: "r", Branch: "feature/x", Base: "origin/main", Create: true}
	tests := []struct {
		name        string
		env         envOptions
		args        []string
		wantCode    int
		wantPartial []app.AddOptions
		wantAdd     string
	}{
		{
			name:        "everything missing",
			env:         envOptions{tty: true},
			args:        []string{"worktree", "add"},
			wantCode:    0,
			wantPartial: []app.AddOptions{{}},
			wantAdd:     "worktree add --no-track -b feature/x " + "%s/tasks/t1/r--feature-x origin/main",
		},
		{
			name:        "task from current directory",
			env:         envOptions{tty: true, cwd: "tasks/t1"},
			args:        []string{"worktree", "add"},
			wantCode:    0,
			wantPartial: []app.AddOptions{{Task: "t1"}},
			wantAdd:     "worktree add --no-track -b feature/x " + "%s/tasks/t1/r--feature-x origin/main",
		},
		{
			name:        "only missing values are asked",
			env:         envOptions{tty: true, cwd: "tasks/t1"},
			args:        []string{"worktree", "add", "--repo", "r", "-c"},
			wantCode:    0,
			wantPartial: []app.AddOptions{{Task: "t1", Repo: "r", Create: true}},
			wantAdd:     "worktree add --no-track -b feature/x " + "%s/tasks/t1/r--feature-x origin/main",
		},
		{
			name:     "not a terminal",
			env:      envOptions{tty: false},
			args:     []string{"worktree", "add"},
			wantCode: 2,
		},
		{
			name:     "inside an agent",
			env:      envOptions{tty: true, claudeCode: true},
			args:     []string{"worktree", "add"},
			wantCode: 2,
		},
		{
			name:     "complete arguments never prompt",
			env:      envOptions{tty: true, cwd: "tasks/t1"},
			args:     []string{"worktree", "add", "--repo", "r", "--branch", "b", "-c", "--base", "origin/main"},
			wantCode: 0,
			wantAdd:  "worktree add --no-track -b b %s/tasks/t1/r--b origin/main",
		},
		{
			name:     "named task that does not exist is not prompted",
			env:      envOptions{tty: true},
			args:     []string{"worktree", "add", "--task", "nope"},
			wantCode: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, tt.env)
			h.prompter.addResult = chosen
			if got := h.run(tt.args...); got != tt.wantCode {
				t.Fatalf("exit code = %d, want %d\nstderr: %s", got, tt.wantCode, h.stderr.String())
			}
			if !slices.Equal(h.prompter.addCalls, tt.wantPartial) {
				t.Errorf("prompter calls = %+v, want %+v", h.prompter.addCalls, tt.wantPartial)
			}
			if tt.wantAdd != "" {
				want := strings.Replace(tt.wantAdd, "%s", h.root, 1)
				if !slices.Contains(h.git.commands, want) {
					t.Errorf("git commands %q do not contain %q", h.git.commands, want)
				}
			}
		})
	}
}

func TestRenamePrompt(t *testing.T) {
	t.Parallel()
	chosen := app.RenameOptions{Task: "t1", Repo: "r", Branch: "old", To: "new"}

	t.Run("missing values are asked", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, envOptions{tty: true, cwd: "tasks/t1"})
		h.prompter.renameOut = chosen
		h.run("worktree", "rename", "--repo", "r")
		want := []app.RenameOptions{{Task: "t1", Repo: "r"}}
		if !slices.Equal(h.prompter.renameCalls, want) {
			t.Errorf("prompter calls = %+v, want %+v", h.prompter.renameCalls, want)
		}
		if !strings.Contains(h.stderr.String(), "作業用 worktree") {
			t.Errorf("the chosen options must reach app.Rename, stderr: %s", h.stderr.String())
		}
	})

	t.Run("not a terminal", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, envOptions{cwd: "tasks/t1"})
		if got := h.run("worktree", "rename", "--repo", "r"); got != 2 {
			t.Errorf("exit code = %d, want 2", got)
		}
		if len(h.prompter.renameCalls) != 0 {
			t.Error("prompter must not be called")
		}
	})
}

func TestUserAbortedPrompt(t *testing.T) {
	t.Parallel()
	h := newHarness(t, envOptions{tty: true, cwd: "tasks/t1"})
	h.prompter.err = huh.ErrUserAborted
	if got := h.run("worktree", "add"); got != cli.ExitInterrupt {
		t.Errorf("exit code = %d, want %d", got, cli.ExitInterrupt)
	}
	if h.stderr.Len() != 0 {
		t.Errorf("an aborted prompt prints nothing, got %q", h.stderr.String())
	}
}
