// app.go: ws の「ロジック層」の中心です。App 構造体と、task / worktree の作成・一覧・ブランチ名変更といった各コマンドのユースケースを置いています。
// UI(cobra や huh)のことは知らず、git の実行は gitexec.Runner、パスの規則は layout、ファイル操作は fsx に任せます。
// Go では「同じディレクトリ = 同じ package」なので、newtask.go / query.go / activity.go も package app として App のメソッドを共有しています。
// (TypeScript で言えば、1 つの class を複数ファイルに分けて書いているイメージです。)

package app

// import は使うパッケージの宣言です。標準ライブラリは "context" のような短い名前、外部やこのリポジトリのものは URL 形式のパスで書きます。
// 使わない import があるとコンパイルエラーになります。TypeScript の import と違い、名前を個別に取り出さず「パッケージ名.関数名」の形(例: fmt.Sprintf)で使います。
import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ayame113/my-workspace-manager/internal/fsx"
	"github.com/ayame113/my-workspace-manager/internal/gitexec"
	"github.com/ayame113/my-workspace-manager/internal/layout"
)

// Config は App を作るときの設定値をまとめた構造体(struct)です。TypeScript の interface / オブジェクト型に近く、フィールドは「名前 型」の順に書きます。
// Go は大文字で始まる名前が package 外に公開(export)され、小文字始まりは package 内だけで使えます(public / private の代わり)。
// Layout や Git は「何を使うか」を外から差し込めるようにしてあり、テストでは偽物に差し替えられます(依存性の注入)。
// Git の型 gitexec.Runner は interface です。Go の interface は「このメソッドを持っていれば満たす」という構造的な型で、TypeScript の interface とほぼ同じ感覚です。
// Getenv / Getwd は「関数を値として持つ」フィールドです。func(string) string は「string を受け取り string を返す関数」の型です。
type Config struct {
	Layout layout.Layout
	Git    gitexec.Runner
	Getenv func(string) string
	Getwd  func() (string, error)
	// IntN returns a value in [0, n). It defaults to math/rand/v2.IntN.
	// フィールド名のあとに型が来ます。ここでは「int を受け取って int を返す関数」を保持しています。テストで乱数を固定するための差し込み口です。
	IntN func(n int) int
}

// App は ws の全ユースケースを持つ本体です。フィールドが小文字始まりなので、外の package からは直接触れず、New 経由と公開メソッドでだけ使います。
// Go に class はなく、「struct + その struct を受け取るメソッド」で同じことをします。
type App struct {
	layout layout.Layout
	git    gitexec.Runner
	getenv func(string) string
	getwd  func() (string, error)
	intn   func(n int) int
}

// New は Config から App を作るコンストラクタ関数です。Go にコンストラクタ構文は無く、New で始まる関数を作る慣習です。
// 戻り値の *App は App へのポインタです。struct をコピーせず同じ実体を共有して渡すために使います(TypeScript でオブジェクトを参照渡しするのと同じ感覚)。
func New(cfg Config) *App {
	// `:=` は「変数の宣言と代入を同時に行う」短縮記法です(TypeScript の const / let に近い)。型は右辺から推論されます。
	intn := cfg.IntN
	// Go の関数型や map、ポインタ、スライスなどは未設定だと nil(TypeScript の null / undefined 相当)になります。
	// IntN が渡されなければ標準ライブラリの rand.IntN を使う、というデフォルト値の処理です。
	if intn == nil {
		intn = rand.IntN
	}
	// &App{...} は struct を作ってそのポインタを返す書き方です。`名前: 値` で各フィールドを初期化します(指定しなかったフィールドは「ゼロ値」になります)。
	// ゼロ値とは型ごとの初期値で、数値は 0、string は ""、bool は false、ポインタ・関数・スライス・map は nil です。
	return &App{layout: cfg.Layout, git: cfg.Git, getenv: cfg.Getenv, getwd: cfg.Getwd, intn: intn}
}

// OutsideTaskError は「カレントディレクトリが task の外だった」ことを表すエラー型です。
// Go の例外は throw ではなく「エラーを戻り値で返す」のが基本です。独自のエラー型を作るには、Error() string というメソッドを持たせるだけで済みます(error interface を満たす)。
// 呼び出し側は errors.As などでこの型かどうかを判定して、メッセージを出し分けられます。
type OutsideTaskError struct {
	TasksRoot string
}

// (e *OutsideTaskError) の部分は「レシーバ」で、このメソッドが OutsideTaskError のポインタに紐づくことを表します。TypeScript の class 内メソッドの this に相当します。
// ポインタレシーバにすると、呼び出し時に struct のコピーが発生せず、メソッド内でフィールドを書き換えることもできます。
func (e *OutsideTaskError) Error() string {
	// fmt.Sprintf は printf 形式の文字列整形です(%s は文字列の埋め込み)。TypeScript のテンプレートリテラル `${...}` に近い役割です。
	return fmt.Sprintf("カレントディレクトリが %s/<task>/ 配下ではありません", e.TasksRoot)
}

