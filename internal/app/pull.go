// pull.go は `ws worktree pull` の本体。task 内の worktree を、リモート(origin)の最新に追従させる。
// まず repo ごとに 1 回だけ `git fetch origin --prune` し、続けて worktree ごとに更新する。
// 作業用 worktree は `git merge --ff-only`(早送りだけ。マージコミットも rebase も作らない)、
// readonly は最新の `origin/<branch>` に detached HEAD で付け直す。
// 安全に更新できないもの(未コミット変更あり、upstream なし、分岐など)は更新せず、理由を表示してスキップする。
// ネットワーク(ssh)を使うので、エージェントではなく人間が実行する前提のコマンド。
package app

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ayame113/my-workspace-manager/internal/gitexec"
	"github.com/ayame113/my-workspace-manager/internal/layout"
)

// 結果の状態を表す文字列定数。Go の const は型のないまま文字列も数値も定義できる(TypeScript の `as const` なオブジェクトに近い)。
// 大文字で始まるものは他のパッケージ(CLI の表示側)から参照でき、小文字のものはこのパッケージ内だけで使う。
const (
	PullUpdated    = "updated"
	PullUpToDate   = "up-to-date"
	PullSkipped    = "skipped"
	PullError      = "error"
	pullNoUpstream = "upstream なし"
	pullDirty      = "未コミット変更あり"
)

// PullEntry は worktree 1 つ分の結果で、report コールバックで 1 件ずつ呼び出し元に渡す。
// Kind は作業用か readonly か、Name は worktree 名、Status は上の定数のどれか、Detail は補足(ブランチ名や理由)。
type PullEntry struct {
	Kind   string
	Name   string
	Status string
	Detail string
}

// PullResult は全体の集計。Total は worktree の数、Failed は error になった数(CLI はこれが 1 以上なら終了コード 1 にする)。
type PullResult struct {
	Total  int
	Failed int
}

// pullOutcome は 1 つの worktree の更新結果(内部用)。PullEntry に詰め替えて報告される。
type pullOutcome struct {
	status string
	detail string
}

// failureOutcome は、エラーを「error 状態の結果」に変換する。
// `error` は Go の組み込みの interface で、Error() メソッドで文字列にできる。
func failureOutcome(err error) pullOutcome {
	return pullOutcome{status: PullError, detail: strings.TrimSpace(err.Error())}
}

// updateReadonly は readonly worktree を、対応する `origin/<branch>` の最新に付け直す。
// 引数: bare(bare repo のパス)、worktree(readonly worktree のパス)、slug(ディレクトリ名の slug 部分)。
// 戻り値: 結果(pullOutcome)と、予期しない失敗のときのエラー。
//
// readonly はブランチ名ではなく slug(ブランチ名を小文字化・記号をハイフンにしたもの)しか名前に残っていないので、
// origin のブランチのうち slug が一致するものを探して、対象のブランチを特定する。
// `feature/a` と `feature-a` のように slug が衝突すると 1 つに決まらないので error にする。
//
// 結果とエラーの 2 つを返す設計: 「スキップ」「一致なし」のような想定内の状況は結果(status)で表し、
// git の実行失敗のような想定外のものは error で返す。呼び出し側(Pull)が後者も error 状態の結果に変換する。
func (a *App) updateReadonly(ctx context.Context, bare, worktree, slug string) (pullOutcome, error) {
	// 未コミット変更があれば更新しない(readonly には手を加えない前提なので、変更があること自体が普通ではない)。
	// `err` が非 nil のときも、ここで抜ける(その場合は `pullOutcome` は無視され、エラーが返る)。
	if dirty, err := gitexec.IsDirty(ctx, a.git, worktree); err != nil || dirty {
		return pullOutcome{status: PullSkipped, detail: pullDirty}, err
	}
	// origin のブランチ名の一覧を 1 行 1 ブランチで取得する。`refs/remotes/origin/feature/a` の先頭 3 要素を落とすと `feature/a` になる。
	// gitexec.Output は git を実行して標準出力を返し、終了コードが 0 以外ならエラーにするヘルパー。
	out, err := gitexec.Output(ctx, a.git, bare, "for-each-ref", "--format=%(refname:strip=3)", "refs/remotes/origin/")
	// エラーを戻り値で受け取り、nil でなければすぐ呼び出し元に返すのが Go のエラー処理の定型。
	// 「失敗しうる関数は error を最後の戻り値に持つ」ので、呼ぶたびにこの形で確認する(try/catch の代わり)。
	if err != nil {
		return pullOutcome{}, err
	}
	// slug が一致する origin ブランチを集める。origin/HEAD(既定ブランチを指す別名)は除く。
	var matches []string
	for name := range strings.SplitSeq(out, "\n") {
		if name != "" && name != "HEAD" && layout.BranchSlug(name) == slug {
			matches = append(matches, name)
		}
	}
	// 一致が 0 件(origin から消えた)または 2 件以上(slug の衝突)だと、付け直す先が決まらないので error にする。
	// `%d` と `%s` は fmt.Sprintf の書式指定(整数と文字列)で、strings.Join はスライスを区切り文字で連結する(JS の `join`)。
	if len(matches) != 1 {
		return pullOutcome{status: PullError, detail: fmt.Sprintf("slug に一致する origin ブランチが %d 件: %s", len(matches), strings.Join(matches, " "))}, nil
	}
	target := "origin/" + matches[0]
	// 今のコミットと、付け直す先のコミットを比べ、同じなら何もしない。
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
	// detached HEAD で付け直す。`_` は戻り値(出力)を使わないという意味。
	if _, err := gitexec.Output(ctx, a.git, worktree, "checkout", "--quiet", "--detach", target); err != nil {
		return pullOutcome{}, err
	}
	// 表示用の短いコミットハッシュ。ここで err があっても、結果と一緒にそのまま返す(呼び出し側が error として扱う)。
	short, err := gitexec.ShortHead(ctx, a.git, worktree)
	return pullOutcome{status: PullUpdated, detail: fmt.Sprintf("%s (%s)", target, short)}, err
}

