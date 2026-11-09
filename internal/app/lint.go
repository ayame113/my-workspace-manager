// lint.go は `ws lint`(workspace 全体)と `ws worktree lint`(1 つの task)の検査ロジック。
// ディレクトリ構成や git の状態が ws の規約から外れていないかを調べ、問題の一覧(Issue)を返す。
// 検出するだけで修正はしない。直せるものには、実行すべき git コマンドを Fix に入れて表示させる。
//
// 検出項目(Issue.Kind): naming(命名規則違反)、broken-worktree(git が worktree と認識しない / ディレクトリが無い)、
// dir-branch-mismatch(ディレクトリ名の slug と実際のブランチが違う、detached HEAD)、
// upstream-mismatch(upstream が別名ブランチ)、readonly(参照用 worktree の破綻)。
//
// 背景: ws では、リポジトリの本体を bare repo(作業ディレクトリを持たない git リポジトリ、`~/ws/repos/<repo>.git`)に置き、
// そこから `git worktree` で作った作業ディレクトリを `~/ws/tasks/<task>/<repo>--<branch-slug>` に並べる。
// 参照専用の worktree(readonly)は `_readonly/` の下にあり、`origin/<branch>` を detached HEAD で checkout している。
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

// Issue は lint が見つけた問題 1 件。CLI 側が整形して表示する。
// Task と Worktree は場所(task に属さないものは "-")、Kind は上記の種類、Message は説明、Fix は直し方。
type Issue struct {
	Task     string
	Worktree string
	Kind     string
	Message  string
	Fix      string
}

// issueCollector は検査中に見つかった問題を貯めておく入れ物。
type issueCollector struct {
	issues []Issue
}

// add は問題を 1 件追加する。`task, worktree, kind, message, fix string` は、同じ型の引数をまとめて書く省略形
// (TypeScript の `(task: string, worktree: string, ...)` に相当)。
// ポインタレシーバ(`*issueCollector`)なので、呼び出し元の issues スライスを実際に更新できる。
// 値レシーバだとコピーに追加することになり、呼び出し元には反映されない。
func (c *issueCollector) add(task, worktree, kind, message, fix string) {
	// append は「足した後のスライス」を返すので、自分自身に代入する。
	c.issues = append(c.issues, Issue{Task: task, Worktree: worktree, Kind: kind, Message: message, Fix: fix})
}

// readGitDir は worktree の git ディレクトリの絶対パスを返す。
// 戻り値は 3 つ: パス、「git が worktree として認識したか」、エラー。
// 「認識しなかった」は異常ではなく検査結果なので、エラーにせず false で返す。エラーは git の実行自体に失敗したときだけ。
func (a *App) readGitDir(ctx context.Context, worktree string) (string, bool, error) {
	// .git が無いディレクトリでは、親ディレクトリの repo を拾わないようにする
	// .git ファイルが無いディレクトリで git を実行すると、親ディレクトリのリポジトリが見つかってしまい、誤判定になる。
	// (worktree の .git は、bare repo 側の情報を指すファイル。)
	if !fsx.Exists(worktree + "/.git") {
		return "", false, nil
	}
	// a.git は git コマンドを実行する部品(interface)。Run は指定ディレクトリで git を実行し、
	// 標準出力・終了コードなどを持つ結果を返す。git が 0 以外で終了してもエラーにはならず、ExitCode に入る。
	// interface は「このメソッドを持っていれば何でも渡せる」型で、TypeScript の interface に近い
	// (明示的に implements と書かなくても、メソッドが揃っていれば満たす)。テストで偽物に差し替えられる。
	result, err := a.git.Run(ctx, worktree, "rev-parse", "--absolute-git-dir")
	// エラーを戻り値で受け取り、nil でなければすぐ呼び出し元に返すのが Go のエラー処理の定型。
	// TypeScript なら throw して try/catch に任せるところを、毎回明示的に書く。
	if err != nil {
		return "", false, err
	}
	return result.Stdout, result.ExitCode == 0, nil
}

