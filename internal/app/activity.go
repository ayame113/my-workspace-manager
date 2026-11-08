package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// worktreeActivity estimates when a worktree was last worked on from the files git rewrites on
// add, commit, checkout and status, so that it costs a few stats instead of walking node_modules.
// It returns the zero time when none of them can be read.
func worktreeActivity(worktree string) time.Time {
	gitdir := filepath.Join(worktree, ".git")
	if data, err := os.ReadFile(gitdir); err == nil {
		target, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir: ")
		if !ok {
			return time.Time{}
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(worktree, target)
		}
		gitdir = target
	}
	var latest time.Time
	for _, name := range []string{"index", "logs/HEAD", "HEAD"} {
		if info, err := os.Stat(filepath.Join(gitdir, name)); err == nil && info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	return latest
}

func modTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// newestFirst sorts by updated descending, keeping the existing (name) order for ties.
func newestFirst[T any](items []T, updated func(T) time.Time) {
	slices.SortStableFunc(items, func(a, b T) int {
		return updated(b).Compare(updated(a))
	})
}

// taskActivity is the newest activity among the task's worktrees, or the task directory's mtime when it has none.
func (a *App) taskActivity(task string, entries []WorktreeEntry) time.Time {
	if len(entries) == 0 {
		return modTime(a.layout.TaskDir(task))
	}
	var latest time.Time
	for _, entry := range entries {
		if entry.Updated.After(latest) {
			latest = entry.Updated
		}
	}
	return latest
}