// taskFromCwd はカレントディレクトリがどの task 配下かを調べ、task 名を返します(小文字始まりなので package 内専用)。
// Go の関数は複数の値を返せます。(string, error) は「結果と、失敗したときのエラー」の組で、成功なら error は nil、失敗なら string は "" にするのが慣習です。
// TypeScript の「try/catch」の代わりに、呼び出し側が毎回 `if err != nil` で確認します。
func (a *App) taskFromCwd() (string, error) {
	// task の外にいる場合に返すエラーを先に作っておきます。&OutsideTaskError{...} は struct のポインタ(上述)です。
	outside := &OutsideTaskError{TasksRoot: a.layout.TasksRoot()}
	// シンボリックリンクを解決した実パスにします(macOS では /tmp などが実際は別の場所を指すため、文字列の比較がずれるのを避けます)。
	tasksRoot, err := fsx.RealPath(a.layout.TasksRoot())
	// `x, err := f()` は複数の戻り値を受け取る書き方です。直後の `if err != nil { return ... }` が Go 定番のエラー処理で、
	// エラーが起きたらその場で自分の呼び出し元へエラーを返します(throw で上へ伝播させるのを、手で書いている形)。
	if err != nil {
		// ここでは、tasks ディレクトリ自体が無い(= 実パスを解決できない)なら「task の外」と同じ扱いにしています。
		return "", outside
	}
	cwd, err := a.getwd()
	if err != nil {
		// %w は fmt.Errorf で「元のエラーを包んで(ラップして)新しいエラーにする」指定です。
		// メッセージに文脈を足しつつ、元のエラーも errors.Is / errors.As で取り出せるまま残せます(TypeScript の new Error(msg, { cause: err }) に近い)。
		return "", fmt.Errorf("カレントディレクトリを取得できません: %w", err)
	}
	cwd, err = fsx.RealPath(cwd)
	if err != nil {
		return "", fmt.Errorf("カレントディレクトリを解決できません: %w", err)
	}
	// `task, ok := ...` の ok は「見つかったか」を表す bool で、Go でよく使う書き方です(map の取得や型の判定でも同じ形)。
	// 値が無い場合に null を返す代わりに、ok で有無を分けます。
	task, ok := layout.TaskUnder(tasksRoot, cwd)
	if !ok {
		return "", outside
	}
	return task, nil
}

// TaskDir は task のディレクトリのパスを返します(存在するかは調べない)。
// 本体が 1 行の関数は、このように 1 行にまとめて書くこともできます。
// TaskDir returns the directory of task, whether or not it exists.
func (a *App) TaskDir(task string) string { return a.layout.TaskDir(task) }

// ResolveTask resolves a --task value. An empty value or "." means the task containing the current directory.
// A *OutsideTaskError is returned when that fails.
// ResolveTask は、--task の値を「実在する task 名」に確定させます。
// 空文字か "." のときはカレントディレクトリから決めます(ws worktree 配下のコマンドで --task を省略できる理由です)。
// 失敗時は (空文字, エラー) を返します。
func (a *App) ResolveTask(task string) (string, error) {
	if task == "" || task == "." {
		// `if x, err := ...; 条件 {` のように、if の中で変数を宣言する書き方もあります。ここでの fromCwd, err は直前の `:=` で作られ、この関数の以降でも使えます。
		fromCwd, err := a.taskFromCwd()
		if err != nil {
			if task == "" {
				// %w でラップしているので、呼び出し側は errors.As で *OutsideTaskError かどうかを判定できます。--task を省略した場合に限り、指定を促す文を足しています。
				return "", fmt.Errorf("%w。--task <task> を指定してください", err)
			}
			return "", err
		}
		// task = fromCwd は代入です。`:=` は新しい変数を作るときだけで、既にある変数(引数 task も含む)に入れるときは `=` を使います。
		task = fromCwd
	}
	// `if err := f(); err != nil {` は「関数を呼んでその場でエラーを確認する」Go の定番形です。err の有効範囲はこの if ブロック内に限られます。
	if err := layout.ValidateTaskName(task); err != nil {
		return "", err
	}
	if !fsx.Exists(a.layout.TaskDir(task)) {
		return "", fmt.Errorf("task '%s' がありません。先に 'ws new %s' を実行してください", task, task)
	}
	return task, nil
}

// requireBare は、repo の bare repo(repos/<repo>.git。作業ツリーを持たない git の本体データ)が存在することを確認し、そのパスを返します。
// ついでに、bare repo に ws が必要とする git の設定を適用します。
// ctx は context.Context で、キャンセルやタイムアウトを実行中の処理(ここでは git コマンド)へ伝える仕組みです。
// Go では I/O を伴う関数の第 1 引数に ctx を渡す慣習があり、TypeScript の AbortSignal を引き回すのに近いです。
func (a *App) requireBare(ctx context.Context, repo string) (string, error) {
	if err := layout.ValidateRepoName(repo); err != nil {
		return "", err
	}
	bare := a.layout.BarePath(repo)
	if !fsx.Exists(bare) {
		// bare repo が無いときは、ユーザーが次に何をすればよいかをエラーメッセージに含めます(ws clone は別ターミナルで実行してもらう決まりのため)。
		return "", fmt.Errorf("bare repo '%s' がありません。別ターミナルで次を実行してください:\n  ws clone <url> %s", repo, repo)
	}
	if err := gitexec.ApplyRepoConfig(ctx, a.git, bare); err != nil {
		return "", err
	}
	return bare, nil
}