// lintWorktree は worktree 1 つを検査し、問題を c に追加する。
// 引数: task(所属 task 名)、directory(worktree のパス)、name(ディレクトリ名)、readonly(参照用か)。
// 戻り値の error は git の実行失敗など、検査を続けられない場合だけ。規約違反は error ではなく c に追加する。
//
// 検査の順序: (1) 名前が `<repo>--<slug>` 形式か (2) git が worktree と認識するか
// (3) 名前の repo 部分と実際の bare repo が一致するか (4) ブランチ・upstream・readonly の状態。
// (1) と (2) に失敗したら、以降の検査はできないので、問題を追加して return nil(エラーなしで終了)する。
func (a *App) lintWorktree(ctx context.Context, c *issueCollector, task, directory, name string, readonly bool) error {
	// 表示用の名前。readonly のものは、作業用と区別できるよう `_readonly/` を付ける。
	label := name
	if readonly {
		label = "_readonly/" + name
	}
	// manual は「手動で対応するしかない問題」を追加する関数。関数を変数に入れて使う(クロージャ)。
	// TypeScript の `const manual = (kind, message, fix) => {...}` と同じで、外側の c、task、label を取り込んで使える。
	// lintReadonly にも渡して、同じ形式で問題を追加させる。
	manual := func(kind, message, fix string) {
		c.add(task, label, kind, message, "手動対応: "+fix)
	}
	// `<repo>--<slug>` を repo と slug に分解する。2 つ目の戻り値 ok は、分解できたか(`v, ok :=` の定型)。
	parsed, ok := layout.ParseWorktreeName(name)
	if !ok {
		manual("naming", "`<repo>--<slug>` 形式のディレクトリ名ではありません", "名前を確認し、不要なら削除")
		return nil
	}
	// `:=` の左辺に既存の変数(ok)があっても、新しい変数(gitdir、err)が 1 つでもあれば使える。
	// その場合、既存の変数には代入だけが行われる。
	gitdir, ok, err := a.readGitDir(ctx, directory)
	if err != nil {
		return err
	}
	if !ok {
		manual("broken-worktree", "git が worktree として認識しません", "不要なら削除し、必要なら ws worktree add で作り直す")
		return nil
	}
	// worktree の git ディレクトリは `<bare repo>/worktrees/<名前>` なので、2 回 Dir(親ディレクトリ)を取ると bare repo のパスになる。
	// 実際の bare repo と、ディレクトリ名の repo 部分から期待される bare repo(`repos/<repo>.git`)を、
	// symlink を解決した実パスで比べ、リネームやコピー漏れ(名前と実体の不一致)を検出する。
	actualBare, actualErr := fsx.RealPath(filepath.Dir(filepath.Dir(gitdir)))
	expectedBare, expectedErr := fsx.RealPath(a.layout.BarePath(parsed.Repo))
	if actualErr != nil || expectedErr != nil || actualBare != expectedBare {
		shown := actualBare
		if actualErr != nil {
			shown = "不明"
		}
		manual("naming", fmt.Sprintf("repo 部分 '%s' と実際の bare repo(%s)が一致しません", parsed.Repo, shown), "ディレクトリ名を実際の repo に合わせる")
	}
	// 現在のブランチ名。detached HEAD のときは空文字が返る。
	branch, err := gitexec.CurrentBranch(ctx, a.git, directory)
	if err != nil {
		return err
	}
	// 式なしの switch は、上から順に条件を評価して、最初に真になった case を実行する(if / else if の連鎖と同じ)。
	// Go の switch は break を書かなくても次の case に落ちない。
	switch {
	case readonly:
		return a.lintReadonly(ctx, directory, branch, manual)
	// 作業用 worktree で、ブランチを checkout している(通常の状態)。
	case branch != "":
		// ディレクトリ名の slug と、実際のブランチ名から計算した slug が違う。
		// ws の外で `git switch` や `git branch -m` をしたときに起こる。
		if layout.BranchSlug(branch) != parsed.Slug {
			manual("dir-branch-mismatch", fmt.Sprintf("現在のブランチ '%s' の slug が名前と異なります", branch), "ブランチを戻すか worktree を作り直す")
		}
		// upstream(`git push` / `git pull` の既定の相手)を調べる。設定されていないと git が 0 以外で終了するので、
		// ExitCode が 0 のときだけ upstream があるとみなす。ここでの err は git を起動できなかったときのもの。
		upstream, err := a.git.Run(ctx, directory, "rev-parse", "--abbrev-ref", "@{upstream}")
		if err != nil {
			return err
		}
		// upstream があっても `origin/<同名のブランチ>` でなければ、別名ブランチを指している(例: base の origin/main)。
		// この状態だと push が意図しないブランチへ向かうので報告する。直し方は upstream を外すこと。
		if upstream.ExitCode == 0 && upstream.Stdout != "origin/"+branch {
			c.add(task, label, "upstream-mismatch",
				fmt.Sprintf("upstream が別名ブランチ '%s' を指しています", upstream.Stdout),
				"git -C "+directory+" branch --unset-upstream")
		}
	// branch が空、つまり detached HEAD。作業用 worktree はブランチに固定する規約なので違反になる。
	default:
		manual("dir-branch-mismatch", "detached HEAD です(作業用 worktree はブランチ固定)", "ブランチを checkout する")
	}
	return nil
}

