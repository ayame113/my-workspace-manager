package layout

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

type Layout struct {
	Root string
}

func FromEnv(getenv func(string) string) Layout {
	if root := getenv("WS_ROOT"); root != "" {
		return Layout{Root: root}
	}
	return Layout{Root: getenv("HOME") + "/ws"}
}

func (l Layout) TasksRoot() string { return l.Root + "/tasks" }

func (l Layout) ReposRoot() string { return l.Root + "/repos" }

func (l Layout) TaskDir(task string) string { return l.TasksRoot() + "/" + task }

func (l Layout) ReadonlyDir(task string) string { return l.TaskDir(task) + "/_readonly" }

func (l Layout) BarePath(repo string) string { return l.ReposRoot() + "/" + repo + ".git" }

func (l Layout) CompactIndexPath() string { return l.Root + "/.compact-index" }

func isSlugChar(c rune) bool {
	return ('a' <= c && c <= 'z') || ('0' <= c && c <= '9')
}

// IsValidTaskName reports whether name matches `^[a-z0-9]+(-[a-z0-9]+)*$`. The same rule applies to branch slugs.
func IsValidTaskName(name string) bool {
	return name != "" &&
		!strings.HasPrefix(name, "-") &&
		!strings.HasSuffix(name, "-") &&
		!strings.Contains(name, "--") &&
		!strings.ContainsFunc(name, func(c rune) bool { return !isSlugChar(c) && c != '-' })
}

// IsRepoNameShape reports whether name matches `^[A-Za-z0-9][A-Za-z0-9._-]*$`.
func IsRepoNameShape(name string) bool {
	alphanumeric := func(c rune) bool {
		return isSlugChar(c) || ('A' <= c && c <= 'Z')
	}
	if name == "" || !alphanumeric(rune(name[0])) {
		return false
	}
	return !strings.ContainsFunc(name, func(c rune) bool {
		return !alphanumeric(c) && c != '.' && c != '_' && c != '-'
	})
}

// BranchSlug lowercases branch, collapses every run of characters outside [a-z0-9] into a single "-", and trims leading and trailing "-".
func BranchSlug(branch string) string {
	replaced := strings.Map(func(c rune) rune {
		if isSlugChar(c) {
			return c
		}
		return '-'
	}, strings.ToLower(branch))
	return strings.Join(strings.FieldsFunc(replaced, func(c rune) bool { return c == '-' }), "-")
}

func SlugOf(branch string) (string, error) {
	slug := BranchSlug(branch)
	if slug == "" {
		return "", fmt.Errorf("ブランチ名 '%s' から slug を作れません", branch)
	}
	return slug, nil
}

func ValidateTaskName(task string) error {
	if !IsValidTaskName(task) {
		return fmt.Errorf("task 名は [a-z0-9-] のみ(先頭末尾・連続ハイフン不可): '%s'", task)
	}
	return nil
}

func ValidateRepoName(repo string) error {
	if !IsRepoNameShape(repo) || strings.Contains(repo, "--") {
		return fmt.Errorf("repo 名が不正です('--' を含まず英数字 . _ - のみ): '%s'", repo)
	}
	return nil
}

type WorktreeName struct {
	Repo string
	Slug string
}

// SplitWorktreeName splits `<repo>--<slug>` at the first "--". Slug is empty when there is none.
func SplitWorktreeName(name string) WorktreeName {
	repo, slug, _ := strings.Cut(name, "--")
	return WorktreeName{Repo: repo, Slug: slug}
}

// ParseWorktreeName splits name only when both parts follow the naming rules.
func ParseWorktreeName(name string) (WorktreeName, bool) {
	parsed := SplitWorktreeName(name)
	if !IsRepoNameShape(parsed.Repo) || !IsValidTaskName(parsed.Slug) {
		return WorktreeName{}, false
	}
	return parsed, true
}

// TaskUnder returns the first path component of path below tasksRoot.
func TaskUnder(tasksRoot, path string) (string, bool) {
	relative, ok := strings.CutPrefix(path, tasksRoot+"/")
	if !ok {
		return "", false
	}
	task, _, _ := strings.Cut(relative, "/")
	return task, task != ""
}

func WorktreePath(dir, repo, slug string) string {
	return filepath.Join(dir, repo+"--"+slug)
}

// RepoTaskPrefix joins the slugs of repos into a task-name prefix, writing their common leading words once
// (app-web, app-api → app-web-api). It returns "" when no repo has a usable slug.
func RepoTaskPrefix(repos []string) string {
	var words [][]string
	for _, repo := range repos {
		if slug := BranchSlug(repo); slug != "" {
			words = append(words, strings.Split(slug, "-"))
		}
	}
	if len(words) == 0 {
		return ""
	}
	common := words[0]
	for _, w := range words[1:] {
		n := 0
		for n < len(common) && n < len(w) && common[n] == w[n] {
			n++
		}
		common = common[:n]
	}
	parts := slices.Clone(common)
	for _, w := range words {
		rest := strings.Join(w[len(common):], "-")
		if rest != "" && !slices.Contains(parts, rest) {
			parts = append(parts, rest)
		}
	}
	return strings.Join(parts, "-")
}
