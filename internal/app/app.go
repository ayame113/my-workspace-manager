package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ayame113/my-workspace-manager/internal/fsx"
	"github.com/ayame113/my-workspace-manager/internal/gitexec"
	"github.com/ayame113/my-workspace-manager/internal/layout"
)

type Config struct {
	Layout layout.Layout
	Git    gitexec.Runner
	Getenv func(string) string
	Getwd  func() (string, error)
}

type App struct {
	layout layout.Layout
	git    gitexec.Runner
	getenv func(string) string
	getwd  func() (string, error)
}

func New(cfg Config) *App {
	return &App{layout: cfg.Layout, git: cfg.Git, getenv: cfg.Getenv, getwd: cfg.Getwd}
}

type OutsideTaskError struct {
	TasksRoot string
}

func (e *OutsideTaskError) Error() string {
	return fmt.Sprintf("カレントディレクトリが %s/<task>/ 配下ではありません", e.TasksRoot)
}

func (a *App) taskFromCwd() (string, error) {
	outside := &OutsideTaskError{TasksRoot: a.layout.TasksRoot()}
	tasksRoot, err := fsx.RealPath(a.layout.TasksRoot())
	if err != nil {
		return "", outside
	}
	cwd, err := a.getwd()
	if err != nil {
		return "", fmt.Errorf("カレントディレクトリを取得できません: %w", err)
	}
	cwd, err = fsx.RealPath(cwd)
	if err != nil {
		return "", fmt.Errorf("カレントディレクトリを解決できません: %w", err)
	}
	task, ok := layout.TaskUnder(tasksRoot, cwd)
	if !ok {
		return "", outside
	}
	return task, nil
}

// ResolveTask resolves a --task value. An empty value or "." means the task containing the current directory.
// A *OutsideTaskError is returned when that fails.
func (a *App) ResolveTask(task string) (string, error) {
	if task == "" || task == "." {
		fromCwd, err := a.taskFromCwd()
		if err != nil {
			if task == "" {
				return "", fmt.Errorf("%w。--task <task> を指定してください", err)
			}
			return "", err
		}
		task = fromCwd
	}
	if err := layout.ValidateTaskName(task); err != nil {
		return "", err
	}
	if !fsx.Exists(a.layout.TaskDir(task)) {
		return "", fmt.Errorf("task '%s' がありません。先に 'ws new %s' を実行してください", task, task)
	}
	return task, nil
}

// RefuseInsideAgent rejects commands that may wait for an ssh passphrase when run by an agent.
func (a *App) RefuseInsideAgent(what string) error {
	if a.getenv("CLAUDECODE") != "" {
		return fmt.Errorf("%s は ssh のパスフレーズ入力が必要なため、エージェントからは実行できません。ユーザーに別のターミナルで実行してもらってください", what)
	}
	return nil
}

func (a *App) requireBare(ctx context.Context, repo string) (string, error) {
	if err := layout.ValidateRepoName(repo); err != nil {
		return "", err
	}
	bare := a.layout.BarePath(repo)
	if !fsx.Exists(bare) {
		return "", fmt.Errorf("bare repo '%s' がありません。別ターミナルで次を実行してください:\n  ws clone <url> %s", repo, repo)
	}
	if err := gitexec.ApplyRepoConfig(ctx, a.git, bare); err != nil {
		return "", err
	}
	return bare, nil
}

// listWorktreeNames returns the names of directories under dir that contain .git. A missing dir yields none.
func listWorktreeNames(dir string) ([]string, error) {
	if !fsx.Exists(dir) {
		return nil, nil
	}
	names, err := fsx.SubdirNames(dir)
	if err != nil {
		return nil, err
	}
	var worktrees []string
	for _, name := range names {
		if fsx.Exists(filepath.Join(dir, name, ".git")) {
			worktrees = append(worktrees, name)
		}
	}
	return worktrees, nil
}

type NewTaskResult struct {
	Dir     string
	Created bool
}

func (a *App) NewTask(task string) (NewTaskResult, error) {
	if err := layout.ValidateTaskName(task); err != nil {
		return NewTaskResult{}, err
	}
	dir := a.layout.TaskDir(task)
	if fsx.Exists(dir) {
		return NewTaskResult{Dir: dir}, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return NewTaskResult{}, err
	}
	return NewTaskResult{Dir: dir, Created: true}, nil
}

