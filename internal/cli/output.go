package cli

import (
	"fmt"
	"unicode/utf8"

	"github.com/ayame113/my-workspace-manager/internal/app"
)

func (r *runner) printTasks(tasks []app.TaskSummary) {
	type row struct{ name, count, state string }
	rows := make([]row, 0, len(tasks))
	nameWidth, countWidth := 0, 0
	for _, task := range tasks {
		count := fmt.Sprintf("%d worktrees", task.Work+task.Readonly)
		if task.Readonly > 0 {
			count += fmt.Sprintf(" (%d readonly)", task.Readonly)
		}
		state := "clean"
		if task.Dirty > 0 {
			state = fmt.Sprintf("%d dirty", task.Dirty)
		}
		rows = append(rows, row{task.Name, count, state})
		nameWidth = max(nameWidth, utf8.RuneCountInString(task.Name))
		countWidth = max(countWidth, utf8.RuneCountInString(count))
	}
	for _, row := range rows {
		r.printf("%-*s%-*s%s\n", nameWidth+3, row.name, countWidth+3, row.count, row.state)
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

func (r *runner) printIssue(fix bool) func(app.IssueResult) {
	return func(result app.IssueResult) {
		issue := result.Issue
		r.printf("[%s] %s\n  種類: %s\n  内容: %s\n  fix: %s\n", issue.Task, issue.Worktree, issue.Kind, issue.Message, issue.Fix)
		if !fix {
			return
		}
		switch result.Outcome {
		case app.Fixed:
			r.printf("  結果: 修正しました\n")
		case app.NeedsManual:
			r.printf("  結果: 手動対応が必要です\n")
		case app.FixFailed:
			r.printf("  結果: 修正に失敗しました: %s\n", result.Failure)
		}
	}
}

func (r *runner) finishLint(summary app.LintSummary, fix bool, err error) error {
	if err != nil {
		return err
	}
	if summary.Total == 0 {
		r.printf("問題はありません\n")
		return nil
	}
	suffix := ""
	if fix {
		suffix = fmt.Sprintf("(未解決 %d 件)", summary.Unresolved)
	}
	r.printf("\n%d 件の問題%s\n", summary.Total, suffix)
	if summary.Unresolved > 0 {
		return exitError{ExitFailure}
	}
	return nil
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
	if result.AvailableFirst != nil && result.AvailableLast != nil {
		r.printf("空き容量: %s → %s\n", formatMiB(int64(*result.AvailableFirst)), formatMiB(int64(*result.AvailableLast)))
	}
}