// listWorktreeNames は dir 直下のディレクトリのうち、.git を持つもの(= worktree)の名前だけを返します。
// worktree のディレクトリには、本体の場所を指す .git というファイルが置かれるため、それが目印です。
// dir が無い場合は「0 件」として nil を返します。
// listWorktreeNames returns the names of directories under dir that contain .git. A missing dir yields none.
func listWorktreeNames(dir string) ([]string, error) {
	// nil のスライスは「空のスライス」として扱えるので、`return nil, nil` が「結果なし・エラーなし」になります。
	if !fsx.Exists(dir) {
		return nil, nil
	}
	names, err := fsx.SubdirNames(dir)
	if err != nil {
		return nil, err
	}
	// `var worktrees []string` は「型だけ指定して宣言」する書き方で、初期値はゼロ値(nil のスライス = 空)です。
	// スライスは TypeScript の配列(string[])に近い可変長の列です。
	var worktrees []string
	// `for _, name := range names` は配列を順に処理するループです。range はインデックスと要素の 2 つを返し、使わないほうは _(捨てる記号)で受けます。
	// TypeScript の `for (const name of names)` に近いです。
	for _, name := range names {
		if fsx.Exists(filepath.Join(dir, name, ".git")) {
			// append は要素を足した新しいスライスを返すので、必ず元の変数に代入し直します(TypeScript の push と違い、結果を受け取る形です)。
			worktrees = append(worktrees, name)
		}
	}
	return worktrees, nil
}

// NewTaskResult は NewTask の結果です。Dir は task のディレクトリ、Created は「今回新しく作ったか」を表します(false なら既存だった)。
type NewTaskResult struct {
	Dir     string
	Created bool
}

// NewTask は task のディレクトリを作ります。既に存在する場合は何もせず成功を返し、Created を false にします(冪等)。
// task 名が規則に合わないときはエラーを返します。
func (a *App) NewTask(task string) (NewTaskResult, error) {
	if err := layout.ValidateTaskName(task); err != nil {
		// エラー時は結果の型も返す必要があるため、空の NewTaskResult{}(全フィールドがゼロ値)を返しています。
		return NewTaskResult{}, err
	}
	dir := a.layout.TaskDir(task)
	if fsx.Exists(dir) {
		return NewTaskResult{Dir: dir}, nil
	}
	// MkdirAll は mkdir -p 相当です。0o755 は権限(8 進数)で、Go では先頭の 0o が 8 進数のしるしです。
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return NewTaskResult{}, err
	}
	// フィールド名つきで struct を作っています。指定しなかった Created は false(ゼロ値)です。
	return NewTaskResult{Dir: dir, Created: true}, nil
}

// repoNameFromURL は git の URL から repo 名を取り出します(例: git@github.com:owner/repo.git → repo)。
// 末尾のスラッシュを落とし、最後の "/" より後ろを取って、".git" を除きます。
func repoNameFromURL(url string) string {
	trimmed := strings.TrimRight(url, "/")
	name := trimmed[strings.LastIndex(trimmed, "/")+1:]
	return strings.TrimSuffix(name, ".git")
}