func repoNameFromURL(url string) string {
	trimmed := strings.TrimRight(url, "/")
	name := trimmed[strings.LastIndex(trimmed, "/")+1:]
	return strings.TrimSuffix(name, ".git")
}

// Clone prepares repos/<repo>.git. repo is derived from url when empty. It returns the bare repo path.
func (a *App) Clone(ctx context.Context, url, repo string) (path string, err error) {
	if err := a.RefuseInsideAgent("ws clone"); err != nil {
		return "", err
	}
	if repo == "" {
		repo = repoNameFromURL(url)
	}
	if repo == "" {
		return "", errors.New("URL から repo 名を導出できません")
	}
	if err := layout.ValidateRepoName(repo); err != nil {
		return "", err
	}
	bare := a.layout.BarePath(repo)
	if fsx.Exists(bare) {
		return "", fmt.Errorf("%s は既に存在します", bare)
	}
	if err := os.MkdirAll(a.layout.ReposRoot(), 0o755); err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(bare)
		}
	}()
	// git clone --bare はリモートの全ブランチをローカルブランチ化し、add の「ローカルブランチ存在判定」を狂わせる
	if _, err := gitexec.Output(ctx, a.git, "", "init", "--quiet", "--bare", bare); err != nil {
		return "", err
	}
	if err := gitexec.ApplyRepoConfig(ctx, a.git, bare); err != nil {
		return "", err
	}
	for _, args := range [][]string{
		{"remote", "add", "origin", url},
		{"config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"},
		{"fetch", "--quiet", "origin"},
	} {
		if _, err := gitexec.Output(ctx, a.git, bare, args...); err != nil {
			return "", err
		}
	}
	return bare, nil
}

type AddOptions struct {
	Task     string
	Repo     string
	Branch   string
	Base     string
	Create   bool
	ReadOnly bool
}

func errMissingBranch(branch string) error {
	return fmt.Errorf("ローカルに '%s' も 'origin/%s' もありません。"+
		"リモートにあるはずなら、別ターミナルで ws worktree pull --task <task> か git fetch が必要です。"+
		"新規ブランチなら --branch <branch> --create --base <start-point> を使ってください", branch, branch)
}

