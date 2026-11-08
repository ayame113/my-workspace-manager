package app

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ayame113/my-workspace-manager/internal/gitexec"
	"github.com/ayame113/my-workspace-manager/internal/layout"
)

const (
	PullUpdated    = "updated"
	PullUpToDate   = "up-to-date"
	PullSkipped    = "skipped"
	PullError      = "error"
	pullNoUpstream = "upstream なし"
	pullDirty      = "未コミット変更あり"
)

type PullEntry struct {
	Kind   string
	Name   string
	Status string
	Detail string
}

type PullResult struct {
	Total  int
	Failed int
}

type pullOutcome struct {
	status string
	detail string
}

// failureOutcome turns an expected failure into an error outcome. Cancellation is not expected and propagates.
func failureOutcome(ctx context.Context, err error) (pullOutcome, error) {
	if ctx.Err() != nil {
		return pullOutcome{}, err
	}
	return pullOutcome{status: PullError, detail: strings.TrimSpace(err.Error())}, nil
}

func (a *App) updateReadonly(ctx context.Context, bare, worktree, slug string) (pullOutcome, error) {
	if dirty, err := gitexec.IsDirty(ctx, a.git, worktree); err != nil || dirty {
		return pullOutcome{status: PullSkipped, detail: pullDirty}, err
	}
	out, err := gitexec.Output(ctx, a.git, bare, "for-each-ref", "--format=%(refname:strip=3)", "refs/remotes/origin/")
	if err != nil {
		return pullOutcome{}, err
	}
	var matches []string
	for name := range strings.SplitSeq(out, "\n") {
		if name != "" && name != "HEAD" && layout.BranchSlug(name) == slug {
			matches = append(matches, name)
		}
	}
	if len(matches) != 1 {
		return pullOutcome{status: PullError, detail: fmt.Sprintf("slug に一致する origin ブランチが %d 件: %s", len(matches), strings.Join(matches, " "))}, nil
	}
	target := "origin/" + matches[0]
	head, err := gitexec.Output(ctx, a.git, worktree, "rev-parse", "HEAD")
	if err != nil {
		return pullOutcome{}, err
	}
	targetRev, err := gitexec.Output(ctx, a.git, worktree, "rev-parse", target)
	if err != nil {
		return pullOutcome{}, err
	}
	if head == targetRev {
		return pullOutcome{status: PullUpToDate, detail: target}, nil
	}
	if _, err := gitexec.Output(ctx, a.git, worktree, "checkout", "--quiet", "--detach", target); err != nil {
		return pullOutcome{}, err
	}
	short, err := gitexec.ShortHead(ctx, a.git, worktree)
	return pullOutcome{status: PullUpdated, detail: fmt.Sprintf("%s (%s)", target, short)}, err
}

func (a *App) updateWork(ctx context.Context, worktree string) (pullOutcome, error) {
	branch, err := gitexec.CurrentBranch(ctx, a.git, worktree)
	if err != nil {
		return pullOutcome{}, err
	}
	if branch == "" {
		return pullOutcome{status: PullSkipped, detail: "detached HEAD"}, nil
	}
	if dirty, err := gitexec.IsDirty(ctx, a.git, worktree); err != nil || dirty {
		return pullOutcome{status: PullSkipped, detail: pullDirty}, err
	}
	hasUpstream, err := gitexec.Succeeds(ctx, a.git, worktree, "rev-parse", "--verify", "--quiet", "@{upstream}")
	if err != nil {
		return pullOutcome{}, err
	}
	if !hasUpstream {
		return pullOutcome{status: PullSkipped, detail: pullNoUpstream}, nil
	}
	before, err := gitexec.ShortHead(ctx, a.git, worktree)
	if err != nil {
		return pullOutcome{}, err
	}
	merge, err := a.git.Run(ctx, worktree, "merge", "--quiet", "--ff-only", "@{upstream}")
	if err != nil {
		return pullOutcome{}, err
	}
	if merge.ExitCode != 0 {
		// 先頭は hint: の説明が続くことがあるので、結論の最終行(fatal: ...)を使う
		lines := strings.Split(strings.TrimSpace(merge.Stderr), "\n")
		return pullOutcome{status: PullSkipped, detail: branch + ": " + lines[len(lines)-1]}, nil
	}
	after, err := gitexec.ShortHead(ctx, a.git, worktree)
	if err != nil {
		return pullOutcome{}, err
	}
	if after == before {
		return pullOutcome{status: PullUpToDate, detail: branch}, nil
	}
	return pullOutcome{status: PullUpdated, detail: fmt.Sprintf("%s (%s)", branch, after)}, nil
}

// Pull fetches each repo used by the (already resolved) task once, then reports every worktree through report as it is processed.
func (a *App) Pull(ctx context.Context, task string, report func(PullEntry)) (PullResult, error) {
	entries, err := a.worktreeEntries(task)
	if err != nil {
		return PullResult{}, err
	}
	var repos []string
	for _, entry := range entries {
		repo := layout.SplitWorktreeName(entry.Name).Repo
		if !slices.Contains(repos, repo) {
			repos = append(repos, repo)
		}
	}
	fetchFailures := map[string]pullOutcome{}
	for _, repo := range repos {
		bare, err := a.requireBare(ctx, repo)
		if err == nil {
			_, err = gitexec.Output(ctx, a.git, bare, "fetch", "--quiet", "origin", "--prune")
		}
		if err != nil {
			if fetchFailures[repo], err = failureOutcome(ctx, err); err != nil {
				return PullResult{}, err
			}
		}
	}
	result := PullResult{Total: len(entries)}
	for _, entry := range entries {
		parsed := layout.SplitWorktreeName(entry.Name)
		path := entry.path(a.layout, task)
		outcome, failed := fetchFailures[parsed.Repo]
		if !failed {
			if entry.Kind == KindReadonly {
				outcome, err = a.updateReadonly(ctx, a.layout.BarePath(parsed.Repo), path, parsed.Slug)
			} else {
				outcome, err = a.updateWork(ctx, path)
			}
			if err != nil {
				if outcome, err = failureOutcome(ctx, err); err != nil {
					return result, err
				}
			}
		}
		if outcome.status == PullError {
			result.Failed++
		}
		report(PullEntry{Kind: entry.Kind, Name: entry.Name, Status: outcome.status, Detail: outcome.detail})
	}
	return result, nil
}