// Clone は bare repo を repos/<repo>.git に用意します(ws clone の本体)。repo が空なら url から決めます。
// 戻り値は bare repo のパスです。戻り値に名前(path, err)を付けているのは、下の defer の中から err を参照するためです。
// Clone prepares repos/<repo>.git. repo is derived from url when empty. It returns the bare repo path.
func (a *App) Clone(ctx context.Context, url, repo string) (path string, err error) {
	if repo == "" {
		repo = repoNameFromURL(url)
	}
	if repo == "" {
		return "", errors.New("URL から repo 名を導出できません")
	}
	if err := layout.ValidateRepoName(repo); err != nil {
		return "", err
	}
	bare := a.layout.BarePath(repo)
	if fsx.Exists(bare) {
		return "", fmt.Errorf("%s は既に存在します", bare)
	}
	if err := os.MkdirAll(a.layout.ReposRoot(), 0o755); err != nil {
		return "", err
	}
	// defer は「この関数を抜けるときに実行する処理」を予約します。TypeScript の try/finally の finally に近いです。
	// ここでは、途中で失敗(err が nil でない)したなら作りかけの bare repo を削除します。
	// 無名関数 func() { ... }() が、外側の変数 err を参照しています(クロージャ。TypeScript のアロー関数と同じく、外の変数をそのまま使える)。
	// 名前付き戻り値 err を見ているので、各 return で返したエラーがここに反映されます。
	defer func() {
		if err != nil {
			os.RemoveAll(bare)
		}
	}()
	// 下の内側の `if _, err := ...` は、新しい err を内側のスコープで作る(シャドーイング)形です。
	// `_` は戻り値を使わないという意味で、ここでは git の標準出力は不要なので捨てています。
	// ただし return でエラーを返すと、名前付き戻り値の err にその値が入るため、defer の判定は正しく働きます。
	// git clone --bare はリモートの全ブランチをローカルブランチ化し、add の「ローカルブランチ存在判定」を狂わせる
	if _, err := gitexec.Output(ctx, a.git, "", "init", "--quiet", "--bare", bare); err != nil {
		return "", err
	}
	if err := gitexec.ApplyRepoConfig(ctx, a.git, bare); err != nil {
		return "", err
	}
	// [][]string は「文字列スライスのスライス」(TypeScript の string[][])です。実行したい git コマンドの引数を並べ、順に実行しています。
	// { ... } の中の要素は、型を省略して書けます。
	for _, args := range [][]string{
		{"remote", "add", "origin", url},
		{"config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"},
		{"fetch", "--quiet", "origin"},
	} {
		// args... は、スライスを可変長引数として展開して渡す書き方です(TypeScript の ...args と同じ記号で、呼び出し側でも使う)。
		if _, err := gitexec.Output(ctx, a.git, bare, args...); err != nil {
			return "", err
		}
	}
	return bare, nil
}

// AddOptions は Add(worktree の作成)に渡す指定です。
// Branch はブランチ名、Base は新規ブランチの起点、Create は新規ブランチを作るか、ReadOnly は参照専用(readonly)worktree を作るか、を表します。
// readonly worktree は origin/<branch> を detached HEAD で checkout するもので、同じブランチを複数の task から参照できます。
type AddOptions struct {
	Task     string
	Repo     string
	Branch   string
	Base     string
	Create   bool
	ReadOnly bool
}

// errMissingBranch は「ローカルにもリモートにもブランチが無い」ときのエラーを作ります。
// エラーは errors.New か fmt.Errorf で作れます。ここでは文字列を + でつないで読みやすく分けています。
func errMissingBranch(branch string) error {
	return fmt.Errorf("ローカルに '%s' も 'origin/%s' もありません。"+
		"リモートにあるはずなら、別ターミナルで ws worktree pull --task <task> か git fetch が必要です。"+
		"新規ブランチなら --branch <branch> --create --base <start-point> を使ってください", branch, branch)
}

