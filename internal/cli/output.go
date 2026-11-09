// このファイルは、コマンドの実行結果を人間が読める文字列にして出力する関数を集めている。
// task 一覧、worktree 一覧、rename / pull / lint / compact の結果表示がある。
// ロジック(何を調べるか)は internal/app が担当し、ここは受け取った結果を整形して表示するだけにしている。
// 列をそろえる表示には fmt の書式指定子(%-6s など)を使っている。
package cli

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ayame113/my-workspace-manager/internal/app"
)

// printNewTask prints the created task directory and each worktree created in it.
//
// printNewTask は、ws new(対話)で作った task のディレクトリと、その中に作った worktree を 1 行ずつ表示する。
// (r *runner) はレシーバで、このメソッドが runner 型に属することを表す。
// ポインタレシーバ(*runner)なので、r は runner の実体への参照で、r.env などをコピーせずに使える。
// TypeScript のクラスのメソッドで this を使うのに近い。
func (r *runner) printNewTask(result app.NewTaskWithWorktreesResult) {
	r.printf("作成しました: %s\n", result.Dir)
	// for ... range はスライスの繰り返しで、`for (const [i, v] of arr.entries())` に近い。
	// 1 つ目の値はインデックス、2 つ目は要素。インデックスを使わないときは、未使用の変数を禁止する Go の規則のため、_ で捨てる。
	for _, worktree := range result.Worktrees {
		r.printf("作成しました: %s (ブランチ: %s)\n", worktree.Path, result.Branch)
	}
}

// printTasks prints one line per task.
//
// printTasks は task の一覧を 1 task 1 行で表示する。ws ls が端末でないとき(パイプやエージェントからの実行)に使う。
// 整形は taskRows に任せ、ここでは 1 行ずつ出すだけ。
func (r *runner) printTasks(tasks []app.TaskSummary) {
	for _, row := range taskRows(tasks) {
		r.printf("%s\n", row)
	}
}

// taskRows formats one aligned line per task, in the order given.
//
// taskRows は task の一覧を、列をそろえた文字列のスライスにして返す。レシーバを持たない普通の関数。
// 端末でないときの表示(printTasks)と、対話の選択画面(pickTask)の両方から使われる。
// 1 行は「task 名  N worktrees (M readonly)  clean / N dirty  description」の形。
// 先に全 task を走査して各列の最大幅を求め、2 回目の走査で幅をそろえて文字列にする。
func taskRows(tasks []app.TaskSummary) []string {
	// 関数の中でだけ使う型を、その場で宣言できる。TypeScript の関数内の type 宣言に近い。
	// `name, count, state, description string` は、同じ型のフィールドをまとめて書く省略記法。
	type row struct{ name, count, state, description string }
	// make([]T, 長さ, 容量) でスライス(可変長配列)を作る。長さ 0・容量 len(tasks) で作るのは、
	// 後で append するときに再確保が起きないようにするため。
	rows := make([]row, 0, len(tasks))
	// 同じ型の変数を並べて初期化できる。3 つとも 0 から始めて、最大値を更新していく。
	nameWidth, countWidth, stateWidth := 0, 0, 0
	for _, task := range tasks {
		// fmt.Sprintf は書式付きで文字列を作って返す(出力はしない)。%d は整数。
		// Work は作業用 worktree の数、Readonly は参照専用(_readonly/ 配下)の worktree の数。
		count := fmt.Sprintf("%d worktrees", task.Work+task.Readonly)
		if task.Readonly > 0 {
			count += fmt.Sprintf(" (%d readonly)", task.Readonly)
		}
		// 未コミットの変更がある worktree の数。Dirty は変更が残っている worktree の数。
		state := "clean"
		if task.Dirty > 0 {
			state = fmt.Sprintf("%d dirty", task.Dirty)
		}
		// append は末尾に要素を足した新しいスライスを返すので、受け取り直す(rows = append(rows, ...))。
		// row{...} の中のフィールド名を省略した書き方は、宣言順に値を並べる。
		rows = append(rows, row{task.Name, count, state, task.Description})
		// max は組み込み関数(Go 1.21 以降)。utf8.RuneCountInString は文字数を数える。
		// len(文字列) はバイト数を返し、日本語は 1 文字 3 バイトになるため、文字数が欲しいときはこちらを使う。
		// (ただし全角文字は表示上 2 桁分の幅なので、日本語が混じるとずれる可能性はある。)
		nameWidth = max(nameWidth, utf8.RuneCountInString(task.Name))
		countWidth = max(countWidth, utf8.RuneCountInString(count))
		stateWidth = max(stateWidth, utf8.RuneCountInString(state))
	}
	lines := make([]string, len(rows))
	for i, row := range rows {
		// 書式指定子 %-*s は「左寄せ(-)で、幅を引数から取る(*)文字列(s)」。
		// 幅の引数 → 文字列の引数の順に 2 つ 1 組で渡す(nameWidth+3, row.name など)。
		// 各列の最大幅に 3 を足して、列の間に 3 文字ぶん空ける。最後の %s(description)は幅指定なし。
		// ここの row は上で宣言した型 row と同じ名前だが、ループ変数が優先される(シャドーイング)。
		line := fmt.Sprintf("%-*s%-*s%-*s%s", nameWidth+3, row.name, countWidth+3, row.count, stateWidth+3, row.state, row.description)
		// description が空のとき、行末に余った空白が残るので取り除く。
		lines[i] = strings.TrimRight(line, " ")
	}
	return lines
}

