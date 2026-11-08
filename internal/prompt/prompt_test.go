package prompt

import (
	"context"
	"errors"
	"io"
	"os"
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
	root := t.TempDir()
	a := app.New(app.Config{
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
	return New(a, in, io.Discard)
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
