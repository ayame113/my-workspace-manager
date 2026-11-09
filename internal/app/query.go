// query.go: task、repo、ブランチ、起点などの「一覧・問い合わせ」系のメソッドを置いています。
// 主に対話入力(huh)で選択肢を出すために cli / prompt 層から呼ばれ、状態は変えず調べるだけです。
// Go 特有の書き方の説明は app.go に書いてあります。ここでは新しく出てくるもの(map、strings.CutSuffix、イテレータなど)を補足します。

package app

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/ayame113/my-workspace-manager/internal/fsx"
	"github.com/ayame113/my-workspace-manager/internal/gitexec"
	"github.com/ayame113/my-workspace-manager/internal/layout"
)

// TaskNames は存在する task の名前を、最近作業した順に返します。tasks ディレクトリが無ければ空(nil)を返します。
// TaskNames lists the existing tasks, most recently active first.
func (a *App) TaskNames() ([]string, error) {
	if !fsx.Exists(a.layout.TasksRoot()) {
		return nil, nil
	}
	names, err := fsx.SubdirNames(a.layout.TasksRoot())
	if err != nil {
		return nil, err
	}
	// map[string]time.Time は「string をキーに time.Time を値に持つ連想配列」です(TypeScript の Map<string, Date>)。
	// make(map..., n) で作ります。map は読み取るだけなら nil でも安全ですが、書き込む前には make で作っておく必要があります。
	updated := make(map[string]time.Time, len(names))
	for _, name := range names {
		entries, err := a.worktreeEntries(name)
		if err != nil {
			return nil, err
		}
		// map への代入は `m[key] = value` です。task ごとの最終更新時刻を覚えておきます。
		updated[name] = a.taskActivity(name, entries)
	}
	// 名前のスライスを、map から引いた更新時刻の新しい順に並べ替えます(newestFirst は activity.go)。
	// `func(name string) time.Time { return updated[name] }` は無名関数で、「名前から時刻を取り出す関数」を渡しています。
	newestFirst(names, func(name string) time.Time { return updated[name] })
	return names, nil
}

// RepoNames は repos/ 配下の bare repo(<repo>.git)から、.git を除いた repo 名を返します。
// RepoNames lists the bare repos under repos/.
func (a *App) RepoNames() ([]string, error) {
	if !fsx.Exists(a.layout.ReposRoot()) {
		return nil, nil
	}
	dirs, err := fsx.SubdirNames(a.layout.ReposRoot())
	if err != nil {
		return nil, err
	}
	// var で宣言したスライスは空(nil)で始まり、append で伸ばせます。
	var repos []string
	for _, dir := range dirs {
		// strings.CutSuffix は「末尾が .git ならそれを除いた文字列と true を返す」関数です。
		// `repo, ok := ...; ok && repo != ""` と if の初期化文で受け、.git で終わらないものと ".git" だけのものを除きます。
		if repo, ok := strings.CutSuffix(dir, ".git"); ok && repo != "" {
			repos = append(repos, repo)
		}
	}
	return repos, nil
}

// Branches は repo のブランチ一覧です。Local はローカルブランチ、Remote は origin のブランチ(最後に fetch した時点の状態)です。
type Branches struct {
	Local  []string
	Remote []string
}

// 日本語の補足: (b Branches) は値レシーバで、Branches のメソッドとして b.All() の形で呼べます。
// slices.Concat で 2 つをつなぎ、slices.Values でイテレータにし、slices.Sorted で並べ替えて、slices.Compact で隣り合う重複を除いています。
// Compact は並べ替え済みであることが前提なので、この順序(Sorted の後に Compact)を入れ替えてはいけません。
// All returns local and origin branches without duplicates, sorted.
func (b Branches) All() []string {
	return slices.Compact(slices.Sorted(slices.Values(slices.Concat(b.Local, b.Remote))))
}

