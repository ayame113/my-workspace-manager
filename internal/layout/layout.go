// Package layout は、ワークスペース(既定は ~/ws)配下のパスの組み立てと、task 名・repo 名・ブランチ名の命名規則を扱う。
// ファイル I/O や git は呼ばず、文字列を受け取って文字列や bool を返すだけの純粋な関数が中心なので、テストしやすい。
// 依存の向きは app → layout で、layout は他の internal パッケージを import しない(一番下の層)。
//
// ディレクトリ構成は次のとおり固定されている。
//
//	<Root>/repos/<repo>.git                   bare repo(作業ツリーを持たない git の保管庫)
//	<Root>/tasks/<task>/<repo>--<slug>        作業用 worktree(slug はブランチ名から作る)
//	<Root>/tasks/<task>/_readonly/...         参照専用の readonly worktree
package layout

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// Layout はワークスペースのルートディレクトリを保持し、そこから各種パスを導出する。
//
// struct(構造体)は TypeScript の interface / type { Root: string } に近いが、フィールドの型だけでなく
// 値そのものを持つ。Go にはクラスが無く、struct にメソッドを生やして代用する。
// 小さい値なので、ポインタではなく値のままコピーして渡している。
type Layout struct {
	Root string
}

// FromEnv は環境変数から Layout を作る。WS_ROOT があればそれを、無ければ $HOME/ws をルートにする。
//
// getenv は「環境変数名を受け取って値を返す関数」を引数に取る(関数型の引数)。
// 本番では os.Getenv を渡し、テストでは固定の値を返す関数を渡せるので、os.Setenv を使わずに済む。
// TypeScript なら (name: string) => string という型の引数に相当する。
func FromEnv(getenv func(string) string) Layout {
	// if の中で変数を宣言して(root :=)、その値を条件に使う書き方。root はこの if ブロックの中でだけ有効。
	if root := getenv("WS_ROOT"); root != "" {
		return Layout{Root: root}
	}
	return Layout{Root: getenv("HOME") + "/ws"}
}

// 以降の TasksRoot などは Layout に生えたメソッド。
// 「func (l Layout) 名前()」の (l Layout) をレシーバと呼び、TypeScript のクラスメソッドの this に相当する(名前は自分で決める)。
// 呼び出しは layout.TasksRoot() のように値.メソッド() と書く。

// TasksRoot は全 task を置くディレクトリ(<Root>/tasks)を返す。
func (l Layout) TasksRoot() string { return l.Root + "/tasks" }

// ReposRoot は bare repo を置くディレクトリ(<Root>/repos)を返す。
func (l Layout) ReposRoot() string { return l.Root + "/repos" }

// TaskDir は task のディレクトリ(<Root>/tasks/<task>)を返す。task 名の妥当性は検証しない。
func (l Layout) TaskDir(task string) string { return l.TasksRoot() + "/" + task }

// ReadonlyDir は task 内の readonly worktree を置くディレクトリ(<task>/_readonly)を返す。
// readonly は「main など、複数の task から参照するブランチ」を見るための worktree。
// git は同じブランチを複数の worktree で同時に checkout できないが、readonly は origin/<branch> を detached HEAD で checkout することでこの制約を避ける。
func (l Layout) ReadonlyDir(task string) string { return l.TaskDir(task) + "/_readonly" }

// BarePath は repo 名から bare repo のパス(<Root>/repos/<repo>.git)を返す。
// bare repo は git の履歴だけを持ち作業ツリーを持たないリポジトリで、全 worktree の共通の元になる。
func (l Layout) BarePath(repo string) string { return l.ReposRoot() + "/" + repo + ".git" }

// CompactIndexPath は ws compact が処理済みファイルを記録するファイルのパスを返す。
func (l Layout) CompactIndexPath() string { return l.Root + "/.compact-index" }

// isSlugChar は c が slug に使える文字(半角小文字 a-z と数字 0-9)かどうかを返す。
// 名前の先頭が小文字(isSlugChar)の関数は、このパッケージの中だけで使える非公開のもの。
//
// rune は Unicode の 1 文字を表す整数型(int32 の別名)。Go の文字列は UTF-8 のバイト列なので、
// 文字単位で扱うときは rune に変換する。'a' のようにシングルクォートで書くと rune のリテラルになる。
func isSlugChar(c rune) bool {
	return ('a' <= c && c <= 'z') || ('0' <= c && c <= '9')
}

