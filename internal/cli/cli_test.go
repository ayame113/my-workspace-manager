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

	"github.com/ayame113/my-workspace-manager/internal/app"
	"github.com/ayame113/my-workspace-manager/internal/cli"
	"github.com/ayame113/my-workspace-manager/internal/gitexec"
	"github.com/ayame113/my-workspace-manager/internal/prompt"
)

type fakeGit struct {
	mu                sync.Mutex
	commands          []string
	failWorktreeAddIn string
}

func (g *fakeGit) Run(ctx context.Context, dir string, args ...string) (gitexec.Result, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.commands = append(g.commands, strings.Join(args, " "))
	if args[0] == "worktree" && args[1] == "add" && g.failWorktreeAddIn != "" && strings.HasSuffix(dir, g.failWorktreeAddIn) {
		return gitexec.Result{ExitCode: 128, Stderr: "boom\n"}, nil
	}
	if args[0] == "show-ref" {
		return gitexec.Result{ExitCode: 1}, nil
	}
	if args[0] == "symbolic-ref" {
		return gitexec.Result{Stdout: "old"}, nil
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

	chooseTaskCalls int
	chooseTaskOut   string
	newCalls        int
	newOut          app.NewTaskOptions

	skillTargetCalls int
	skillTargetsOut  []string
}

func (p *fakePrompter) ChooseTask(ctx context.Context) (string, error) {
	p.chooseTaskCalls++
	return p.chooseTaskOut, p.err
}

func (p *fakePrompter) New(ctx context.Context) (app.NewTaskOptions, error) {
	p.newCalls++
	return p.newOut, p.err
}

func (p *fakePrompter) ChooseSkillTargets(ctx context.Context) ([]string, error) {
	p.skillTargetCalls++
	return p.skillTargetsOut, p.err
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
	tty bool
	cwd string
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
			case name == "HOME":
				return root
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
		{"skill alone", []string{"skill"}, 2},
		{"skill install without target", []string{"skill", "install"}, 2},
		{"skill install unknown target", []string{"skill", "install", "bogus"}, 2},
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

func TestAddPrompt(t *testing.T) {
	t.Parallel()
	chosen := app.AddOptions{Task: "t1", Repo: "r", Branch: "feature/x", Base: "origin/main", Create: true}
	tests := []struct {
		name        string
		env         envOptions
		args        []string
		wantCode    int
		wantPartial []app.AddOptions
	}{
		{"everything missing", envOptions{tty: true}, []string{"worktree", "add"}, 0, []app.AddOptions{{}}},
		{"only missing values are asked", envOptions{tty: true, cwd: "tasks/t1"}, []string{"worktree", "add", "--repo", "r", "-c"}, 0, []app.AddOptions{{Task: "t1", Repo: "r", Create: true}}},
		{"not a terminal", envOptions{}, []string{"worktree", "add"}, 2, nil},
		{"complete arguments", envOptions{tty: true, cwd: "tasks/t1"}, []string{"worktree", "add", "--repo", "r", "--branch", "b", "-c", "--base", "origin/main"}, 0, nil},
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
			if len(tt.wantPartial) > 0 && !slices.ContainsFunc(h.git.commands, func(c string) bool { return strings.Contains(c, "-b "+chosen.Branch) }) {
				t.Errorf("prompted branch did not reach git: %q", h.git.commands)
			}
		})
	}
}

