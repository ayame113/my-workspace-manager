// このファイルは、シェル補完(Tab キーで task 名やブランチ名の候補を出す機能)の候補を返す関数を集めている。
// cobra が補完用の隠しコマンドを用意していて、`source <(ws completion zsh)` で読み込んだシェルが、
// Tab が押されるたびに ws を呼び出して候補を取得する。ここの関数はそのとき呼ばれる。
// 各関数は「候補のリスト」と「シェルへの指示(ShellCompDirective)」の 2 つを返す。
package cli

import (
	"slices"

	"github.com/spf13/cobra"

	"github.com/ayame113/my-workspace-manager/internal/app"
)

// completeFunc is the signature cobra expects for a completion function.
//
// completeFunc は、cobra の補完関数の型に付けた別名(型エイリアス)。
// `type A = B` の `=` が付く形はエイリアスで、A と B は完全に同じ型として扱われる。
// TypeScript の `type CompleteFunc = (...) => ...` に近い。長い関数型を何度も書かずに済む。
// 引数は、補完中のコマンド、すでに入力された位置引数、いま入力中の文字列。
// 戻り値は、候補のスライスと、シェルに対する指示(ShellCompDirective)。
// cobra.Completion は string の別名で、候補 1 つを表す。
type completeFunc = func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective)

// candidates converts a list lookup result into a completion result.
//
// candidates は、「名前の一覧と error」を返す関数の結果を、補完関数の戻り値の形に変換する。
// `candidates(r.app.RepoNames())` のように、複数の戻り値を返す関数の結果をそのまま引数に渡せる。
// 一覧の取得に失敗したときは ShellCompDirectiveError を返して、補完が失敗したことをシェルに伝える。
// 成功したときの ShellCompDirectiveNoFileComp は「候補が無くても、ファイル名の補完にはフォールバックしないで」という指示。
// ws の引数はファイルパスではなく task 名やブランチ名なので、ファイル名が出ると邪魔になる。
func candidates(names []string, err error) ([]cobra.Completion, cobra.ShellCompDirective) {
	if err != nil {
		// nil はスライスのゼロ値で、「候補なし」を意味する。
		return nil, cobra.ShellCompDirectiveError
	}
	// names は []string、戻り値は []cobra.Completion(string の別名のスライス)だが、
	// 別名なので同じ型として扱え、変換せずにそのまま返せる。
	return names, cobra.ShellCompDirectiveNoFileComp
}

// disableFileCompletion stops shells from offering file names where ws takes no paths.
//
// disableFileCompletion は、cmd とそのすべてのサブコマンドについて、ファイル名の補完を無効にする。
// 補完関数(ValidArgsFunction)も固定の候補(ValidArgs)も持たないコマンドにだけ cobra.NoFileCompletions を設定する。
// 何も設定しないと、cobra は引数の位置でファイル名を補完してしまう。
// 自分の中でサブコマンドごとに自分自身を呼び出す再帰関数。rootCommand で最後に root に対して 1 回だけ呼べば、
// コマンドの木全体に行き渡る。
func disableFileCompletion(cmd *cobra.Command) {
	// ValidArgsFunction は「位置引数の補完候補を動的に返す関数」を入れるフィールド。
	// 関数型のフィールドの未設定は nil。ValidArgs は固定の候補の文字列スライス。
	if cmd.ValidArgsFunction == nil && len(cmd.ValidArgs) == 0 {
		cmd.ValidArgsFunction = cobra.NoFileCompletions
	}
	// cmd.Commands() はサブコマンドのスライス。
	for _, child := range cmd.Commands() {
		disableFileCompletion(child)
	}
}

// completeTasks completes task names.
//
// completeTasks は task 名の補完。`ws open <Tab>` や `--task <Tab>` で使われる。
// 引数名を省略して型だけを書いている(*cobra.Command, []string, string)のは、この関数では引数を使わないため。
// こうして書いても、completeFunc と同じ形なので、そのまま補完関数として登録できる。
// slices.Sorted(slices.Values(names)) は、スライスを並べ替えた新しいスライスを作る(元の names は変えない)。
// TypeScript で言えば `names.toSorted()` に近い。
func (r *runner) completeTasks(*cobra.Command, []string, string) ([]cobra.Completion, cobra.ShellCompDirective) {
	names, err := r.app.TaskNames()
	return candidates(slices.Sorted(slices.Values(names)), err)
}