// printWorktrees prints one line per worktree: kind, name, HEAD, and whether it has uncommitted changes.
//
// printWorktrees は ws worktree ls の出力。1 worktree 1 行で、種類(work / readonly)、名前、HEAD、clean / dirty を表示する。
// %-6s のように幅を数字で直接指定すると、左寄せで指定文字数になるまで空白で埋める。
func (r *runner) printWorktrees(entries []app.WorktreeEntry) {
	for _, entry := range entries {
		state := "clean"
		if entry.Dirty {
			state = "dirty"
		}
		r.printf("%-6s %-55s %-45s %s\n", entry.Kind, entry.Name, entry.Head, state)
	}
}

// printRename prints the outcome of ws worktree rename, including how to reflect the new name on the remote.
//
// printRename は ws worktree rename の結果を表示する。
// rename はローカルのブランチ名とディレクトリ名だけを変え、リモート(origin)は変更しない。
// そのため、元のブランチにリモート追跡(upstream)があった場合は、リモートに反映する手順を案内する。
func (r *runner) printRename(result app.RenameResult) {
	r.printf("リネームしました: %s → %s\n", result.Old, result.New)
	// ブランチ名の slug が変わったときだけ、worktree のディレクトリも移動している。
	if result.Moved {
		r.printf("新しいパス: %s\n", result.NewPath)
	}
	// error 型のフィールドが nil でなければ、upstream の設定を外す処理が失敗している。
	// rename 自体は成功しているので、エラーにはせず警告として stderr に出す(終了コードは 0 のまま)。
	if result.UnsetUpstreamErr != nil {
		fmt.Fprintf(r.env.Stderr, "警告: %s の upstream 設定を外せませんでした: %s\n手動で外すには: git -C %s branch --unset-upstream %s\n",
			result.New, strings.TrimSpace(result.UnsetUpstreamErr.Error()), result.NewPath, result.New)
	}
	// %[1]s、%[2]s は「N 番目の引数を使う」という書式指定子。同じ値を何度も書式に埋め込むときに、
	// 引数を繰り返し渡さずに済む。ここでは 1 番目が旧ブランチ名(result.Old)、2 番目が新ブランチ名(result.New)。
	// 文字列は "..." + "..." で連結して、長い文章を複数行に分けて書いている。
	if result.HadUpstream {
		r.printf("origin/%[1]s は残っています。リモートに反映するには(別ターミナルで):\n"+
			"  git push -u origin %[2]s\n"+
			"  git push origin --delete %[1]s\n"+
			"※ %[1]s の PR がある場合は、代わりに GitHub の Branches 画面でリネームし、\n"+
			"  ws worktree pull の後に git branch -u origin/%[2]s を実行してください\n", result.Old, result.New)
	}
}