// lintReadonly は参照専用(readonly)worktree の検査。
// readonly は `origin/<branch>` の detached HEAD で、手を加えない前提。次を問題として報告する:
// ブランチを checkout している / origin に無いコミットがある / 未コミット変更がある。
// manual は lintWorktree から渡される、問題を追加する関数(関数を引数として受け取っている)。
func (a *App) lintReadonly(ctx context.Context, directory, branch string, manual func(kind, message, fix string)) error {
	if branch != "" {
		manual("readonly", fmt.Sprintf("ブランチ '%s' を checkout しています", branch), "detached に戻す(git switch --detach)")
	}
	// HEAD から辿れて、origin のどのブランチからも辿れないコミットの数。つまり、origin に無い(ローカルだけの)コミット。
	count, err := gitexec.Output(ctx, a.git, directory, "rev-list", "--count", "HEAD", "--not", "--remotes=origin")
	// Atoi は文字列を整数に変換する。コマンドが失敗した、または数値として読めないときは、何も報告しない。
	if unpushed, parseErr := strconv.Atoi(count); err == nil && parseErr == nil && unpushed > 0 {
		manual("readonly", fmt.Sprintf("origin に無いコミットが %d 件あります", unpushed), "内容を確認する")
	}
	// 未コミットの変更(追跡中ファイルの変更や新規ファイル)があるかどうか。
	dirty, err := gitexec.IsDirty(ctx, a.git, directory)
	if err != nil {
		return err
	}
	if dirty {
		manual("readonly", "未コミット変更があります", "内容を確認する")
	}
	return nil
}

