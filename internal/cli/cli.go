package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/ayame113/my-workspace-manager/internal/app"
	"github.com/ayame113/my-workspace-manager/internal/gitexec"
	"github.com/ayame113/my-workspace-manager/internal/layout"
	"github.com/ayame113/my-workspace-manager/internal/prompt"
)

const (
	ExitOK        = 0
	ExitFailure   = 1
	ExitUsage     = 2
	ExitInterrupt = 130
)

// Env gathers everything Run takes from the process so that tests can replace it.
type Env struct {
	Stdin            io.Reader
	Stdout           io.Writer
	Stderr           io.Writer
	Getenv           func(string) string
	Getwd            func() (string, error)
	StdinIsTerminal  bool
	StderrIsTerminal bool
	Git              gitexec.Runner
	// Prompter が nil なら、対話が必要なときに huh で実装したものを使う
	Prompter Prompter
}

func OSEnv() Env {
	return Env{
		Stdin:            os.Stdin,
		Stdout:           os.Stdout,
		Stderr:           os.Stderr,
		Getenv:           os.Getenv,
		Getwd:            os.Getwd,
		StdinIsTerminal:  term.IsTerminal(int(os.Stdin.Fd())),
		StderrIsTerminal: term.IsTerminal(int(os.Stderr.Fd())),
		Git:              gitexec.New(),
	}
}

type Prompter interface {
	ChooseTask(ctx context.Context) (string, error)
	New(ctx context.Context) (app.NewTaskOptions, error)
	Add(ctx context.Context, partial app.AddOptions) (app.AddOptions, error)
	Rename(ctx context.Context, partial app.RenameOptions) (app.RenameOptions, error)
	ChooseSkillTargets(ctx context.Context) ([]string, error)
	Describe(ctx context.Context, task, current string) (string, error)
	PickTask(ctx context.Context, choices []prompt.Choice) (string, error)
}

type usageError struct{ error }

func usagef(format string, args ...any) error {
	return usageError{fmt.Errorf(format, args...)}
}

// exitError ends the process with code without printing anything further.
type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

type runner struct {
	env     Env
	app     *app.App
	started bool
}

func version() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "(devel)"
}

func (r *runner) interactive() bool {
	return r.env.StdinIsTerminal && r.env.StderrIsTerminal
}

func (r *runner) prompter() Prompter {
	if r.env.Prompter != nil {
		return r.env.Prompter
	}
	return prompt.New(r.app, r.env.Stdin, r.env.Stderr)
}

// Run executes the ws command line (without the program name) and returns the exit code.
// 0: success, 1: runtime failure (including unresolved lint issues), 2: usage error, 130: interrupted.
func Run(ctx context.Context, args []string, env Env) int {
	if env.Git == nil {
		env.Git = gitexec.New()
	}
	r := &runner{
		env: env,
		app: app.New(app.Config{
			Layout: layout.FromEnv(env.Getenv),
			Git:    env.Git,
			Getenv: env.Getenv,
			Getwd:  env.Getwd,
		}),
	}
	root := r.rootCommand()
	root.SetArgs(args)
	root.SetOut(env.Stdout)
	root.SetErr(env.Stderr)
	err := root.ExecuteContext(ctx)
	return r.exitCode(root, args, err)
}

func (r *runner) exitCode(root *cobra.Command, args []string, err error) int {
	stderr := r.env.Stderr
	if exit, ok := errors.AsType[exitError](err); ok {
		return exit.code
	}
	if err == nil {
		return ExitOK
	}
	if _, ok := errors.AsType[usageError](err); ok || !r.started {
		fmt.Fprintf(stderr, "error: %v\n", err)
		target, _, _ := root.Find(args)
		fmt.Fprintf(stderr, "詳しくは '%s --help' を参照してください\n", target.CommandPath())
		return ExitUsage
	}
	if errors.Is(err, prompt.ErrAborted) {
		fmt.Fprintln(stderr, "ws: 中断しました")
		return ExitInterrupt
	}
	if gitErr, ok := errors.AsType[*gitexec.ExitError](err); ok {
		fmt.Fprint(stderr, gitErr.Stderr)
	} else {
		fmt.Fprintf(stderr, "ws: %v\n", err)
	}
	return ExitFailure
}