// forEachRef は git for-each-ref で参照(ブランチなど)の名前を取り出します。prefix で範囲を、format で名前の取り方を指定します。
// HEAD(origin/HEAD の指し先を示す特別な参照)は、ブランチではないので除きます。
func (a *App) forEachRef(ctx context.Context, bare, prefix, format string) ([]string, error) {
	// `"--format="+format` は文字列の連結です。出力は 1 行 1 名前のテキストで返ります。
	out, err := gitexec.Output(ctx, a.git, bare, "for-each-ref", "--format="+format, prefix)
	if err != nil {
		return nil, err
	}
	var names []string
	// strings.SplitSeq は文字列を区切って順に返すイテレータ(Go 1.24 以降)です。`for name := range ...` で 1 つずつ受け取り、スライスを作らずに処理できます。
	// 空行と HEAD は捨てます。
	for name := range strings.SplitSeq(out, "\n") {
		if name != "" && name != "HEAD" {
			names = append(names, name)
		}
	}
	return names, nil
}

// BranchesOf は repo のローカルブランチと origin ブランチを返します(origin/HEAD は除く)。
// BranchesOf lists the local and origin branches of a repo, excluding origin/HEAD.
func (a *App) BranchesOf(ctx context.Context, repo string) (Branches, error) {
	// requireBare で bare repo の存在を確認します。無ければここでエラーになります。
	bare, err := a.requireBare(ctx, repo)
	if err != nil {
		return Branches{}, err
	}
	// %(refname:strip=2) は、refs/heads/main から先頭 2 要素を除いて main にする git の書式です。
	local, err := a.forEachRef(ctx, bare, "refs/heads/", "%(refname:strip=2)")
	if err != nil {
		return Branches{}, err
	}
	// こちらは refs/remotes/origin/main から先頭 3 要素を除いて main にします。
	remote, err := a.forEachRef(ctx, bare, "refs/remotes/origin/", "%(refname:strip=3)")
	if err != nil {
		return Branches{}, err
	}
	return Branches{Local: local, Remote: remote}, nil
}

// DefaultBase は、新規ブランチの起点にする参照を返します。origin/HEAD の指し先があればそれ、無ければ origin/main があればそれ、どちらも無ければ空文字です。
// ブランチを新しく切るときの既定の起点を決めるのに使います。
// DefaultBase returns the ref origin/HEAD points to, else origin/main when it exists, else "".
func (a *App) DefaultBase(ctx context.Context, repo string) (string, error) {
	bare := a.layout.BarePath(repo)
	// if / else if の中でも `head, err := ...` と初期化文を使えます。エラーなら return、ExitCode が 0 でなければ「origin/HEAD が設定されていない」ので次の候補へ進みます。
	if head, err := a.git.Run(ctx, bare, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err != nil {
		return "", err
	} else if head.ExitCode == 0 && head.Stdout != "" {
		return head.Stdout, nil
	}
	// origin/main が存在するかを確認します。`err != nil || !exists` なら、エラーのときはそのエラーを、存在しないときは nil エラーで空文字を返します。
	exists, err := gitexec.RefExists(ctx, a.git, bare, "refs/remotes/origin/main")
	if err != nil || !exists {
		return "", err
	}
	return "origin/main", nil
}

// RenameCandidate は、ブランチ名を変えられる worktree の候補(repo とブランチ)です。
type RenameCandidate struct {
	Repo   string
	Branch string
}

// RenameCandidates は task 内の作業用 worktree のうち、ブランチを checkout しているものを返します。readonly(detached)は対象外です。
// RenameCandidates lists the work worktrees of a task that have a branch checked out.
func (a *App) RenameCandidates(ctx context.Context, task string) ([]RenameCandidate, error) {
	names, err := listWorktreeNames(a.layout.TaskDir(task))
	if err != nil {
		return nil, err
	}
	var candidates []RenameCandidate
	for _, name := range names {
		// ParseWorktreeName は、worktree のディレクトリ名 <repo>--<slug> を repo と slug に分けます。規則に合わないものは ok が false になるので飛ばします。
		// `continue` はループの次の要素へ進みます。
		parsed, ok := layout.ParseWorktreeName(name)
		if !ok {
			continue
		}
		branch, err := gitexec.CurrentBranch(ctx, a.git, a.layout.TaskDir(task)+"/"+name)
		if err != nil {
			return nil, err
		}
		if branch != "" {
			candidates = append(candidates, RenameCandidate{Repo: parsed.Repo, Branch: branch})
		}
	}
	return candidates, nil
}
