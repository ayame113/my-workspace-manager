package cli

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ayame113/my-workspace-manager/internal/app"
)

func (r *runner) printNewTask(result app.NewTaskWithWorktreesResult) {
	r.printf("作成しました: %s\n", result.Dir)
	for _, worktree := range result.Worktrees {
		r.printf("作成しました: %s (ブランチ: %s)\n", worktree.Path, result.Branch)
	}
}

func (r *runner) printTasks(tasks []app.TaskSummary) {
	type row struct{ name, count, state, description string }
	rows := make([]row, 0, len(tasks))
	nameWidth, countWidth, stateWidth := 0, 0, 0
	for _, task := range tasks {
		count := fmt.Sprintf("%d worktrees", task.Work+task.Readonly)
		if task.Readonly > 0 {
			count += fmt.Sprintf(" (%d readonly)", task.Readonly)
		}
		state := "clean"
		if task.Dirty > 0 {
			state = fmt.Sprintf("%d dirty", task.Dirty)
		}
		rows = append(rows, row{task.Name, count, state, task.Description})
		nameWidth = max(nameWidth, utf8.RuneCountInString(task.Name))
		countWidth = max(countWidth, utf8.RuneCountInString(count))
		stateWidth = max(stateWidth, utf8.RuneCountInString(state))
	}
	for _, row := range rows {
		line := fmt.Sprintf("%-*s%-*s%-*s%s", nameWidth+3, row.name, countWidth+3, row.count, stateWidth+3, row.state, row.description)
		r.printf("%s\n", strings.TrimRight(line, " "))
	}
}

func (r *runner) printWorktrees(entries []app.WorktreeEntry) {
	for _, entry := range entries {
		state := "clean"
		if entry.Dirty {
			state = "dirty"
		}
		r.printf("%-6s %-55s %-45s %s\n", entry.Kind, entry.Name, entry.Head, state)
	}
}

func (r *runner) printRename(result app.RenameResult) {
	r.printf("リネームしました: %s → %s\n", result.Old, result.New)
	if result.Moved {
		r.printf("新しいパス: %s\n", result.NewPath)
	}
	if result.UnsetUpstreamErr != nil {
		fmt.Fprintf(r.env.Stderr, "警告: %s の upstream 設定を外せませんでした: %s\n手動で外すには: git -C %s branch --unset-upstream %s\n",
			result.New, strings.TrimSpace(result.UnsetUpstreamErr.Error()), result.NewPath, result.New)
	}
	if result.HadUpstream {
		r.printf("origin/%[1]s は残っています。リモートに反映するには(別ターミナルで):\n"+
			"  git push -u origin %[2]s\n"+
			"  git push origin --delete %[1]s\n"+
			"※ %[1]s の PR がある場合は、代わりに GitHub の Branches 画面でリネームし、\n"+
			"  ws worktree pull の後に git branch -u origin/%[2]s を実行してください\n", result.Old, result.New)
	}
}

func (r *runner) printPullEntry(entry app.PullEntry) {
	r.printf("%-5s %-55s %-10s %s\n", entry.Kind, entry.Name, entry.Status, entry.Detail)
}

func (r *runner) reportIssues(issues []app.Issue, err error) error {
	if err != nil {
		return err
	}
	if len(issues) == 0 {
		r.printf("問題はありません\n")
		return nil
	}
	for _, issue := range issues {
		r.printf("[%s] %s\n  種類: %s\n  内容: %s\n  fix: %s\n", issue.Task, issue.Worktree, issue.Kind, issue.Message, issue.Fix)
	}
	r.printf("\n%d 件の問題\n", len(issues))
	return exitError{ExitFailure}
}

func (r *runner) printCompactEvent(event app.CompactEvent) {
	switch event.Kind {
	case app.CompactScanProgress:
		r.printf("走査中: %d ファイル\n", event.Count)
	case app.CompactScanned:
		r.printf("走査しました: %d ファイル\n", event.Count)
	case app.CompactHashProgress:
		r.printf("ハッシュ計算中: %d / %d\n", event.Count, event.Total)
	case app.CompactHashed:
		r.printf("ハッシュ計算: %d ファイル\n", event.Count)
	}
}

func formatMiB(bytes int64) string {
	tenths := bytes * 10 / 1048576
	return fmt.Sprintf("%d.%d MiB", tenths/10, tenths%10)
}

func (r *runner) printCompactResult(result app.CompactResult) {
	if result.DryRun {
		r.printf("置き換え対象: %d ファイル(削減見込み %s)。--dry-run のため変更していません\n", result.Targets, formatMiB(result.SavingBytes))
		return
	}
	r.printf("置き換え: %d ファイル(削減見込み %s)\n", result.Replaced, formatMiB(result.ReplacedBytes))
	if result.Changed > 0 || result.NotCloned > 0 {
		r.printf("スキップ: 走査後に変更されていた %d ファイル、複製できなかった %d ファイル\n", result.Changed, result.NotCloned)
	}
}
