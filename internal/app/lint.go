package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ayame113/my-workspace-manager/internal/fsx"
	"github.com/ayame113/my-workspace-manager/internal/gitexec"
	"github.com/ayame113/my-workspace-manager/internal/layout"
)

type Issue struct {
	Task     string
	Worktree string
	Kind     string
	Message  string
	Fix      string
	fix      func(context.Context) error
}

type FixOutcome int

const (
	FixNotRequested FixOutcome = iota
	Fixed
	NeedsManual
	FixFailed
)

type IssueResult struct {
	Issue   Issue
	Outcome FixOutcome
	Failure string
}

type LintSummary struct {
	Total      int
	Unresolved int
}

type issueCollector struct {
	issues []Issue
}

func (c *issueCollector) add(task, worktree, kind, message, fix string, action func(context.Context) error) {
	c.issues = append(c.issues, Issue{Task: task, Worktree: worktree, Kind: kind, Message: message, Fix: fix, fix: action})
}

func (a *App) readGitDir(ctx context.Context, worktree string) (string, bool, error) {
	// .git が無いディレクトリでは、親ディレクトリの repo を拾わないようにする
	if !fsx.Exists(worktree + "/.git") {
		return "", false, nil
	}
	result, err := a.git.Run(ctx, worktree, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", false, err
	}
	return result.Stdout, result.ExitCode == 0, nil
}

func (a *App) lintWorktree(ctx context.Context, c *issueCollector, task, directory, name string, readonly bool) error {
	label := name
	if readonly {
		label = "_readonly/" + name
	}
	manual := func(kind, message, fix string) {
		c.add(task, label, kind, message, "手動対応: "+fix, nil)
	}
	parsed, ok := layout.ParseWorktreeName(name)
	if !ok {
		manual("naming", "`<repo>--<slug>` 形式のディレクトリ名ではありません", "名前を確認し、不要なら削除")
		return nil
	}
	gitdir, ok, err := a.readGitDir(ctx, directory)
	if err != nil {
		return err
	}
	if !ok {
		manual("broken-worktree", "git が worktree として認識しません", "不要なら削除し、必要なら ws worktree add で作り直す")
		return nil
	}
	actualBare, actualErr := fsx.RealPath(filepath.Dir(filepath.Dir(gitdir)))
	expectedBare, expectedErr := fsx.RealPath(a.layout.BarePath(parsed.Repo))
	if actualErr != nil || expectedErr != nil || actualBare != expectedBare {
		shown := actualBare
		if actualErr != nil {
			shown = "不明"
		}
		manual("naming", fmt.Sprintf("repo 部分 '%s' と実際の bare repo(%s)が一致しません", parsed.Repo, shown), "ディレクトリ名を実際の repo に合わせる")
	}
	branch, err := gitexec.CurrentBranch(ctx, a.git, directory)
	if err != nil {
		return err
	}
	switch {
	case readonly:
		return a.lintReadonly(ctx, directory, branch, manual)
	case branch != "":
		if layout.BranchSlug(branch) != parsed.Slug {
			manual("dir-branch-mismatch", fmt.Sprintf("現在のブランチ '%s' の slug が名前と異なります", branch), "ブランチを戻すか worktree を作り直す")
		}
		upstream, err := a.git.Run(ctx, directory, "rev-parse", "--abbrev-ref", "@{upstream}")
		if err != nil {
			return err
		}
		if upstream.ExitCode == 0 && upstream.Stdout != "origin/"+branch {
			c.add(task, label, "upstream-mismatch",
				fmt.Sprintf("upstream が別名ブランチ '%s' を指しています", upstream.Stdout),
				"git branch --unset-upstream を実行",
				func(ctx context.Context) error {
					_, err := gitexec.Output(ctx, a.git, directory, "branch", "--unset-upstream")
					return err
				})
		}
	default:
		manual("dir-branch-mismatch", "detached HEAD です(作業用 worktree はブランチ固定)", "ブランチを checkout する")
	}
	return nil
}

func (a *App) lintReadonly(ctx context.Context, directory, branch string, manual func(kind, message, fix string)) error {
	if branch != "" {
		manual("readonly", fmt.Sprintf("ブランチ '%s' を checkout しています", branch), "detached に戻す(git switch --detach)")
	}
	count, err := gitexec.Output(ctx, a.git, directory, "rev-list", "--count", "HEAD", "--not", "--remotes=origin")
	if unpushed, parseErr := strconv.Atoi(count); err == nil && parseErr == nil && unpushed > 0 {
		manual("readonly", fmt.Sprintf("origin に無いコミットが %d 件あります", unpushed), "内容を確認する")
	}
	dirty, err := gitexec.IsDirty(ctx, a.git, directory)
	if err != nil {
		return err
	}
	if dirty {
		manual("readonly", "未コミット変更があります", "内容を確認する")
	}
	return nil
}

