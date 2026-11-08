package app

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/ayame113/my-workspace-manager/internal/fsx"
	"github.com/ayame113/my-workspace-manager/internal/gitexec"
	"github.com/ayame113/my-workspace-manager/internal/layout"
)

// TaskNames lists the existing tasks, most recently active first.
func (a *App) TaskNames() ([]string, error) {
	if !fsx.Exists(a.layout.TasksRoot()) {
		return nil, nil
	}
	names, err := fsx.SubdirNames(a.layout.TasksRoot())
	if err != nil {
		return nil, err
	}
	updated := make(map[string]time.Time, len(names))
	for _, name := range names {
		entries, err := a.worktreeEntries(name)
		if err != nil {
			return nil, err
		}
		updated[name] = a.taskActivity(name, entries)
	}
	newestFirst(names, func(name string) time.Time { return updated[name] })
	return names, nil
}

// RepoNames lists the bare repos under repos/.
func (a *App) RepoNames() ([]string, error) {
	if !fsx.Exists(a.layout.ReposRoot()) {
		return nil, nil
	}
	dirs, err := fsx.SubdirNames(a.layout.ReposRoot())
	if err != nil {
		return nil, err
	}
	var repos []string
	for _, dir := range dirs {
		if repo, ok := strings.CutSuffix(dir, ".git"); ok && repo != "" {
			repos = append(repos, repo)
		}
	}
	return repos, nil
}

type Branches struct {
	Local  []string
	Remote []string
}

// All returns local and origin branches without duplicates, sorted.
func (b Branches) All() []string {
	return slices.Compact(slices.Sorted(slices.Values(slices.Concat(b.Local, b.Remote))))
}

func (a *App) forEachRef(ctx context.Context, bare, prefix, format string) ([]string, error) {
	out, err := gitexec.Output(ctx, a.git, bare, "for-each-ref", "--format="+format, prefix)
	if err != nil {
		return nil, err
	}
	var names []string
	for name := range strings.SplitSeq(out, "\n") {
		if name != "" && name != "HEAD" {
			names = append(names, name)
		}
	}
	return names, nil
}

// BranchesOf lists the local and origin branches of a repo, excluding origin/HEAD.
func (a *App) BranchesOf(ctx context.Context, repo string) (Branches, error) {
	bare, err := a.requireBare(ctx, repo)
	if err != nil {
		return Branches{}, err
	}
	local, err := a.forEachRef(ctx, bare, "refs/heads/", "%(refname:strip=2)")
	if err != nil {
		return Branches{}, err
	}
	remote, err := a.forEachRef(ctx, bare, "refs/remotes/origin/", "%(refname:strip=3)")
	if err != nil {
		return Branches{}, err
	}
	return Branches{Local: local, Remote: remote}, nil
}

// DefaultBase returns the ref origin/HEAD points to, else origin/main when it exists, else "".
func (a *App) DefaultBase(ctx context.Context, repo string) (string, error) {
	bare := a.layout.BarePath(repo)
	if head, err := a.git.Run(ctx, bare, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err != nil {
		return "", err
	} else if head.ExitCode == 0 && head.Stdout != "" {
		return head.Stdout, nil
	}
	exists, err := gitexec.RefExists(ctx, a.git, bare, "refs/remotes/origin/main")
	if err != nil || !exists {
		return "", err
	}
	return "origin/main", nil
}

type RenameCandidate struct {
	Repo   string
	Branch string
}

// RenameCandidates lists the work worktrees of a task that have a branch checked out.
func (a *App) RenameCandidates(ctx context.Context, task string) ([]RenameCandidate, error) {
	names, err := listWorktreeNames(a.layout.TaskDir(task))
	if err != nil {
		return nil, err
	}
	var candidates []RenameCandidate
	for _, name := range names {
		parsed, ok := layout.ParseWorktreeName(name)
		if !ok {
			continue
		}
		branch, err := gitexec.CurrentBranch(ctx, a.git, a.layout.TaskDir(task)+"/"+name)
		if err != nil {
			return nil, err
		}
		if branch != "" {
			candidates = append(candidates, RenameCandidate{Repo: parsed.Repo, Branch: branch})
		}
	}
	return candidates, nil
}