func TestRenamePrompt(t *testing.T) {
	t.Parallel()
	t.Run("missing values are asked", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, envOptions{tty: true, cwd: "tasks/t1"})
		h.prompter.renameOut = app.RenameOptions{Task: "t1", Repo: "r", Branch: "old", To: "new"}
		worktree := filepath.Join(h.root, "tasks/t1/r--old")
		if err := os.MkdirAll(worktree, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(worktree, ".git"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if got := h.run("worktree", "rename", "--repo", "r"); got != 0 {
			t.Fatalf("exit code = %d, want 0\nstderr: %s", got, h.stderr.String())
		}
		if want := []app.RenameOptions{{Task: "t1", Repo: "r"}}; !slices.Equal(h.prompter.renameCalls, want) {
			t.Errorf("prompter calls = %+v, want %+v", h.prompter.renameCalls, want)
		}
		if !slices.Contains(h.git.commands, "branch --quiet -m old new") {
			t.Errorf("prompted names did not reach git: %q", h.git.commands)
		}
	})

	for _, tt := range []struct {
		name string
		env  envOptions
	}{
		{"not a terminal", envOptions{cwd: "tasks/t1"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, tt.env)
			if got := h.run("worktree", "rename", "--repo", "r"); got != 2 {
				t.Errorf("exit code = %d, want 2", got)
			}
			if len(h.prompter.renameCalls) != 0 {
				t.Error("prompter must not be called")
			}
		})
	}
}

func TestUserAbortedPrompt(t *testing.T) {
	t.Parallel()
	h := newHarness(t, envOptions{tty: true, cwd: "tasks/t1"})
	h.prompter.err = prompt.ErrAborted
	if got := h.run("worktree", "add"); got != cli.ExitInterrupt {
		t.Errorf("exit code = %d, want %d", got, cli.ExitInterrupt)
	}
	if got := h.stderr.String(); got != "ws: 中断しました\n" {
		t.Errorf("stderr = %q", got)
	}
}

func TestWorktreeCommandsChooseTaskOutsideTask(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"ls", "lint", "pull"} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			tests := []struct {
				name       string
				env        envOptions
				args       []string
				wantCode   int
				wantChoose int
			}{
				{"outside a task", envOptions{tty: true}, nil, 0, 1},
				{"inside a task", envOptions{tty: true, cwd: "tasks/t1"}, nil, 0, 0},
				{"task flag", envOptions{tty: true}, []string{"--task", "t2"}, 0, 0},
				{"not a terminal", envOptions{}, nil, 1, 0},
			}
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()
					h := newHarness(t, tt.env)
					h.prompter.chooseTaskOut = "t1"
					if got := h.run(append([]string{"worktree", command}, tt.args...)...); got != tt.wantCode {
						t.Fatalf("exit code = %d, want %d\nstderr: %s", got, tt.wantCode, h.stderr.String())
					}
					if h.prompter.chooseTaskCalls != tt.wantChoose {
						t.Errorf("ChooseTask calls = %d, want %d", h.prompter.chooseTaskCalls, tt.wantChoose)
					}
				})
			}
		})
	}
}

func TestChosenTaskReachesCommand(t *testing.T) {
	t.Parallel()
	h := newHarness(t, envOptions{tty: true})
	h.prompter.chooseTaskOut = "nope"
	if got := h.run("worktree", "ls"); got != 1 {
		t.Errorf("exit code = %d, want 1 for a task that does not exist", got)
	}
}

func TestChooseTaskAbort(t *testing.T) {
	t.Parallel()
	h := newHarness(t, envOptions{tty: true})
	h.prompter.err = prompt.ErrAborted
	if got := h.run("worktree", "ls"); got != cli.ExitInterrupt {
		t.Errorf("exit code = %d, want %d", got, cli.ExitInterrupt)
	}
}

