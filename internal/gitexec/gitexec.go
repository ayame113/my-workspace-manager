package gitexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

type Runner interface {
	// Run executes git in dir (or the current directory when dir is empty).
	// A non-zero exit is reported in Result; err is set only when git could not run.
	Run(ctx context.Context, dir string, args ...string) (Result, error)
}

type ExitError struct {
	Stderr string
}

func (e *ExitError) Error() string { return e.Stderr }

type execRunner struct{}

func New() Runner { return execRunner{} }

func (execRunner) Run(ctx context.Context, dir string, args ...string) (Result, error) {
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := Result{Stdout: strings.TrimSpace(stdout.String()), Stderr: stderr.String()}
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("git を実行できません: %w", err)
	}
	return result, nil
}

// Output returns stdout, or an *ExitError carrying git's stderr on a non-zero exit.
func Output(ctx context.Context, r Runner, dir string, args ...string) (string, error) {
	result, err := r.Run(ctx, dir, args...)
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", &ExitError{Stderr: result.Stderr}
	}
	return result.Stdout, nil
}

func Succeeds(ctx context.Context, r Runner, dir string, args ...string) (bool, error) {
	result, err := r.Run(ctx, dir, args...)
	return err == nil && result.ExitCode == 0, err
}

func RefExists(ctx context.Context, r Runner, bare, ref string) (bool, error) {
	return Succeeds(ctx, r, bare, "show-ref", "--verify", "--quiet", ref)
}

func IsDirty(ctx context.Context, r Runner, worktree string) (bool, error) {
	out, err := Output(ctx, r, worktree, "status", "--porcelain")
	return out != "", err
}

// CurrentBranch returns "" for a detached HEAD.
func CurrentBranch(ctx context.Context, r Runner, worktree string) (string, error) {
	result, err := r.Run(ctx, worktree, "symbolic-ref", "-q", "--short", "HEAD")
	if err != nil || result.ExitCode != 0 {
		return "", err
	}
	return result.Stdout, nil
}

func ShortHead(ctx context.Context, r Runner, worktree string) (string, error) {
	return Output(ctx, r, worktree, "rev-parse", "--short", "HEAD")
}

func ApplyRepoConfig(ctx context.Context, r Runner, bare string) error {
	// 手で切ったブランチの upstream が base(origin/main 等)になると、エディタの push/sync で base に push してしまう
	_, err := Output(ctx, r, bare, "config", "branch.autoSetupMerge", "simple")
	return err
}
