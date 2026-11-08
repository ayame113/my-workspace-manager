package prompt

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ayame113/my-workspace-manager/internal/app"
	"github.com/ayame113/my-workspace-manager/internal/gitexec"
	"github.com/ayame113/my-workspace-manager/internal/layout"
)

func newPrompter(t *testing.T, in io.Reader) *Prompter {
	t.Helper()
	p, _ := newPrompterAt(t, in)
	return p
}

func firstWords(int) int { return 0 }

func newPrompterAt(t *testing.T, in io.Reader) (*Prompter, string) {
	t.Helper()
	return newPrompterWithIntN(t, in, firstWords)
}

func newPrompterWithIntN(t *testing.T, in io.Reader, intn func(int) int) (*Prompter, string) {
	t.Helper()
	root := t.TempDir()
	a := app.New(app.Config{
		IntN: intn,
		Layout: layout.FromEnv(func(name string) string {
			if name == "WS_ROOT" {
				return root
			}
			return ""
		}),
		Git:    gitexec.New(),
		Getenv: func(string) string { return "" },
		Getwd:  func() (string, error) { return root, nil },
	})
	return New(a, in, io.Discard), root
}

func acceptAny(string) error { return nil }

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestInputReturnsTypedValue(t *testing.T) {
	p := newPrompter(t, strings.NewReader("abc\r"))
	got, err := p.input(testContext(t), "title", "init-", acceptAny)
	if err != nil {
		t.Fatal(err)
	}
	if got != "init-abc" {
		t.Errorf("got %q, want %q", got, "init-abc")
	}
}

func TestChooseReturnsSelectedOption(t *testing.T) {
	p := newPrompter(t, strings.NewReader("\x1b[B\r"))
	got, err := p.choose(testContext(t), "title", []string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "b" {
		t.Errorf("got %q, want %q", got, "b")
	}
}

func TestChooseModeReturnsSelectedMode(t *testing.T) {
	p := newPrompter(t, strings.NewReader("\x1b[B\r"))
	got, err := p.chooseMode(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	if got != modeNew {
		t.Errorf("got %q, want %q", got, modeNew)
	}
}

func TestRenameReturnsNewName(t *testing.T) {
	p := newPrompter(t, strings.NewReader("-new\r"))
	got, err := p.Rename(testContext(t), app.RenameOptions{Task: "t", Repo: "r", Branch: "old"})
	if err != nil {
		t.Fatal(err)
	}
	if got.To != "old-new" {
		t.Errorf("To = %q, want %q", got.To, "old-new")
	}
}

func TestAbortReturnsErrAborted(t *testing.T) {
	p := newPrompter(t, strings.NewReader("\x03"))
	_, err := p.input(testContext(t), "title", "", acceptAny)
	if !errors.Is(err, ErrAborted) {
		t.Errorf("err = %v, want ErrAborted", err)
	}
}

// A *os.File input lets bubbletea cancel its reader when a form ends, so a finished form does not consume the next form's input.
func TestSequentialFormsDoNotStealInput(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		reader.Close()
		writer.Close()
	})
	p := newPrompter(t, reader)
	var got []string
	for _, typed := range []string{"one\r", "two\r"} {
		if _, err := writer.WriteString(typed); err != nil {
			t.Fatal(err)
		}
		value, err := p.input(testContext(t), "title", "", acceptAny)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, value)
	}
	if !slices.Equal(got, []string{"one", "two"}) {
		t.Errorf("got %q", got)
	}
}

func randomName() string { return layout.RandomName(firstWords) }

func TestChooseManyReturnsToggledOptions(t *testing.T) {
	p := newPrompter(t, strings.NewReader(" \x1b[B\x1b[B \r"))
	got, err := p.chooseMany(testContext(t), "title", []string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"a", "c"}) {
		t.Errorf("got %q", got)
	}
}

func TestChooseManyAllowsNoSelection(t *testing.T) {
	p := newPrompter(t, strings.NewReader("\r"))
	got, err := p.chooseMany(testContext(t), "title", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got %q", got)
	}
}

func TestChooseTaskFailsWithoutTasks(t *testing.T) {
	p := newPrompter(t, strings.NewReader("\r"))
	if _, err := p.ChooseTask(testContext(t)); err == nil || !strings.Contains(err.Error(), "ws new") {
		t.Errorf("err = %v", err)
	}
}

func TestChooseTaskListsNewestFirst(t *testing.T) {
	p, root := newPrompterAt(t, strings.NewReader("\x1b[B\r"))
	now := time.Now()
	for task, age := range map[string]time.Duration{"a": time.Hour, "b": 2 * time.Hour, "c": 0} {
		dir := root + "/tasks/" + task
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(dir, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := p.ChooseTask(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	if got != "a" {
		t.Errorf("got %q, want the second newest", got)
	}
}

func TestNewAdoptsRandomTaskNameOnEnter(t *testing.T) {
	p := newPrompter(t, strings.NewReader("\r\r"))
	got, err := p.New(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	if got.Task != randomName() || len(got.Repos) != 0 {
		t.Errorf("got %+v, want task %q and no repos", got, randomName())
	}
}

func TestNewAsksReposThenTaskNameWithRepoPrefix(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		reader.Close()
		writer.Close()
	})
	p, root := newPrompterAt(t, reader)
	for _, repo := range []string{"alpha", "My_Repo"} {
		if err := os.MkdirAll(root+"/repos/"+repo+".git", 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// 次のフォームの入力を先のフォームに読まれないよう、repo 選択が終わるのを待ってから残りを書く
	go func() {
		writer.WriteString(" \r")
		time.Sleep(500 * time.Millisecond)
		writer.WriteString("\r\r")
	}()
	got, err := p.New(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	want := app.NewTaskOptions{Task: "my-repo-" + randomName(), Repos: []string{"My_Repo"}}
	if got.Task != want.Task || !slices.Equal(got.Repos, want.Repos) || got.Description != want.Description {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestNewRegeneratesCollidingInitialTaskName(t *testing.T) {
	calls := 0
	intn := func(int) int {
		calls++
		if calls <= 2 {
			return 0
		}
		return 1
	}
	p, root := newPrompterWithIntN(t, strings.NewReader("\r\r"), intn)
	if err := os.MkdirAll(root+"/tasks/"+randomName(), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := p.New(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	if want := layout.RandomName(func(int) int { return 1 }); got.Task != want {
		t.Errorf("got %q, want regenerated %q", got.Task, want)
	}
}

func TestAddNewBranchDefaultsToRandomName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	p, root := newPrompterAt(t, strings.NewReader("\r"))
	if out, err := exec.Command("git", "init", "--quiet", "--bare", root+"/repos/r.git").CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got, err := p.Add(testContext(t), app.AddOptions{Task: "t", Repo: "r", Create: true, Base: "origin/main"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Branch != randomName() {
		t.Errorf("Branch = %q, want %q", got.Branch, randomName())
	}
}

func TestRenameKeepsOldNameAsInitialValue(t *testing.T) {
	p := newPrompter(t, strings.NewReader("\r"))
	got, err := p.Rename(testContext(t), app.RenameOptions{Task: "t", Repo: "r", Branch: "old"})
	if err != nil {
		t.Fatal(err)
	}
	if got.To != "old" {
		t.Errorf("To = %q", got.To)
	}
}

func TestDescribeStartsFromCurrent(t *testing.T) {
	p := newPrompter(t, strings.NewReader(" and more\r"))
	got, err := p.Describe(testContext(t), "t", "current")
	if err != nil {
		t.Fatal(err)
	}
	if got != "current and more" {
		t.Errorf("got %q", got)
	}
}