// IsValidTaskName reports whether name matches `^[a-z0-9]+(-[a-z0-9]+)*$`. The same rule applies to branch slugs.
//
// つまり task 名は「半角小文字・数字・ハイフンのみ。先頭と末尾にハイフン不可、ハイフンの連続も不可」。
// "--" を禁止しているのは、worktree のディレクトリ名 `<repo>--<slug>` で repo と slug の区切りに "--" を使うため。
// slug も同じ規則なので、この関数を slug の検証にも使い回している。
func IsValidTaskName(name string) bool {
	// 条件を && で複数行につなぐ書き方。どれか 1 つでも false なら全体が false になり、後ろは評価されない(短絡評価)。
	return name != "" &&
		!strings.HasPrefix(name, "-") &&
		!strings.HasSuffix(name, "-") &&
		!strings.Contains(name, "--") &&
		// ContainsFunc は「条件を満たす文字が 1 つでもあるか」を調べる。
		// func(c rune) bool { ... } は無名関数(TypeScript のアロー関数 (c) => ... に相当)。
		// ここでは「slug 用の文字でもハイフンでもない文字」が 1 つでもあれば不正と判断する。
		!strings.ContainsFunc(name, func(c rune) bool { return !isSlugChar(c) && c != '-' })
}

// IsRepoNameShape reports whether name matches `^[A-Za-z0-9][A-Za-z0-9._-]*$`.
//
// repo 名の「形」だけを調べる(英数字で始まり、英数字と . _ - のみ)。"--" を含むかどうかは見ない。
// "--" の禁止は ValidateRepoName 側で行う。ParseWorktreeName は最初の "--" で切った後の repo 部分にこの関数を使うので、
// そちらでは "--" を含むかどうかを改めて調べる必要が無い。
func IsRepoNameShape(name string) bool {
	// 関数の中に関数を定義して変数に入れられる(クロージャ)。TypeScript の const f = (c) => ... と同じ。
	// 外側の isSlugChar をそのまま使える。
	alphanumeric := func(c rune) bool {
		return isSlugChar(c) || ('A' <= c && c <= 'Z')
	}
	// name[0] は文字列の 0 バイト目(byte)なので、rune(...) で文字に変換して渡している。
	// 空文字だと name[0] は実行時エラーになるため、先に name == "" を判定して || で短絡させている。
	if name == "" || !alphanumeric(rune(name[0])) {
		return false
	}
	return !strings.ContainsFunc(name, func(c rune) bool {
		return !alphanumeric(c) && c != '.' && c != '_' && c != '-'
	})
}

// BranchSlug lowercases branch, collapses every run of characters outside [a-z0-9] into a single "-", and trims leading and trailing "-".
//
// ブランチ名をディレクトリ名に使える形(slug)に変換する。例: "Feature/Add_Login" → "feature-add-login"。
// 返り値が空文字になることもある(例: "日本語" のように使える文字が 1 つも無い場合)。呼び出し側は SlugOf を使えばエラーとして受け取れる。
func BranchSlug(branch string) string {
	// strings.Map は文字列の各文字を関数で変換する。まず小文字にし(ToLower)、slug に使えない文字は "-" に置き換える。
	replaced := strings.Map(func(c rune) rune {
		if isSlugChar(c) {
			return c
		}
		return '-'
	}, strings.ToLower(branch))
	// FieldsFunc は区切り文字で分割し、空の要素を捨てる。"a--b-" を "-" で割ると ["a", "b"] になる。
	// これを Join で "-" でつなぎ直すと、連続したハイフンが 1 つにまとまり、先頭と末尾のハイフンも消える。
	return strings.Join(strings.FieldsFunc(replaced, func(c rune) bool { return c == '-' }), "-")
}

// SlugOf は BranchSlug の結果を返すが、slug が空になる場合はエラーにする。
//
// Go の関数は複数の値を返せる。慣習として、最後の戻り値に error を置き、成功なら nil(= エラー無し)を返す。
// 呼び出し側は slug, err := SlugOf(branch) と受け取り、if err != nil { ... } でエラーを処理する。
// TypeScript の例外(throw)の代わりに、エラーを普通の戻り値として扱うのが Go の流儀。
func SlugOf(branch string) (string, error) {
	slug := BranchSlug(branch)
	if slug == "" {
		// エラー時は、他の戻り値にゼロ値("")を入れて返す。fmt.Errorf は printf 形式でエラーを作る。
		return "", fmt.Errorf("ブランチ名 '%s' から slug を作れません", branch)
	}
	return slug, nil
}

// ValidateTaskName は task 名が規則(IsValidTaskName)に合っていなければエラーを返す。合っていれば nil を返す。
// ユーザーに表示するメッセージを持つので、bool を返す IsValidTaskName とは別に用意している。
func ValidateTaskName(task string) error {
	if !IsValidTaskName(task) {
		return fmt.Errorf("task 名は [a-z0-9-] のみ(先頭末尾・連続ハイフン不可): '%s'", task)
	}
	return nil
}

// ValidateRepoName は repo 名が規則に合っていなければエラーを返す。形が正しく、かつ "--" を含まないことを要求する。
// "--" は worktree のディレクトリ名 `<repo>--<slug>` の区切りなので、repo 名に含められない。
func ValidateRepoName(repo string) error {
	if !IsRepoNameShape(repo) || strings.Contains(repo, "--") {
		return fmt.Errorf("repo 名が不正です('--' を含まず英数字 . _ - のみ): '%s'", repo)
	}
	return nil
}