// printPullEntry prints the pull result of one worktree.
//
// printPullEntry は ws worktree pull で、worktree 1 つぶんの結果(種類、名前、状態、詳細)を 1 行表示する。
// app.Pull に「1 件終わるたびに呼ぶコールバック」として渡される(commands.go の worktreePullCommand 参照)。
// メソッドを値として渡す書き方(r.printPullEntry)は、TypeScript で this を束縛した関数を渡すのに近い。
func (r *runner) printPullEntry(entry app.PullEntry) {
	r.printf("%-5s %-55s %-10s %s\n", entry.Kind, entry.Name, entry.Status, entry.Detail)
}

// reportIssues prints the lint result and returns the error that decides the exit code.
//
// reportIssues は lint の結果を表示する。引数が (issues, err) の 2 つなのは、
// `r.reportIssues(r.app.LintWorkspace(ctx))` のように、複数の戻り値を返す関数の結果をそのまま渡せるため
// (Go では、f(g()) の g が返す値が f の引数とちょうど合えば、まとめて渡せる)。
// 問題が 1 件でもあれば exitError{ExitFailure} を返し、終了コード 1 にする。
// 表示はすでに済ませているので、exitCode は追加のメッセージを出さない。
func (r *runner) reportIssues(issues []app.Issue, err error) error {
	// これが Go の定番のエラー処理。失敗したら、その場でエラーを呼び出し元に返す(TypeScript の throw に近いが、戻り値で返す)。
	if err != nil {
		return err
	}
	// len(スライス) は要素数。0 件なら問題なしとして正常終了する(nil を返すと成功)。
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

// printCompactEvent prints the progress of ws compact.
//
// printCompactEvent は ws compact の途中経過(走査中、ハッシュ計算中など)を表示する。
// app.Compact が処理の節目ごとに呼ぶコールバックとして渡される。
func (r *runner) printCompactEvent(event app.CompactEvent) {
	// switch は、値(ここでは event.Kind)に応じて分岐する。Go の case は自動で break されるので、
	// 次の case に落ちない(JavaScript と違い、break を書く必要が無い)。
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

// formatMiB formats a byte count as MiB with one decimal place, e.g. "12.3 MiB".
//
// formatMiB はバイト数を「12.3 MiB」の形に整形する。1 MiB = 1048576 バイト。
// 整数どうしの割り算は小数を切り捨てるので、先に 10 倍してから割り、小数第 1 位ぶんを整数のまま取り出している。
// (浮動小数点を使わず、/ で十の位以上、% で小数第 1 位を取り出す。)
// int64 は 64 ビット整数型。ファイルサイズの合計は 32 ビット整数に収まらないことがあるため。
func formatMiB(bytes int64) string {
	tenths := bytes * 10 / 1048576
	return fmt.Sprintf("%d.%d MiB", tenths/10, tenths%10)
}

// printCompactResult prints the final summary of ws compact.
//
// printCompactResult は ws compact の最終結果を表示する。
// --dry-run のときは何も置き換えていないので、対象件数と削減見込みだけを出して早期 return する。
// 削減見込みは、置き換えたファイルのサイズの合計。実際のディスク使用量は du では分からない(README 参照)。
func (r *runner) printCompactResult(result app.CompactResult) {
	if result.DryRun {
		r.printf("置き換え対象: %d ファイル(削減見込み %s)。--dry-run のため変更していません\n", result.Targets, formatMiB(result.SavingBytes))
		return
	}
	r.printf("置き換え: %d ファイル(削減見込み %s)\n", result.Replaced, formatMiB(result.ReplacedBytes))
	// 走査した後でファイルが変更された、または clonefile で複製できなかったファイルがあれば、スキップした旨を知らせる。
	if result.Changed > 0 || result.NotCloned > 0 {
		r.printf("スキップ: 走査後に変更されていた %d ファイル、複製できなかった %d ファイル\n", result.Changed, result.NotCloned)
	}
}