// lintTask は task 1 つを検査する。task 名の規則を確認し、直下の各ディレクトリを worktree として lintWorktree に渡す。
// `_readonly` ディレクトリだけは特別扱いで、その中の各ディレクトリを readonly worktree として検査する。
func (a *App) lintTask(ctx context.Context, c *issueCollector, task string) error {
	// layout は `WS_ROOT` 配下のパスの組み立てと命名規則を持つ部品。
	directory := a.layout.TaskDir(task)
	// task 名の規則は `[a-z0-9-]+`、先頭末尾のハイフンと連続ハイフンは不可。違反は報告するだけで、名前の変更は勧めない
	// (Claude Code の履歴がパス単位で保存されるため、task ディレクトリは移動しない方針)。
	if !layout.IsValidTaskName(task) {
		c.add(task, "-", "naming", "task 名が規則([a-z0-9-]、連続・先頭末尾ハイフン不可)に違反しています",
			"手動対応: task 名の変更はしない方針なので内容を確認する")
	}
	// task ディレクトリ直下のサブディレクトリ名の一覧。
	names, err := fsx.SubdirNames(directory)
	if err != nil {
		return err
	}
	for _, name := range names {
		// `_readonly` 以外は作業用 worktree。検査したら continue で次の名前へ進む。
		if name != "_readonly" {
			if err := a.lintWorktree(ctx, c, task, directory+"/"+name, name, false); err != nil {
				return err
			}
			continue
		}
		// ここに来るのは `_readonly` のときだけ。その下の readonly worktree を 1 つずつ検査する。
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
// git が worktree として登録しているのに、ディレクトリが消えているもの(ぶら下がった登録)を探す。
// ディレクトリを直接 `rm -rf` すると、bare repo 側の登録が残る。fix は `git worktree prune`。
// accepts は「この task の分も報告するか」を決める関数で、LintTask は自分の task だけ、LintWorkspace は全部を通す。
// (関数を引数として受け取る。TypeScript のコールバックに近い。)
func (a *App) lintDanglingRegistrations(ctx context.Context, c *issueCollector, accepts func(task string) bool) error {
	// repos/ がまだ無ければ(clone 前)、調べるものが無い。
	if !fsx.Exists(a.layout.ReposRoot()) {
		return nil
	}
	entries, err := fsx.SubdirNames(a.layout.ReposRoot())
	if err != nil {
		return err
	}
	for _, entry := range entries {
		bare := a.layout.ReposRoot() + "/" + entry
		// bare repo ごとに、登録されている worktree の一覧を機械可読(--porcelain)な形式で取得する。
		// `worktree <パス>` で始まる行が 1 件に相当する。
		listing, err := a.git.Run(ctx, bare, "worktree", "list", "--porcelain")
		if err != nil {
			return err
		}
		// bare repo として読めない(壊れている、リポジトリではない)ものは、ここでは扱わずスキップする。
		if listing.ExitCode != 0 {
			continue
		}
		first := true
		// SplitSeq は分割した結果を、スライスを作らずに 1 つずつ返すイテレータ。for-range でそのまま回せる(Go 1.24 以降)。
		for line := range strings.SplitSeq(listing.Stdout, "\n") {
			// 接頭辞があれば取り除いた残り(= パス)と true を返す。無ければ ok は false。
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
			// このパスが tasks/ 配下ならその task 名を得る。2 つ目の戻り値(tasks/ 配下か)は捨てて、
			// 配下でなければ task は空文字になる。
			task, _ := layout.TaskUnder(a.layout.TasksRoot(), worktree)
			if !accepts(task) {
				continue
			}
			// task に属さないものは、表示上 "-" にする。
			shownTask := task
			if shownTask == "" {
				shownTask = "-"
			}
			c.add(shownTask, filepath.Base(worktree), "broken-worktree",
				"git に登録されていますがディレクトリがありません: "+worktree,
				"git -C "+bare+" worktree prune")
		}
	}
	return nil
}

// LintTask checks one task. Dangling registrations are limited to that task.
// LintTask は `ws worktree lint` の本体。task の中を検査し、ぶら下がった登録もその task のものだけに絞る。
// 戻り値は問題のスライス(無ければ空)とエラー。
//
// `func(found string) bool { return found == task }` は無名関数で、外側の task を取り込んで(クロージャ)使う。
func (a *App) LintTask(ctx context.Context, task string) ([]Issue, error) {
	// `&T{}` で構造体を作り、そのポインタを得る。
	c := &issueCollector{}
	if err := a.lintTask(ctx, c, task); err != nil {
		return nil, err
	}
	if err := a.lintDanglingRegistrations(ctx, c, func(found string) bool { return found == task }); err != nil {
		return nil, err
	}
	return c.issues, nil
}

// LintWorkspace checks every task, plus dangling registrations that belong to no task.
// `ws lint` の本体。全 task を検査する。ぶら下がった登録は、ディレクトリが無いので各 task の検査では見つからない。
// そのため別に調べ、task に属するものも属さないものも全部報告する(accepts が常に true を返す)。
func (a *App) LintWorkspace(ctx context.Context) ([]Issue, error) {
	c := &issueCollector{}
	if fsx.Exists(a.layout.TasksRoot()) {
		tasks, err := fsx.SubdirNames(a.layout.TasksRoot())
		if err != nil {
			return nil, err
		}
		for _, task := range tasks {
			if err := a.lintTask(ctx, c, task); err != nil {
				return nil, err
			}
		}
	}
	if err := a.lintDanglingRegistrations(ctx, c, func(string) bool { return true }); err != nil {
		return nil, err
	}
	return c.issues, nil
}