// Add は worktree を 1 つ作り、そのパスを返します。opts.Task は呼び出し前に ResolveTask 済みであることが前提です。
// ブランチの扱いは 4 通りです。readonly なら origin/<branch> を detached で、Create なら起点 Base から新規ブランチを、
// ローカルにあればそれを、無くて origin にあれば upstream 付きでローカルブランチを作ります。
// Add creates a worktree and returns its path. opts.Task must already be resolved.
func (a *App) Add(ctx context.Context, opts AddOptions) (string, error) {
	bare, err := a.requireBare(ctx, opts.Repo)
	if err != nil {
		return "", err
	}
	// layout.SlugOf はブランチ名をディレクトリ名に使える形(slug。例: feature/A → feature-a)に変換します。
	slug, err := layout.SlugOf(opts.Branch)
	if err != nil {
		return "", err
	}
	// 作業用 worktree は task 直下、readonly は task 配下の _readonly/ に作ります。ブランチによってここで置き場所を切り替えます。
	dir := a.layout.TaskDir(opts.Task)
	if opts.ReadOnly {
		dir = a.layout.ReadonlyDir(opts.Task)
	}
	worktree := layout.WorktreePath(dir, opts.Repo, slug)
	// add は「git worktree add にこの引数を足して実行する」ローカル関数(クロージャ)です。TypeScript のアロー関数を変数に入れるのと同じです。
	// `args ...string` は可変長引数で、`append([]string{...}, args...)` は先頭に固定の引数を足したスライスを作っています。
	// 外側の変数 ctx / bare を使えるのもクロージャの特徴です。
	add := func(args ...string) error {
		_, err := gitexec.Output(ctx, a.git, bare, append([]string{"worktree", "add"}, args...)...)
		return err
	}
	// origin 側 / ローカル側にそのブランチがあるかを先に調べ、どのケースで作るかの判断材料にします。
	// refs/heads/ がローカルブランチ、refs/remotes/origin/ がリモート追跡ブランチ(最後に fetch した時点の origin の状態)です。
	remoteExists, err := gitexec.RefExists(ctx, a.git, bare, "refs/remotes/origin/"+opts.Branch)
	if err != nil {
		return "", err
	}
	localExists, err := gitexec.RefExists(ctx, a.git, bare, "refs/heads/"+opts.Branch)
	if err != nil {
		return "", err
	}
	// `switch { case 条件: ... }` は、条件式を上から順に評価して最初に真になった case を実行します(if / else if の連鎖と同じ)。
	// Go の case は自動で break されるので、TypeScript のような fallthrough を気にする必要はありません。
	switch {
	// readonly の場合。origin のブランチが無ければ作れません。
	case opts.ReadOnly:
		if !remoteExists {
			return "", errMissingBranch(opts.Branch)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		// --detach は、ブランチを使わず特定のコミットを直接 checkout する(detached HEAD)指定です。
		// これにより、他の task が同じブランチを作業用に使っていても衝突しません。
		err = add("--detach", worktree, "origin/"+opts.Branch)
	// 新規ブランチの場合。ローカルに同名があれば失敗させます。
	case opts.Create:
		if localExists {
			return "", fmt.Errorf("ローカルブランチ '%s' は既に存在します", opts.Branch)
		}
		// 解決できない名前を渡すと git worktree add は -b を無視し、origin の同名ブランチをローカルに作ってしまう
		// `var resolvable bool` は、型だけ指定した宣言です。こう先に宣言するのは、下で `resolvable, err = ...` と、既にある err に代入する(`:=` で新しい err を作らない)ためです。
		var resolvable bool
		resolvable, err = gitexec.Succeeds(ctx, a.git, bare, "rev-parse", "--verify", "--quiet", opts.Base+"^{commit}")
		if err != nil {
			return "", err
		}
		if !resolvable {
			return "", fmt.Errorf("--base '%s' を解決できません(origin/main のように明示してください)", opts.Base)
		}
		// --no-track は upstream を設定しない指定、-b は新規ブランチ作成です。起点 (Base) は origin/main のように明示します。
		err = add("--no-track", "-b", opts.Branch, worktree, opts.Base)
	// ローカルブランチが既にある場合。そのブランチを worktree に checkout します。
	case localExists:
		err = add(worktree, opts.Branch)
	// ローカルには無いが origin にはある場合。origin のブランチを元に、upstream を紐づけたローカルブランチを作ります。
	case remoteExists:
		err = add("--track", "-b", opts.Branch, worktree, "origin/"+opts.Branch)
	// どれにも当てはまらない場合はエラーです。
	default:
		return "", errMissingBranch(opts.Branch)
	}
	// switch の各 case で err に結果を入れておき、まとめてここで確認しています。
	if err != nil {
		return "", err
	}
	return worktree, nil
}

// RenameOptions は Rename(ブランチ名変更)の指定です。Branch が現在のブランチ名、To が新しい名前です。
type RenameOptions struct {
	Task   string
	Repo   string
	Branch string
	To     string
}

// RenameResult は Rename の結果です。Moved は slug が変わって worktree のディレクトリを移動したか、HadUpstream は元ブランチに upstream があったか、を表します。
type RenameResult struct {
	Old         string
	New         string
	NewPath     string
	Moved       bool
	HadUpstream bool
	// UnsetUpstreamErr is set when the rename succeeded but the stale upstream setting could not be removed.
	// error 型のフィールドは nil のとき「エラー無し」です。ここでは、改名自体は成功しても付随処理が失敗したことを、呼び出し側に伝えるために使います。
	UnsetUpstreamErr error
}

// ValidateBranchName は、git がブランチ名として受け付ける名前かどうかを確認します。問題なければ nil(エラー無し)を返します。
// ValidateBranchName accepts only names that git check-ref-format --branch returns unchanged.
func (a *App) ValidateBranchName(ctx context.Context, name string) error {
	// --branch は @{-1} などの略記を展開するので、入力と一致しない名前は受け付けない
	// a.git.Run は git コマンドを実行し、終了コードや標準出力を持つ結果を返します。
	// err は「git を起動できなかった」などの実行自体の失敗で、git が非 0 で終了したこと(ExitCode)とは別に扱っています。
	result, err := a.git.Run(ctx, "", "check-ref-format", "--branch", name)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 || result.Stdout != name {
		return fmt.Errorf("ブランチ名 '%s' は git で使えません", name)
	}
	return nil
}

// cwdInside は、カレントディレクトリが directory の中(directory 自身を含む)にあるかを返します。
// worktree を移動する前に、今いる場所が移動対象の中でないかを確認するために使います。
func (a *App) cwdInside(directory string) (bool, error) {
	cwd, err := a.getwd()
	if err != nil {
		return false, err
	}
	cwd, err = fsx.RealPath(cwd)
	if err != nil {
		return false, err
	}
	root, err := fsx.RealPath(directory)
	if err != nil {
		return false, err
	}
	// `root+"/"` を付けて前方一致を見ることで、/a/foo と /a/foobar のように名前の一部が同じ別ディレクトリを誤って「中」と判定しないようにしています。
	return cwd == root || strings.HasPrefix(cwd, root+"/"), nil
}

// Rename は、作業用 worktree のブランチ名を変え、必要なら worktree のディレクトリも移動します。ネットワークは使いません。
// 事前確認(worktree が存在する、新しい名前が使える、移動先が空いている、など)を全部済ませてから、実際の変更(renameBranchAndMove)を行います。
// 先に検査し終えるのは、途中で失敗して中途半端な状態になるのを避けるためです。
// Rename renames the branch and moves the worktree. opts.Task must already be resolved.
// A failed rollback or a failed upstream cleanup after a successful rename is reported in the result or error.
func (a *App) Rename(ctx context.Context, opts RenameOptions) (RenameResult, error) {
	bare, err := a.requireBare(ctx, opts.Repo)
	if err != nil {
		return RenameResult{}, err
	}
	oldSlug, err := layout.SlugOf(opts.Branch)
	if err != nil {
		return RenameResult{}, err
	}
	newSlug, err := layout.SlugOf(opts.To)
	if err != nil {
		return RenameResult{}, err
	}
	taskDir := a.layout.TaskDir(opts.Task)
	oldWorktree := layout.WorktreePath(taskDir, opts.Repo, oldSlug)
	newWorktree := layout.WorktreePath(taskDir, opts.Repo, newSlug)
	// `current := ""` は空文字で宣言する書き方です。worktree が無い場合は空のまま残り、下の比較で「無い」と判定されます。
	current := ""
	if fsx.Exists(oldWorktree + "/.git") {
		// `if current, err = f(); err != nil {` は、if の初期化文で既存の変数に代入しています(`:=` ではなく `=` なので、外の current が更新されます)。
		if current, err = gitexec.CurrentBranch(ctx, a.git, oldWorktree); err != nil {
			return RenameResult{}, err
		}
	}
	// 期待したブランチを checkout していない、または worktree が無いなら、改名できないのでエラーにします。
	if current != opts.Branch {
		return RenameResult{}, fmt.Errorf("作業用 worktree %s が無いか、ブランチ '%s' を checkout していません", oldWorktree, opts.Branch)
	}
	if err := a.ValidateBranchName(ctx, opts.To); err != nil {
		return RenameResult{}, err
	}
	exists, err := gitexec.RefExists(ctx, a.git, bare, "refs/heads/"+opts.To)
	if err != nil {
		return RenameResult{}, err
	}
	if exists {
		return RenameResult{}, fmt.Errorf("ローカルブランチ '%s' は既に存在します", opts.To)
	}
	// slug が変わらなければ(例: Foo と foo の違いだけ)ディレクトリ名は同じなので移動は不要です。移動が必要なときだけ追加の確認をします。
	moved := oldSlug != newSlug
	if moved {
		if fsx.Exists(newWorktree) {
			return RenameResult{}, fmt.Errorf("移動先 %s は既に存在します", newWorktree)
		}
		// ここの `inside, err := ...` は if ブロック内の新しい変数です(外の err とは別物)。
		inside, err := a.cwdInside(oldWorktree)
		if err != nil {
			return RenameResult{}, err
		}
		if inside {
			return RenameResult{}, fmt.Errorf("カレントディレクトリが %s 配下のため、worktree を移動できません。別のディレクトリで実行してください", oldWorktree)
		}
	}
	// upstream とは、ブランチが追従するリモートのブランチのことです。設定が残っているかどうかで、後で外す必要があるかを決めます。
	// origin 側が prune 済みだと @{upstream} は解決できないが設定は残るので、設定そのものを見る
	hadUpstream, err := gitexec.Succeeds(ctx, a.git, oldWorktree, "config", "--get", "branch."+opts.Branch+".merge")
	if err != nil {
		return RenameResult{}, err
	}
	// finalPath は改名後の worktree のパスです。移動しないなら元のパスのままです。
	finalPath := oldWorktree
	if moved {
		finalPath = newWorktree
	}
	// `unsetErr, err := ...` は、2 つの戻り値を 2 つの変数で受けています。unsetErr は「改名には成功したが upstream の解除に失敗した」ことを表す、致命的でないエラーです。
	// 致命的なのは err のほうなので、こちらだけ return でエラーとして返します。
	unsetErr, err := a.renameBranchAndMove(ctx, bare, oldWorktree, finalPath, opts.Branch, opts.To, hadUpstream)
	if err != nil {
		return RenameResult{}, err
	}
	// RenameResult{...} に結果を詰めて返し、エラーは nil(無し)にしています。
	return RenameResult{Old: opts.Branch, New: opts.To, NewPath: finalPath, Moved: moved, HadUpstream: hadUpstream, UnsetUpstreamErr: unsetErr}, nil
}

// renameBranchAndMove は、実際の変更を行います。ブランチ名の変更 → (必要なら)worktree の移動 → (あれば)upstream の解除、の順です。
// 移動に失敗したらブランチ名を元に戻す(ロールバック)ため、「途中失敗でも半端な状態を残さない」工夫が入っています。
// 戻り値 (unsetUpstreamErr, err error) は、2 つとも名前付きの error 型です。1 つ目は致命的でない警告、2 つ目は失敗を表します。
// 引数のうち、old / new は同じ型の変数をまとめて `old, new string` と書いています。
func (a *App) renameBranchAndMove(ctx context.Context, bare, oldWorktree, newWorktree, old, new string, hadUpstream bool) (unsetUpstreamErr, err error) {
	// git branch -m は「ブランチ名の変更」です。ここで失敗したら、まだ何も変えていないので、そのままエラーを返します。
	if _, err := gitexec.Output(ctx, a.git, oldWorktree, "branch", "--quiet", "-m", old, new); err != nil {
		return nil, err
	}
	if oldWorktree != newWorktree {
		// worktree move は、git が管理している worktree の場所を、ディレクトリごと正しく移動します(単なる mv だと git の記録が壊れます)。
		if _, err := gitexec.Output(ctx, a.git, bare, "worktree", "move", oldWorktree, newWorktree); err != nil {
			// 移動に失敗したら、先ほど変えたブランチ名を元に戻します。
			if _, rollbackErr := gitexec.Output(ctx, a.git, oldWorktree, "branch", "--quiet", "-m", new, old); rollbackErr != nil {
				// 元に戻すのにも失敗した場合は、手動で直すコマンドを案内します。
				manual := fmt.Errorf("ブランチ名を元に戻せませんでした。手動で戻してください: git -C %s branch -m %s %s", oldWorktree, new, old)
				// errors.Join は複数のエラーを 1 つにまとめます。元の失敗・案内文・ロールバックの失敗が、すべてメッセージに出ます。
				return nil, errors.Join(err, manual, rollbackErr)
			}
			return nil, err
		}
	}
	// upstream が付いていた場合は外します。ここは失敗しても改名自体は成功済みなので、エラーは return せず、警告用の戻り値 unsetUpstreamErr に入れるだけにしています。
	if hadUpstream {
		_, unsetUpstreamErr = gitexec.Output(ctx, a.git, newWorktree, "branch", "--quiet", "--unset-upstream", new)
	}
	// 名前付き戻り値(unsetUpstreamErr)に入れた値を、そのまま返しています。
	return unsetUpstreamErr, nil
}

// WorktreeEntry は task 配下の worktree 1 つ分の情報です。Kind は作業用か readonly か、Name はディレクトリ名、Head は現在のブランチ(または detached)、
// Dirty は未コミットの変更があるか、Updated は最後に作業した推定時刻です。
type WorktreeEntry struct {
	Kind    string
	Name    string
	Head    string
	Dirty   bool
	Updated time.Time
}

// const は定数です。Go に enum は無いので、文字列の定数をこのように並べて「種類」を表すのがよくあります(TypeScript のユニオン型 "work" | "ro" に近い)。
const (
	KindWork     = "work"
	KindReadonly = "ro"
)

// entriesIn は dir 直下の worktree を WorktreeEntry のスライスにして返します。kind は作業用(KindWork)か readonly(KindReadonly)かの区別です。
func entriesIn(dir, kind string) ([]WorktreeEntry, error) {
	names, err := listWorktreeNames(dir)
	if err != nil {
		return nil, err
	}
	// make([]T, 長さ, 容量) でスライスを作ります。ここでは長さ 0・容量 len(names) で作り、あとで append しても再確保が起きないようにしています(性能のための工夫で、挙動は同じです)。
	entries := make([]WorktreeEntry, 0, len(names))
	for _, name := range names {
		// worktreeActivity は activity.go にある関数で、最後に作業した時刻を推定します。
		entries = append(entries, WorktreeEntry{Kind: kind, Name: name, Updated: worktreeActivity(filepath.Join(dir, name))})
	}
	return entries, nil
}

// worktreeEntries は、task 内の worktree を「作業用 → readonly」の順(それぞれ名前順)で返します。git は実行せず、ファイルシステムだけを見るので高速です。
// worktreeEntries lists the work worktrees followed by the readonly ones, each in name order, without querying git.
func (a *App) worktreeEntries(task string) ([]WorktreeEntry, error) {
	work, err := entriesIn(a.layout.TaskDir(task), KindWork)
	if err != nil {
		return nil, err
	}
	readonly, err := entriesIn(a.layout.ReadonlyDir(task), KindReadonly)
	if err != nil {
		return nil, err
	}
	// append(work, readonly...) は 2 つのスライスをつなげます(JavaScript の [...work, ...readonly] に相当)。
	return append(work, readonly...), nil
}

// worktree のフルパスを返すメソッドです。(e WorktreeEntry) は、ポインタではなく値のレシーバで、e のコピーを受け取ります(読むだけなのでコピーで十分)。
// readonly は _readonly/ 配下、作業用は task 直下にあるため、種類で切り替えます。
func (e WorktreeEntry) path(l layout.Layout, task string) string {
	if e.Kind == KindReadonly {
		return l.ReadonlyDir(task) + "/" + e.Name
	}
	return l.TaskDir(task) + "/" + e.Name
}

// ListWorktrees は task 内の worktree 一覧を、現在のブランチ名と未コミット変更の有無つきで返します。
// 作業用と readonly はそれぞれ「最近作業した順」に並べ、作業用が先、readonly が後です。
// ListWorktrees returns the work worktrees followed by the readonly ones, each most recently active first.
func (a *App) ListWorktrees(ctx context.Context, task string) ([]WorktreeEntry, error) {
	entries, err := a.worktreeEntries(task)
	if err != nil {
		return nil, err
	}
	// `for i, entry := range entries` の entry は要素のコピーなので、entry を書き換えても元のスライスには反映されません。
	// そのため、書き換えるときは添え字 i を使って entries[i] に代入しています。
	for i, entry := range entries {
		path := entry.path(a.layout, task)
		branch, err := gitexec.CurrentBranch(ctx, a.git, path)
		if err != nil {
			return nil, err
		}
		// ブランチ名が空なのは detached HEAD(特定のコミットを直接 checkout した状態。readonly worktree など)のときです。その場合はコミットの短縮ハッシュを "detached@..." として表示します。
		if branch == "" {
			short, err := gitexec.ShortHead(ctx, a.git, path)
			if err != nil {
				return nil, err
			}
			branch = "detached@" + short
		}
		entries[i].Head = branch
		// `if entries[i].Dirty, err = ...; err != nil` は、struct のフィールドへ代入しつつエラーを確認しています。
		if entries[i].Dirty, err = gitexec.IsDirty(ctx, a.git, path); err != nil {
			return nil, err
		}
	}
	// slices.IndexFunc は「条件を満たす最初の要素の添え字」を返し、無ければ -1 を返します。
	// ここでは readonly が始まる位置を探し、そこを境に作業用と readonly を別々に並べ替えます。
	// func(e WorktreeEntry) bool { ... } は無名関数(TypeScript のアロー関数 (e) => ... に近い)です。
	readonlyFrom := slices.IndexFunc(entries, func(e WorktreeEntry) bool { return e.Kind == KindReadonly })
	// readonly が 1 つも無いときは、全部が作業用という意味で長さを境にします。
	if readonlyFrom < 0 {
		readonlyFrom = len(entries)
	}
	// 時刻を取り出す関数を変数に入れておき、下の 2 回の並べ替えで使い回しています。
	updated := func(e WorktreeEntry) time.Time { return e.Updated }
	// entries[:n] はスライスの先頭から n 個の「部分ビュー」です(コピーではなく同じ元データを指す)。そのため、並べ替えは元の entries に反映されます。TypeScript の slice と違い、コピーしない点に注意してください。
	newestFirst(entries[:readonlyFrom], updated)
	newestFirst(entries[readonlyFrom:], updated)
	return entries, nil
}

// TaskSummary は ws ls で表示する task 1 つ分の要約です。Work / Readonly は作業用・readonly の worktree 数、Dirty は未コミット変更のある worktree 数です。
type TaskSummary struct {
	Name        string
	Work        int
	Readonly    int
	Dirty       int
	Updated     time.Time
	Description string
}

// ListTasks はすべての task の要約を、最近作業した順に返します。tasks ディレクトリが無ければ空(nil)を返します。
// ListTasks summarizes every task, most recently active first.
func (a *App) ListTasks(ctx context.Context) ([]TaskSummary, error) {
	if !fsx.Exists(a.layout.TasksRoot()) {
		return nil, nil
	}
	names, err := fsx.SubdirNames(a.layout.TasksRoot())
	if err != nil {
		return nil, err
	}
	// make でスライスを作る(長さ 0、容量 len(names))と、あとで append するときに領域の再確保が減ります。
	summaries := make([]TaskSummary, 0, len(names))
	for _, name := range names {
		entries, err := a.worktreeEntries(name)
		if err != nil {
			return nil, err
		}
		// 要約を作りつつ、descriptionは別途読み込みます。読めなかったときは、一覧全体を失敗させず、その旨のメッセージを description 欄に入れるだけにしています。
		summary := TaskSummary{Name: name, Updated: a.taskActivity(name, entries)}
		if summary.Description, err = a.Description(name); err != nil {
			summary.Description = fmt.Sprintf("(%s を読めません)", taskMetaFile)
		}
		// worktree の種類ごとに数えます。`summary.Readonly++` は `+= 1` と同じ、インクリメントです。
		for _, entry := range entries {
			if entry.Kind == KindReadonly {
				summary.Readonly++
			} else {
				summary.Work++
			}
			// 各 worktree を git で調べ、未コミットの変更があるものを数えます。
			dirty, err := gitexec.IsDirty(ctx, a.git, entry.path(a.layout, name))
			if err != nil {
				return nil, err
			}
			if dirty {
				summary.Dirty++
			}
		}
		summaries = append(summaries, summary)
	}
	// ジェネリクスを使った newestFirst(activity.go)で、更新時刻の新しい順に並べ替えます。func(s TaskSummary) time.Time { ... } で「要素から並べ替えキーを取り出す関数」を渡しています。
	newestFirst(summaries, func(s TaskSummary) time.Time { return s.Updated })
	return summaries, nil
}