// WorktreeName は worktree のディレクトリ名 `<repo>--<slug>` を、repo と slug に分けたもの。
// 例: "app-web--fix-login" → {Repo: "app-web", Slug: "fix-login"}。
type WorktreeName struct {
	Repo string
	Slug string
}

// SplitWorktreeName splits `<repo>--<slug>` at the first "--". Slug is empty when there is none.
//
// 検証はしない単純な分割。名前が規則に合っているかも確認したいときは ParseWorktreeName を使う。
func SplitWorktreeName(name string) WorktreeName {
	// strings.Cut は最初の区切りで 2 つに切り、(前, 後, 区切りが見つかったか) の 3 つを返す。
	// 3 つ目は使わないので、_(捨てる変数)に代入している。使わない値は必ず _ で受ける決まりで、そうしないとコンパイルエラーになる。
	repo, slug, _ := strings.Cut(name, "--")
	return WorktreeName{Repo: repo, Slug: slug}
}

// ParseWorktreeName splits name only when both parts follow the naming rules.
//
// 戻り値の 2 つ目は「成功したか」の bool。Go ではエラーの詳細が不要な場面で、(値, ok) の形を使う慣習がある。
// 失敗時は WorktreeName{}(全フィールドが空のゼロ値)と false を返す。
// ws が作った worktree かどうかの判定に使う(規約外のディレクトリは lint で検出される)。
func ParseWorktreeName(name string) (WorktreeName, bool) {
	parsed := SplitWorktreeName(name)
	if !IsRepoNameShape(parsed.Repo) || !IsValidTaskName(parsed.Slug) {
		return WorktreeName{}, false
	}
	return parsed, true
}

// TaskUnder returns the first path component of path below tasksRoot.
//
// 例: tasksRoot が "/ws/tasks" のとき、"/ws/tasks/foo/app--x/src" からは "foo" が取れる。
// どの task の配下でもなければ ("", false) を返す。カレントディレクトリがどの task に属するかを調べるために使う。
func TaskUnder(tasksRoot, path string) (string, bool) {
	// CutPrefix は先頭が一致すれば、それを取り除いた残りと true を返す。一致しなければ ("", false)。
	// 末尾に "/" を足しているのは、"/ws/tasks2/..." のような別のディレクトリを誤って一致させないため。
	relative, ok := strings.CutPrefix(path, tasksRoot+"/")
	if !ok {
		return "", false
	}
	task, _, _ := strings.Cut(relative, "/")
	// 戻り値の位置に式をそのまま書ける。task が空("/ws/tasks/" ちょうどなど)なら false になる。
	return task, task != ""
}

// WorktreePath は作業用 worktree のパス <dir>/<repo>--<slug> を返す。dir には task のディレクトリなどを渡す。
// filepath.Join は OS に合ったパス区切りで連結し、"//" や末尾の "/" も整える(Node.js の path.join に相当)。
func WorktreePath(dir, repo, slug string) string {
	return filepath.Join(dir, repo+"--"+slug)
}

// RepoTaskPrefix joins the slugs of repos into a task-name prefix, writing their common leading words once
// (app-web, app-api → app-web-api). It returns "" when no repo has a usable slug.
//
// `ws new` の対話で、選んだ repo から task 名の初期値(の前半)を作るために使う。
// 手順は、(1) 各 repo 名を slug 化してハイフンで単語に分ける、(2) 全 repo に共通する先頭の単語を求める、
// (3) 共通部分を 1 回だけ書き、各 repo の残りの単語を続ける、の 3 段階。
func RepoTaskPrefix(repos []string) string {
	// var words [][]string は「文字列の配列」の配列(string[][])。初期値は nil(空)で、append で増やせる。
	var words [][]string
	for _, repo := range repos {
		// if の先頭で slug を作って、その場で条件に使う。slug が空の repo は無視する。
		if slug := BranchSlug(repo); slug != "" {
			words = append(words, strings.Split(slug, "-"))
		}
	}
	if len(words) == 0 {
		return ""
	}
	// 最初の repo の単語列を出発点にして、他の repo と比べながら共通の先頭部分だけに削っていく。
	common := words[0]
	for _, w := range words[1:] {
		n := 0
		for n < len(common) && n < len(w) && common[n] == w[n] {
			n++
		}
		// common[:n] はスライス(配列の一部分)で、TypeScript の common.slice(0, n) に相当する。
		common = common[:n]
	}
	// スライスは元の配列を共有するので、append で common 側のデータを壊さないよう Clone でコピーしてから使う。
	parts := slices.Clone(common)
	for _, w := range words {
		// 共通部分より後ろの単語を、ハイフンでつなぎ直す。
		rest := strings.Join(w[len(common):], "-")
		// 空(共通部分だけの repo)や、すでに入っているもの(同じ repo を 2 回選んだ場合など)は足さない。
		if rest != "" && !slices.Contains(parts, rest) {
			parts = append(parts, rest)
		}
	}
	return strings.Join(parts, "-")
}