// updateWork は作業用 worktree を、upstream の最新に早送り(fast-forward)で更新する。
// fast-forward は「ローカルが upstream の祖先であるとき、ブランチの位置を進めるだけ」の更新で、
// マージコミットを作らず、履歴も書き換えない。upstream とローカルが分岐していれば失敗するので、スキップして理由を表示する。
// スキップの条件: detached HEAD、未コミット変更あり、upstream なし、早送りできない。
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
	// `@{upstream}` が解決できるか(終了コードが 0 か)で、upstream の有無を判定する。
	hasUpstream, err := gitexec.Succeeds(ctx, a.git, worktree, "rev-parse", "--verify", "--quiet", "@{upstream}")
	if err != nil {
		return pullOutcome{}, err
	}
	if !hasUpstream {
		return pullOutcome{status: PullSkipped, detail: pullNoUpstream}, nil
	}
	// 更新の前後でコミットを比べ、「更新された」か「すでに最新」かを区別するために、先に記録しておく。
	before, err := gitexec.ShortHead(ctx, a.git, worktree)
	if err != nil {
		return pullOutcome{}, err
	}
	// `--ff-only` は、早送りできないときは何も変更せずに失敗する指定。
	// a.git.Run は 0 以外の終了コードをエラーにせず、ExitCode で返す(ここでは失敗を「スキップ」として扱いたいため)。
	merge, err := a.git.Run(ctx, worktree, "merge", "--quiet", "--ff-only", "@{upstream}")
	if err != nil {
		return pullOutcome{}, err
	}
	if merge.ExitCode != 0 {
		// 先頭は hint: の説明が続くことがあるので、結論の最終行(fatal: ...)を使う
		// `lines[len(lines)-1]` は最後の要素(Go に `.at(-1)` や負のインデックスは無い)。
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
// Pull は `ws worktree pull` の本体で、CLI から呼ばれる。
// 引数: task は解決済みの task 名、report は worktree 1 つの処理が終わるたびに呼ばれるコールバック(CLI が表示する)。
// 戻り値: 集計と、worktree の一覧を取れなかったときのエラー。個々の worktree の失敗は、戻り値の error ではなく、
// 結果の Failed に数えて report で伝える(1 つ失敗しても、残りの worktree の処理は続ける)。
func (a *App) Pull(ctx context.Context, task string, report func(PullEntry)) (PullResult, error) {
	// task 内の worktree の一覧(作業用と readonly の両方)。
	entries, err := a.worktreeEntries(task)
	if err != nil {
		return PullResult{}, err
	}
	// task 内の worktree が使う repo を、重複なく、出てきた順に集める。fetch は repo ごとに 1 回で済ませるため。
	var repos []string
	for _, entry := range entries {
		// `<repo>--<slug>` の名前から repo 部分を取り出す。
		repo := layout.SplitWorktreeName(entry.Name).Repo
		// スライスに含まれていなければ追加する(スライスで集合を代用)。
		if !slices.Contains(repos, repo) {
			repos = append(repos, repo)
		}
	}
	// fetch に失敗した repo の結果を、repo 名をキーにして覚えておく。
	// その repo の worktree は、更新せずにこの結果をそのまま報告する(古い origin に追従しても意味がないため)。
	fetchFailures := map[string]pullOutcome{}
	for _, repo := range repos {
		// bare repo(`repos/<repo>.git`)が存在することを確認する。無ければエラー。
		bare, err := a.requireBare(ctx, repo)
		// ここでの err は、直前の requireBare の結果。成功したときだけ fetch する。
		// `--prune` で、origin から消えたブランチの `origin/<branch>` も削除する。
		// fetch は ssh を使うので、鍵のパスフレーズが必要な環境では端末のある場所で実行しないと失敗する。
		if err == nil {
			_, err = gitexec.Output(ctx, a.git, bare, "fetch", "--quiet", "origin", "--prune")
		}
		if err != nil {
			fetchFailures[repo] = failureOutcome(err)
		}
	}
	// ここから、worktree を 1 つずつ更新して報告する。
	result := PullResult{Total: len(entries)}
	for _, entry := range entries {
		parsed := layout.SplitWorktreeName(entry.Name)
		path := entry.path(a.layout, task)
		// map の存在確認。`failed` は、この repo の fetch が失敗していたか。
		outcome, failed := fetchFailures[parsed.Repo]
		if !failed {
			// readonly か作業用かで、更新のしかたを切り替える。
			if entry.Kind == KindReadonly {
				outcome, err = a.updateReadonly(ctx, a.layout.BarePath(parsed.Repo), path, parsed.Slug)
			} else {
				outcome, err = a.updateWork(ctx, path)
			}
			if err != nil {
				// 想定外のエラーも、この worktree の error 状態の結果にして、次の worktree の処理を続ける。
				outcome = failureOutcome(err)
			}
		}
		// error になった worktree を数える。
		if outcome.status == PullError {
			result.Failed++
		}
		// コールバックを呼んで、1 件分の結果を呼び出し元に渡す。
		report(PullEntry{Kind: entry.Kind, Name: entry.Name, Status: outcome.status, Detail: outcome.detail})
	}
	return result, nil
}