func (a *App) lintTask(ctx context.Context, c *issueCollector, task string) error {
	directory := a.layout.TaskDir(task)
	if !layout.IsValidTaskName(task) {
		c.add(task, "-", "naming", "task 名が規則([a-z0-9-]、連続・先頭末尾ハイフン不可)に違反しています",
			"手動対応: task 名の変更はしない方針なので内容を確認する", nil)
	}
	names, err := fsx.SubdirNames(directory)
	if err != nil {
		return err
	}
	for _, name := range names {
		if name != "_readonly" {
			if err := a.lintWorktree(ctx, c, task, directory+"/"+name, name, false); err != nil {
				return err
			}
			continue
		}
		readonlyNames, err := fsx.SubdirNames(directory + "/_readonly")
		if err != nil {
			return err
		}
		for _, readonlyName := range readonlyNames {
			if err := a.lintWorktree(ctx, c, task, directory+"/_readonly/"+readonlyName, readonlyName, true); err != nil {
				return err
			}
		}
	}
	return nil
}

// lintDanglingRegistrations reports registered worktrees whose directory is gone and for which accepts(task) holds.
// task is "" for a worktree outside tasks/.
func (a *App) lintDanglingRegistrations(ctx context.Context, c *issueCollector, accepts func(task string) bool) error {
	if !fsx.Exists(a.layout.ReposRoot()) {
		return nil
	}
	entries, err := fsx.SubdirNames(a.layout.ReposRoot())
	if err != nil {
		return err
	}
	for _, entry := range entries {
		bare := a.layout.ReposRoot() + "/" + entry
		listing, err := a.git.Run(ctx, bare, "worktree", "list", "--porcelain")
		if err != nil {
			return err
		}
		if listing.ExitCode != 0 {
			continue
		}
		first := true
		for line := range strings.SplitSeq(listing.Stdout, "\n") {
			worktree, ok := strings.CutPrefix(line, "worktree ")
			if !ok {
				continue
			}
			// 先頭は bare repo 自身
			if first {
				first = false
				continue
			}
			if fsx.Exists(worktree) {
				continue
			}
			task, _ := layout.TaskUnder(a.layout.TasksRoot(), worktree)
			if !accepts(task) {
				continue
			}
			shownTask := task
			if shownTask == "" {
				shownTask = "-"
			}
			c.add(shownTask, filepath.Base(worktree), "broken-worktree",
				"git に登録されていますがディレクトリがありません: "+worktree,
				"git worktree prune を実行",
				func(ctx context.Context) error {
					_, err := gitexec.Output(ctx, a.git, bare, "worktree", "prune")
					return err
				})
		}
	}
	return nil
}

// LintTask checks one task. Dangling registrations are limited to that task.
func (a *App) LintTask(ctx context.Context, task string, fix bool, report func(IssueResult)) (LintSummary, error) {
	c := &issueCollector{}
	if err := a.lintTask(ctx, c, task); err != nil {
		return LintSummary{}, err
	}
	if err := a.lintDanglingRegistrations(ctx, c, func(found string) bool { return found == task }); err != nil {
		return LintSummary{}, err
	}
	return a.resolveIssues(ctx, c.issues, fix, report)
}

// LintWorkspace checks every task, plus dangling registrations that belong to no task.
func (a *App) LintWorkspace(ctx context.Context, fix bool, report func(IssueResult)) (LintSummary, error) {
	c := &issueCollector{}
	if fsx.Exists(a.layout.TasksRoot()) {
		tasks, err := fsx.SubdirNames(a.layout.TasksRoot())
		if err != nil {
			return LintSummary{}, err
		}
		for _, task := range tasks {
			if err := a.lintTask(ctx, c, task); err != nil {
				return LintSummary{}, err
			}
		}
	}
	if err := a.lintDanglingRegistrations(ctx, c, func(string) bool { return true }); err != nil {
		return LintSummary{}, err
	}
	return a.resolveIssues(ctx, c.issues, fix, report)
}

func (a *App) resolveIssues(ctx context.Context, issues []Issue, fix bool, report func(IssueResult)) (LintSummary, error) {
	summary := LintSummary{Total: len(issues)}
	for _, issue := range issues {
		result := IssueResult{Issue: issue}
		switch {
		case !fix:
			summary.Unresolved++
		case issue.fix == nil:
			result.Outcome = NeedsManual
			summary.Unresolved++
		default:
			if err := issue.fix(ctx); err != nil {
				if ctx.Err() != nil {
					return summary, err
				}
				result.Outcome = FixFailed
				result.Failure = strings.TrimSpace(err.Error())
				summary.Unresolved++
			} else {
				result.Outcome = Fixed
			}
		}
		report(result)
	}
	return summary, nil
}