// completeRepos completes the names of the bare repos in repos/.
//
// completeRepos は repos/ にある bare repo の名前の補完。--repo フラグで使われる。
// r.app.RepoNames() が (名前の一覧, error) の 2 つを返すので、そのまま candidates に渡している。
func (r *runner) completeRepos(*cobra.Command, []string, string) ([]cobra.Completion, cobra.ShellCompDirective) {
	return candidates(r.app.RepoNames())
}

// completeAddBranch needs --repo given earlier on the line.
//
// completeAddBranch は ws worktree add の --branch の補完関数を作って返す。
// 戻り値が関数(completeFunc)になっていて、返される関数は opts を覚えている。これがクロージャ
// (外側の変数を捕まえて使い続ける関数。TypeScript / JavaScript のクロージャと同じもの)。
// opts はポインタで受け取っているため、cobra がフラグを解析して opts.Repo などに値を入れた後の内容を、
// 補完が呼ばれた時点で読める。--repo が先に入力されていなければ(opts.Repo が空)、どの repo か分からないので候補は出さない。
// --readonly のときは、origin にあるブランチだけを候補にする(readonly の worktree は origin/<branch> を参照するため)。
func (r *runner) completeAddBranch(opts *app.AddOptions) completeFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if opts.Repo == "" {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		// cmd.Context() は、実行時に渡された ctx。git の実行にキャンセルを伝えるために渡す。
		branches, err := r.app.BranchesOf(cmd.Context(), opts.Repo)
		if opts.ReadOnly {
			return candidates(branches.Remote, err)
		}
		return candidates(branches.All(), err)
	}
}

// completeBase completes --base with the remote branches of the chosen repo, written as origin/<branch>.
//
// completeBase は ws worktree add の --base(新規ブランチの起点)の補完関数を作って返す。
// 仕組みは completeAddBranch と同じクロージャ。起点は origin/main のように「origin/」付きで書くので、
// リモートブランチ名の先頭に "origin/" を付けた候補を作る。
func (r *runner) completeBase(opts *app.AddOptions) completeFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if opts.Repo == "" {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		branches, err := r.app.BranchesOf(cmd.Context(), opts.Repo)
		// err が非 nil でも、先に進む。candidates が err を見て、失敗なら bases を使わずに Error を返す。
		// make でスライスを作り、添字で代入している(bases[i] = ...)。TypeScript の `branches.Remote.map(...)` に相当する。
		bases := make([]string, len(branches.Remote))
		for i, branch := range branches.Remote {
			bases[i] = "origin/" + branch
		}
		return candidates(bases, err)
	}
}

// completeRenameBranch offers the branches checked out in the task's work worktrees, narrowed by --repo when given.
//
// completeRenameBranch は ws worktree rename の --branch の補完関数を作って返す(クロージャ)。
// task が決まらないとき(task の外にいて --task も無い)は、候補を出さない。
// rename の対象は作業用 worktree が checkout しているブランチなので、その一覧から候補を作る。
// --repo が指定されていればその repo のものだけに絞る。
func (r *runner) completeRenameBranch(opts *app.RenameOptions) completeFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		task, err := r.app.ResolveTask(opts.Task)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		// `all, err := ...` の := は、左辺に 1 つでも新しい変数(all)があれば使える。err は既に宣言済みなので、再代入になる。
		all, err := r.app.RenameCandidates(cmd.Context(), task)
		// `var branches []string` は、初期値なしで変数を宣言する書き方。スライスのゼロ値は nil で、
		// nil のスライスにも append できる(要素が 1 つも無ければ nil のまま)。
		var branches []string
		for _, c := range all {
			if opts.Repo == "" || c.Repo == opts.Repo {
				branches = append(branches, c.Branch)
			}
		}
		return candidates(branches, err)
	}
}
