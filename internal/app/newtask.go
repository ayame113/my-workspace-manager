package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/ayame113/my-workspace-manager/internal/fsx"
	"github.com/ayame113/my-workspace-manager/internal/gitexec"
	"github.com/ayame113/my-workspace-manager/internal/layout"
)

const maxRandomNameAttempts = 100

func (a *App) randomNameNotTaken(taken func(name string) (bool, error), prefix string) (string, error) {
	for range maxRandomNameAttempts {
		name := prefix + layout.RandomName(a.intn)
		isTaken, err := taken(name)
		if err != nil {
			return "", err
		}
		if !isTaken {
			return name, nil
		}
	}
	return "", errors.New("重ならないランダムな名前を作れませんでした")
}

// RandomTaskName returns a task name that has no directory under tasks/.
// The name is prefixed with the repo names (see layout.RepoTaskPrefix), so the task says what it is for.
func (a *App) RandomTaskName(repos []string) (string, error) {
	prefix := layout.RepoTaskPrefix(repos)
	if prefix != "" {
		prefix += "-"
	}
	return a.randomNameNotTaken(func(name string) (bool, error) {
		return fsx.Exists(a.layout.TaskDir(name)), nil
	}, prefix)
}

// RandomBranchName returns a branch name that exists neither locally nor on origin in any of repos.
func (a *App) RandomBranchName(ctx context.Context, repos []string) (string, error) {
	bares := make([]string, len(repos))
	for i, repo := range repos {
		bare, err := a.requireBare(ctx, repo)
		if err != nil {
			return "", err
		}
		bares[i] = bare
	}
	return a.randomNameNotTaken(func(name string) (bool, error) {
		for _, bare := range bares {
			for _, ref := range []string{"refs/heads/", "refs/remotes/origin/"} {
				exists, err := gitexec.RefExists(ctx, a.git, bare, ref+name)
				if err != nil || exists {
					return exists, err
				}
			}
		}
		return false, nil
	}, "")
}

// ValidateNewTaskName accepts a valid task name that has no directory yet.
func (a *App) ValidateNewTaskName(task string) error {
	if err := layout.ValidateTaskName(task); err != nil {
		return err
	}
	if fsx.Exists(a.layout.TaskDir(task)) {
		return fmt.Errorf("task '%s' は既に存在します", task)
	}
	return nil
}

type NewTaskOptions struct {
	Task  string
	Repos []string
}

type CreatedWorktree struct {
	Repo string
	Path string
}

type NewTaskWithWorktreesResult struct {
	Dir       string
	Branch    string
	Worktrees []CreatedWorktree
}

// NewTaskWithWorktrees creates a task and, in each of repos, a worktree on one new branch based on the default base.
// Everything is checked before the first change. If a worktree fails midway, what was created stays and is returned along with the error.
func (a *App) NewTaskWithWorktrees(ctx context.Context, task string, repos []string) (NewTaskWithWorktreesResult, error) {
	if err := a.ValidateNewTaskName(task); err != nil {
		return NewTaskWithWorktreesResult{}, err
	}
	bases := make([]string, len(repos))
	for i, repo := range repos {
		if _, err := a.requireBare(ctx, repo); err != nil {
			return NewTaskWithWorktreesResult{}, err
		}
		base, err := a.DefaultBase(ctx, repo)
		if err != nil {
			return NewTaskWithWorktreesResult{}, err
		}
		if base == "" {
			return NewTaskWithWorktreesResult{}, fmt.Errorf("repo '%s' に起点にできる origin/HEAD も origin/main もありません", repo)
		}
		bases[i] = base
	}
	branch := ""
	if len(repos) > 0 {
		var err error
		if branch, err = a.RandomBranchName(ctx, repos); err != nil {
			return NewTaskWithWorktreesResult{}, err
		}
	}
	created, err := a.NewTask(task)
	if err != nil {
		return NewTaskWithWorktreesResult{}, err
	}
	result := NewTaskWithWorktreesResult{Dir: created.Dir, Branch: branch}
	for i, repo := range repos {
		path, err := a.Add(ctx, AddOptions{Task: task, Repo: repo, Branch: branch, Base: bases[i], Create: true})
		if err != nil {
			return result, &WorktreeCreateError{Repo: repo, Err: err}
		}
		result.Worktrees = append(result.Worktrees, CreatedWorktree{Repo: repo, Path: path})
	}
	return result, nil
}

type WorktreeCreateError struct {
	Repo string
	Err  error
}

func (e *WorktreeCreateError) Error() string {
	return fmt.Sprintf("repo '%s' の worktree を作成できませんでした: %v", e.Repo, e.Err)
}

func (e *WorktreeCreateError) Unwrap() error { return e.Err }