// Add creates a worktree and returns its path. opts.Task must already be resolved.
func (a *App) Add(ctx context.Context, opts AddOptions) (string, error) {
	bare, err := a.requireBare(ctx, opts.Repo)
	if err != nil {
		return "", err
	}
	slug, err := layout.SlugOf(opts.Branch)
	if err != nil {
		return "", err
	}
	dir := a.layout.TaskDir(opts.Task)
	if opts.ReadOnly {
		dir = a.layout.ReadonlyDir(opts.Task)
	}
	worktree := layout.WorktreePath(dir, opts.Repo, slug)
	add := func(args ...string) error {
		_, err := gitexec.Output(ctx, a.git, bare, append([]string{"worktree", "add"}, args...)...)
		return err
	}
	remoteExists, err := gitexec.RefExists(ctx, a.git, bare, "refs/remotes/origin/"+opts.Branch)
	if err != nil {
		return "", err
	}
	localExists, err := gitexec.RefExists(ctx, a.git, bare, "refs/heads/"+opts.Branch)
	if err != nil {
		return "", err
	}
	switch {
	case opts.ReadOnly:
		if !remoteExists {
			return "", errMissingBranch(opts.Branch)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		err = add("--detach", worktree, "origin/"+opts.Branch)
	case opts.Create:
		if localExists {
			return "", fmt.Errorf("ローカルブランチ '%s' は既に存在します", opts.Branch)
		}
		// 解決できない名前を渡すと git worktree add は -b を無視し、origin の同名ブランチをローカルに作ってしまう
		resolvable, err := gitexec.Succeeds(ctx, a.git, bare, "rev-parse", "--verify", "--quiet", opts.Base+"^{commit}")
		if err != nil {
			return "", err
		}
		if !resolvable {
			return "", fmt.Errorf("--base '%s' を解決できません(origin/main のように明示してください)", opts.Base)
		}
		err = add("--no-track", "-b", opts.Branch, worktree, opts.Base)
	case localExists:
		err = add(worktree, opts.Branch)
	case remoteExists:
		err = add("--track", "-b", opts.Branch, worktree, "origin/"+opts.Branch)
	default:
		return "", errMissingBranch(opts.Branch)
	}
	if err != nil {
		return "", err
	}
	return worktree, nil
}

type RenameOptions struct {
	Task   string
	Repo   string
	Branch string
	To     string
}

type RenameResult struct {
	Old         string
	New         string
	NewPath     string
	Moved       bool
	HadUpstream bool
	// UnsetUpstreamErr is set when the rename succeeded but the stale upstream setting could not be removed.
	UnsetUpstreamErr error
}

// ValidateBranchName accepts only names that git check-ref-format --branch returns unchanged.
func (a *App) ValidateBranchName(ctx context.Context, name string) error {
	// --branch は @{-1} などの略記を展開するので、入力と一致しない名前は受け付けない
	result, err := a.git.Run(ctx, "", "check-ref-format", "--branch", name)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 || result.Stdout != name {
		return fmt.Errorf("ブランチ名 '%s' は git で使えません", name)
	}
	return nil
}

func (a *App) cwdInside(directory string) (bool, error) {
	cwd, err := a.getwd()
	if err != nil {
		return false, err
	}
	cwd, err = fsx.RealPath(cwd)
	if err != nil {
		return false, err
	}
	root, err := fsx.RealPath(directory)
	if err != nil {
		return false, err
	}
	return cwd == root || strings.HasPrefix(cwd, root+"/"), nil
}

// Rename renames the branch and moves the worktree. opts.Task must already be resolved.
// A failed rollback or a failed upstream cleanup after a successful rename is reported in the result or error.
func (a *App) Rename(ctx context.Context, opts RenameOptions) (RenameResult, error) {
	bare, err := a.requireBare(ctx, opts.Repo)
	if err != nil {
		return RenameResult{}, err
	}
	oldSlug, err := layout.SlugOf(opts.Branch)
	if err != nil {
		return RenameResult{}, err
	}
	newSlug, err := layout.SlugOf(opts.To)
	if err != nil {
		return RenameResult{}, err
	}
	taskDir := a.layout.TaskDir(opts.Task)
	oldWorktree := layout.WorktreePath(taskDir, opts.Repo, oldSlug)
	newWorktree := layout.WorktreePath(taskDir, opts.Repo, newSlug)
	current := ""
	if fsx.Exists(oldWorktree + "/.git") {
		if current, err = gitexec.CurrentBranch(ctx, a.git, oldWorktree); err != nil {
			return RenameResult{}, err
		}
	}
	if current != opts.Branch {
		return RenameResult{}, fmt.Errorf("作業用 worktree %s が無いか、ブランチ '%s' を checkout していません", oldWorktree, opts.Branch)
	}
	if err := a.ValidateBranchName(ctx, opts.To); err != nil {
		return RenameResult{}, err
	}
	exists, err := gitexec.RefExists(ctx, a.git, bare, "refs/heads/"+opts.To)
	if err != nil {
		return RenameResult{}, err
	}
	if exists {
		return RenameResult{}, fmt.Errorf("ローカルブランチ '%s' は既に存在します", opts.To)
	}
	moved := oldSlug != newSlug
	if moved {
		if fsx.Exists(newWorktree) {
			return RenameResult{}, fmt.Errorf("移動先 %s は既に存在します", newWorktree)
		}
		inside, err := a.cwdInside(oldWorktree)
		if err != nil {
			return RenameResult{}, err
		}
		if inside {
			return RenameResult{}, fmt.Errorf("カレントディレクトリが %s 配下のため、worktree を移動できません。別のディレクトリで実行してください", oldWorktree)
		}
	}
	// origin 側が prune 済みだと @{upstream} は解決できないが設定は残るので、設定そのものを見る
	hadUpstream, err := gitexec.Succeeds(ctx, a.git, oldWorktree, "config", "--get", "branch."+opts.Branch+".merge")
	if err != nil {
		return RenameResult{}, err
	}
	finalPath := oldWorktree
	if moved {
		finalPath = newWorktree
	}
	// branch -m が成功してから worktree move が失敗した場合に戻せるよう、中断させず最後まで実行する
	ctx = gitexec.Isolated(context.WithoutCancel(ctx))
	unsetErr, err := a.renameBranchAndMove(ctx, bare, oldWorktree, finalPath, opts.Branch, opts.To, hadUpstream)
	if err != nil {
		return RenameResult{}, err
	}
	return RenameResult{Old: opts.Branch, New: opts.To, NewPath: finalPath, Moved: moved, HadUpstream: hadUpstream, UnsetUpstreamErr: unsetErr}, nil
}

func (a *App) renameBranchAndMove(ctx context.Context, bare, oldWorktree, newWorktree, old, new string, hadUpstream bool) (unsetUpstreamErr, err error) {
	if _, err := gitexec.Output(ctx, a.git, oldWorktree, "branch", "--quiet", "-m", old, new); err != nil {
		return nil, err
	}
	if oldWorktree != newWorktree {
		if _, err := gitexec.Output(ctx, a.git, bare, "worktree", "move", oldWorktree, newWorktree); err != nil {
			if _, rollbackErr := gitexec.Output(ctx, a.git, oldWorktree, "branch", "--quiet", "-m", new, old); rollbackErr != nil {
				manual := fmt.Errorf("ブランチ名を元に戻せませんでした。手動で戻してください: git -C %s branch -m %s %s", oldWorktree, new, old)
				return nil, errors.Join(err, manual, rollbackErr)
			}
			return nil, err
		}
	}
	if hadUpstream {
		_, unsetUpstreamErr = gitexec.Output(ctx, a.git, newWorktree, "branch", "--quiet", "--unset-upstream", new)
	}
	return unsetUpstreamErr, nil
}

type WorktreeEntry struct {
	Kind  string
	Name  string
	Head  string
	Dirty bool
}

const (
	KindWork     = "work"
	KindReadonly = "ro"
)

func entriesIn(dir, kind string) ([]WorktreeEntry, error) {
	names, err := listWorktreeNames(dir)
	if err != nil {
		return nil, err
	}
	entries := make([]WorktreeEntry, 0, len(names))
	for _, name := range names {
		entries = append(entries, WorktreeEntry{Kind: kind, Name: name})
	}
	return entries, nil
}

// worktreeEntries lists the work worktrees followed by the readonly ones, without querying git.
func (a *App) worktreeEntries(task string) ([]WorktreeEntry, error) {
	work, err := entriesIn(a.layout.TaskDir(task), KindWork)
	if err != nil {
		return nil, err
	}
	readonly, err := entriesIn(a.layout.ReadonlyDir(task), KindReadonly)
	if err != nil {
		return nil, err
	}
	return append(work, readonly...), nil
}

func (e WorktreeEntry) path(l layout.Layout, task string) string {
	if e.Kind == KindReadonly {
		return l.ReadonlyDir(task) + "/" + e.Name
	}
	return l.TaskDir(task) + "/" + e.Name
}

// ListWorktrees returns the work worktrees followed by the readonly ones.
func (a *App) ListWorktrees(ctx context.Context, task string) ([]WorktreeEntry, error) {
	entries, err := a.worktreeEntries(task)
	if err != nil {
		return nil, err
	}
	for i, entry := range entries {
		path := entry.path(a.layout, task)
		branch, err := gitexec.CurrentBranch(ctx, a.git, path)
		if err != nil {
			return nil, err
		}
		if branch == "" {
			short, err := gitexec.ShortHead(ctx, a.git, path)
			if err != nil {
				return nil, err
			}
			branch = "detached@" + short
		}
		entries[i].Head = branch
		if entries[i].Dirty, err = gitexec.IsDirty(ctx, a.git, path); err != nil {
			return nil, err
		}
	}
	return entries, nil
}

type TaskSummary struct {
	Name     string
	Work     int
	Readonly int
	Dirty    int
}

func (a *App) ListTasks(ctx context.Context) ([]TaskSummary, error) {
	if !fsx.Exists(a.layout.TasksRoot()) {
		return nil, nil
	}
	names, err := fsx.SubdirNames(a.layout.TasksRoot())
	if err != nil {
		return nil, err
	}
	summaries := make([]TaskSummary, 0, len(names))
	for _, name := range names {
		entries, err := a.worktreeEntries(name)
		if err != nil {
			return nil, err
		}
		summary := TaskSummary{Name: name}
		for _, entry := range entries {
			if entry.Kind == KindReadonly {
				summary.Readonly++
			} else {
				summary.Work++
			}
			dirty, err := gitexec.IsDirty(ctx, a.git, entry.path(a.layout, name))
			if err != nil {
				return nil, err
			}
			if dirty {
				summary.Dirty++
			}
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}