func TestNewInteractive(t *testing.T) {
	t.Parallel()
	t.Run("creates the task and a worktree per repo", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, envOptions{tty: true})
		h.prompter.newOut = app.NewTaskOptions{Task: "t3", Repos: []string{"r"}}
		if got := h.run("new"); got != 0 {
			t.Fatalf("exit code = %d\nstderr: %s", got, h.stderr.String())
		}
		taskDir := filepath.Join(h.root, "tasks/t3")
		if _, err := os.Stat(taskDir); err != nil {
			t.Fatal(err)
		}
		out := h.stdout.String()
		if !strings.Contains(out, "作成しました: "+taskDir+"\n") || !strings.Contains(out, "作成しました: "+taskDir+"/r--") || !strings.Contains(out, "ブランチ: ") {
			t.Errorf("stdout = %q", out)
		}
		if !slices.ContainsFunc(h.git.commands, func(c string) bool {
			return strings.HasPrefix(c, "worktree add --no-track -b ") && strings.HasSuffix(c, " old")
		}) {
			t.Errorf("git commands = %q", h.git.commands)
		}
	})

	t.Run("no repo creates only the task", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, envOptions{tty: true})
		h.prompter.newOut = app.NewTaskOptions{Task: "t3"}
		if got := h.run("new"); got != 0 {
			t.Fatalf("exit code = %d\nstderr: %s", got, h.stderr.String())
		}
		if got := h.stdout.String(); strings.Count(got, "\n") != 1 {
			t.Errorf("stdout = %q", got)
		}
		if slices.ContainsFunc(h.git.commands, func(c string) bool { return strings.HasPrefix(c, "worktree add") }) {
			t.Error("no worktree must be created")
		}
	})

	t.Run("failure keeps what was created and exits 1", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, envOptions{tty: true})
		if err := os.MkdirAll(filepath.Join(h.root, "repos/r2.git"), 0o755); err != nil {
			t.Fatal(err)
		}
		h.git.failWorktreeAddIn = "r2.git"
		h.prompter.newOut = app.NewTaskOptions{Task: "t3", Repos: []string{"r", "r2"}}
		if got := h.run("new"); got != 1 {
			t.Fatalf("exit code = %d, want 1", got)
		}
		if !strings.Contains(h.stdout.String(), "tasks/t3/r--") {
			t.Errorf("stdout = %q", h.stdout.String())
		}
		for _, want := range []string{"r2", "boom", "1 / 2"} {
			if !strings.Contains(h.stderr.String(), want) {
				t.Errorf("stderr %q lacks %q", h.stderr.String(), want)
			}
		}
	})

	t.Run("abort", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, envOptions{tty: true})
		h.prompter.err = prompt.ErrAborted
		if got := h.run("new"); got != cli.ExitInterrupt {
			t.Errorf("exit code = %d", got)
		}
	})

	for _, tt := range []struct {
		name string
		env  envOptions
		args []string
		want int
	}{
		{"not a terminal", envOptions{}, []string{"new"}, 2},
		{"with an argument", envOptions{tty: true}, []string{"new", "t3"}, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, tt.env)
			if got := h.run(tt.args...); got != tt.want {
				t.Errorf("exit code = %d, want %d", got, tt.want)
			}
			if h.prompter.newCalls != 0 {
				t.Error("prompter must not be called")
			}
		})
	}
}

func TestSkillInstall(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		env       envOptions
		args      []string
		chosen    []string
		wantAsked bool
		wantDirs  []string
	}{
		{"targets given", envOptions{}, []string{"skill", "install", "codex", "claude", "codex"}, nil, false, []string{".claude/skills", ".agents/skills"}},
		{"chosen interactively", envOptions{tty: true}, []string{"skill", "install"}, []string{"codex"}, true, []string{".agents/skills"}},
		{"nothing chosen", envOptions{tty: true}, []string{"skill", "install"}, nil, true, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, tt.env)
			h.prompter.skillTargetsOut = tt.chosen
			if got := h.run(tt.args...); got != 0 {
				t.Fatalf("exit code = %d\nstderr: %s", got, h.stderr.String())
			}
			if asked := h.prompter.skillTargetCalls > 0; asked != tt.wantAsked {
				t.Errorf("asked = %v, want %v", asked, tt.wantAsked)
			}
			for _, dir := range []string{".claude/skills", ".agents/skills"} {
				_, err := os.Stat(filepath.Join(h.root, dir, "ws-task/SKILL.md"))
				if want := slices.Contains(tt.wantDirs, dir); (err == nil) != want {
					t.Errorf("%s installed = %v, want %v", dir, err == nil, want)
				}
			}
		})
	}
}
