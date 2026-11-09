// このファイルは ws コマンド全体の入り口(Run 関数)と、その土台になる型を定義する。
// 標準入出力や環境変数といった「プロセスから受け取るもの」を Env にまとめ、テストで差し替えられるようにしている。
// あわせて、エラーの種類に応じて終了コード(0 / 1 / 2 / 130)を決める処理(exitCode)もここにある。
// 個々のサブコマンドの定義は commands.go、表示の整形は output.go、シェル補完は completion.go にある。
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/ayame113/my-workspace-manager/internal/app"
	"github.com/ayame113/my-workspace-manager/internal/gitexec"
	"github.com/ayame113/my-workspace-manager/internal/layout"
	"github.com/ayame113/my-workspace-manager/internal/prompt"
)

// 終了コードの定義。ws はシェルから呼ばれるので、成功か失敗かをプロセスの終了コードで伝える。
// const (...) は定数をまとめて宣言する書き方。TypeScript の enum や `as const` のオブジェクトに近い。
// 名前が大文字で始まるものは他のパッケージから見える(公開)。Go には export キーワードが無く、
// 大文字始まりなら公開、小文字始まりならパッケージ内だけで使える、という命名規則で決まる。
//   - 0: 成功
//   - 1: 実行時の失敗(lint で問題が見つかった場合を含む)
//   - 2: 使い方の誤り(引数が足りない、不明なサブコマンドなど)
//   - 130: 対話入力の途中で Ctrl-C などにより中断された(シェルの慣習で 128 + SIGINT(2))
const (
	ExitOK        = 0
	ExitFailure   = 1
	ExitUsage     = 2
	ExitInterrupt = 130
)

// Env gathers everything Run takes from the process so that tests can replace it.
//
// Env は、Run がプロセスから受け取るもの(標準入出力、環境変数、カレントディレクトリ、git の実行など)を 1 か所に集めた構造体。
// 本番では OSEnv() が本物を詰め、テストでは偽物(バッファや疑似 git)を詰める。
// こうして外部の物を引数で渡して差し替えられるようにする設計を「依存注入(DI)」と呼ぶ。
// グローバル変数や os.Stdout を直接触らないのはこのため。
//
// struct は TypeScript の interface / type で書くオブジェクト型に近いが、フィールドは宣言順に並ぶ固定の型で、値のコピーが基本。
// 各フィールドは「名前 型」の順に書く(TypeScript の `name: type` と逆順)。
//
// フィールドの意味:
//   - Stdin / Stdout / Stderr: io.Reader / io.Writer は「読み出せるもの」「書き込めるもの」を表す標準の interface。
//     os.Stdin / os.Stdout だけでなく、テストでは bytes.Buffer なども渡せる。
//   - Getenv: func(string) string は関数型。os.Getenv と同じ形の関数を渡す(process.env[key] を関数にしたもの)。
//   - Getwd: 戻り値が 2 つ(値と error)の関数型。Go の関数は複数の値を返せる。
//   - StdinIsTerminal / StderrIsTerminal: stdin / stderr が端末(TTY)につながっているか。
//     対話するかどうかの判定に使う(interactive メソッド参照)。
//   - Git: git コマンドを実行するもの。これも interface で、テストでは疑似実装に差し替える。
type Env struct {
	Stdin            io.Reader
	Stdout           io.Writer
	Stderr           io.Writer
	Getenv           func(string) string
	Getwd            func() (string, error)
	StdinIsTerminal  bool
	StderrIsTerminal bool
	Git              gitexec.Runner
	// Prompter が nil なら、対話が必要なときに huh で実装したものを使う
	//
	// nil は他言語の null / undefined に近い。interface や関数型、ポインタのゼロ値(何も設定しなかったときの値)が nil になる。
	Prompter Prompter
	// OpenEditor が nil なら zed で開く
	OpenEditor func(ctx context.Context, dir string) error
}

// OSEnv returns an Env wired to the real process: actual stdin/stdout/stderr, environment, and git.
//
// OSEnv は本番用の Env を作って返す。cmd/ws の main から Run に渡す想定。
// term.IsTerminal は、ファイルディスクリプタ(Fd)が端末につながっているかを調べる。
// パイプやリダイレクト、エージェントからの実行では false になる。
func OSEnv() Env {
	// Env{...} は構造体リテラル。フィールド名: 値 の形で初期化する(TypeScript のオブジェクトリテラルに近い)。
	// 書かなかったフィールド(Prompter, OpenEditor)はゼロ値(nil)になる。
	return Env{
		Stdin:            os.Stdin,
		Stdout:           os.Stdout,
		Stderr:           os.Stderr,
		Getenv:           os.Getenv,
		Getwd:            os.Getwd,
		StdinIsTerminal:  term.IsTerminal(int(os.Stdin.Fd())),
		StderrIsTerminal: term.IsTerminal(int(os.Stderr.Fd())),
		Git:              gitexec.New(),
	}
}

// Prompter is the set of interactive questions ws can ask.
//
// Prompter は、ws が対話(huh による選択や入力)で尋ねる質問の一覧を interface として定義したもの。
// Go の interface は「このメソッドを全部持つ型なら何でもこの interface を満たす」という構造的な型付けで、
// implements のような宣言は要らない(TypeScript の interface に近い)。
// 本番では internal/prompt の実装が使われ、テストでは決まった答えを返す偽物に差し替えられる。
// どのメソッドも ctx を受け取り、中断されたときは error を返す。
type Prompter interface {
	// 引数なしの worktree 系コマンドで、task を一覧から 1 つ選ばせる。
	ChooseTask(ctx context.Context) (string, error)
	// ws new を引数なしで実行したときの一連の質問(repo、task 名、description など)。
	New(ctx context.Context) (prompt.NewTaskAnswers, error)
	// ws worktree add で足りない引数を尋ねる。partial は指定済みの値で、それを保ったまま不足分だけを埋めて返す。
	Add(ctx context.Context, partial app.AddOptions) (app.AddOptions, error)
	// ws worktree rename で足りない引数を尋ねる。
	Rename(ctx context.Context, partial app.RenameOptions) (app.RenameOptions, error)
	// ws skill install のインストール先(claude / codex)を複数選ばせる。[]string は文字列のスライス(配列)。
	ChooseSkillTargets(ctx context.Context) ([]string, error)
	// ws describe の編集画面。current は現在の description で、編集後の文字列を返す。
	Describe(ctx context.Context, task, current string) (string, error)
	// ws ls で、絞り込みながら task を 1 つ選ばせる。選んだ Choice の Value を返す。
	PickTask(ctx context.Context, choices []prompt.Choice) (string, error)
}

// usageError marks an error as a usage mistake (exit code 2).
//
// usageError は「使い方の誤り」を表すエラー型。exitCode がこの型かどうかを見て、終了コード 2 にする。
//
// `struct{ error }` は「構造体の埋め込み」。フィールド名を付けずに型だけを書くと、
// その型(ここでは error interface)のメソッドが usageError に引き継がれる。
// つまり usageError は中に error を 1 つ持ち、Error() メソッドをそのまま使える。
// TypeScript で言えば、class UsageError extends Error のように「普通のエラーに印を付けた別の型」を作るのに近い。
type usageError struct{ error }

// usagef builds a usage error from a printf-style format.
//
// usagef は fmt.Sprintf のような書式付きで usageError を作る。使い方は `return usagef("task 名が必要です")`。
// `args ...any` は可変長引数(TypeScript の `...args: unknown[]`)で、any は何でも入る型(unknown / any に近い)。
// fmt.Errorf は書式付きで error 値を作る標準関数。
func usagef(format string, args ...any) error {
	// args... は、スライスを可変長引数として展開して渡す書き方(スプレッド構文 ...args に相当)。
	return usageError{fmt.Errorf(format, args...)}
}

// exitError ends the process with code without printing anything further.
//
// exitError は「すでに必要な表示は済ませたので、あとは終了コードだけ指定して終わりたい」ときに返すエラー。
// exitCode が見つけると、追加のメッセージを出さずに code をそのまま終了コードにする。
type exitError struct{ code int }

// Error は error interface を満たすためのメソッド。error は Error() string というメソッド 1 つだけを持つ interface。
// `(e exitError)` はレシーバと呼び、このメソッドが exitError 型に属することを表す(クラスのメソッドに近い)。
// ここは値レシーバで、e のコピーを受け取る。
func (e exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

// runner holds what every subcommand needs: the environment, the app logic, and whether execution has started.
//
// runner は、各サブコマンドが共通で使うものをまとめた構造体。サブコマンドの定義(commands.go)はすべて runner のメソッドになっている。
// 小文字始まりなのでこのパッケージの外からは使えない。
//
// フィールドの意味:
//   - app: internal/app のロジック本体。ポインタ(*app.App)で持つので、コピーではなく同じ実体を共有する。
//     フィールド名 app とパッケージ名 app が同じだが、r.app はフィールド、app.New はパッケージの関数として区別される。
//   - started: cobra が引数の解析を終えて、コマンドの実行を始めたかどうか。
//     false のまま失敗したなら、cobra が引数やフラグを拒否した(= 使い方の誤り)と判断できる(exitCode と beforeRun 参照)。
type runner struct {
	env     Env
	app     *app.App
	started bool
}

// version returns the build version for `ws --version`.
//
// version は ws --version で表示する文字列を返す。go install でビルドされたバイナリには、
// モジュールのバージョンが埋め込まれていて、debug.ReadBuildInfo で読める。取れなければ "(devel)"(開発中)にする。
func version() string {
	// if 文は条件の前に代入文を置ける(`if 代入; 条件 {`)のが Go の特徴。`info, ok := ...; 条件` の形で、ok が true のときだけ中に入る。
	// := は「変数を宣言して代入」を一度に行う書き方(TypeScript の const に近い。ただし後から再代入もできる)。
	// ok は「取得に成功したか」を表す bool で、複数の戻り値の 2 つ目で返ってくる慣習。
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "(devel)"
}

// interactive reports whether ws may ask the user questions.
//
// interactive は、対話(質問)してよいかどうかを返す。stdin と stderr の両方が端末のときだけ true。
// 理由: 質問の画面は stderr に描き、入力は stdin から受け取る。どちらかがパイプやリダイレクトなら、
// 人間が操作していない(エージェントやスクリプトからの実行)と見なして、質問せずに引数不足を使い方の誤りにする。
// stdout は判定に含めない。`dir=$(ws ls)` のように stdout だけをシェルが受け取る使い方でも、対話画面を出せるようにするため。
func (r *runner) interactive() bool {
	return r.env.StdinIsTerminal && r.env.StderrIsTerminal
}

// prompter returns the Prompter to use for questions.
//
// prompter は対話に使う Prompter を返す。テストが env.Prompter を差し替えていればそれを、
// 無ければ本物(huh を使う prompt.New の実装)を作って返す。
func (r *runner) prompter() Prompter {
	if r.env.Prompter != nil {
		return r.env.Prompter
	}
	return prompt.New(r.app, r.env.Stdin, r.env.Stderr)
}

// Run executes the ws command line (without the program name) and returns the exit code.
// 0: success, 1: runtime failure (including unresolved lint issues), 2: usage error, 130: interrupted.
//
// Run は ws の本体。main から呼ばれ、戻り値の終了コードで os.Exit する想定。
// args はプログラム名を除いたコマンドライン引数(例: []string{"worktree", "add", "--repo", "x"})。
// ctx は context.Context で、キャンセルやタイムアウトを実行中の処理に伝えるための入れ物。
// Go では時間のかかる処理(git の実行など)の第 1 引数に ctx を渡す慣習がある。
// TypeScript の AbortSignal に近い。
func Run(ctx context.Context, args []string, env Env) int {
	// env は値渡しで Run に入ってくるコピーなので、ここで書き換えても呼び出し元には影響しない。
	if env.Git == nil {
		env.Git = gitexec.New()
	}
	// &runner{...} は、構造体を作ってそのポインタを得る書き方。ポインタにすることで、
	// 後続のメソッドが r.started などを書き換えても、同じ実体に反映される。
	r := &runner{
		env: env,
		app: app.New(app.Config{
			Layout: layout.FromEnv(env.Getenv),
			Git:    env.Git,
			Getenv: env.Getenv,
			Getwd:  env.Getwd,
		}),
	}
	// cobra のルートコマンドを組み立て、引数と出力先を設定して実行する。
	// cobra は Go で定番のコマンドライン解析ライブラリ。サブコマンドの木構造を作り、引数やフラグを解析して、
	// 該当するコマンドの RunE を呼ぶ。Node.js で言えば commander や yargs に相当する。
	root := r.rootCommand()
	root.SetArgs(args)
	root.SetOut(env.Stdout)
	root.SetErr(env.Stderr)
	// ExecuteContext は引数を解析して該当コマンドを実行する。コマンドの RunE が返したエラーがそのまま戻ってくる。
	// Go には try/catch が無く、エラーは戻り値で受け渡す。ここで err を受け取り、終了コードへの変換は exitCode に任せる。
	err := root.ExecuteContext(ctx)
	return r.exitCode(root, args, err)
}

// exitCode converts the error returned by the command into an exit code, printing the message for it.
//
// exitCode は、コマンドの実行結果(err)を終了コードに変換し、必要ならエラーメッセージを stderr に出す。
// 上から順に判定して、最初に当てはまったものを採用する。
//  1. exitError: 表示済みなので、指定された code をそのまま返す
//  2. err が nil: 成功なので 0
//  3. usageError、または実行開始前の失敗(cobra の引数解析エラー): メッセージとヘルプの案内を出して 2
//  4. 対話の中断(prompt.ErrAborted): 130
//  5. それ以外: 1(git が失敗したときは git の stderr をそのまま見せる)
func (r *runner) exitCode(root *cobra.Command, args []string, err error) int {
	stderr := r.env.Stderr
	// errors.AsType[T](err) は、err(包まれた中身も含めて)に型 T のエラーがあるかを調べる。
	// 見つかれば (その値, true)、無ければ (ゼロ値, false) を返す。
	// TypeScript の `err instanceof ExitError` に近いが、Go のエラーは fmt.Errorf("%w") などで
	// 何重にも包まれる(ラップ)ことがあるため、包みの中まで辿って探してくれる。
	// [exitError] はジェネリクスの型引数で、TypeScript の AsType<ExitError>(err) と同じ見た目。
	if exit, ok := errors.AsType[exitError](err); ok {
		return exit.code
	}
	// err == nil はエラーが無い(成功)ことを表す。Go のエラー処理は「戻り値の error が nil かどうか」で行う。
	if err == nil {
		return ExitOK
	}
	// `_, ok := ...` の _ は「使わない値を捨てる」ための名前。型だけ確かめたいので値は捨てている。
	// r.started が false のまま失敗した場合は、cobra が引数やフラグを拒否した場合(不明なフラグなど)。
	// それも使い方の誤りとして扱う。
	if _, ok := errors.AsType[usageError](err); ok || !r.started {
		fmt.Fprintf(stderr, "error: %v\n", err)
		// root.Find は、args からどのサブコマンドを指しているかを探す。
		// 「ws worktree add --help を参照してください」のように、該当コマンドの help を案内するために使う。
		// 戻り値 3 つのうち使うのは最初だけなので、残りは _ で捨てる。
		target, _, _ := root.Find(args)
		// fmt.Fprintf(出力先, 書式, 値...) は、指定した出力先(ここでは stderr)に書式付きで書き出す。
		// %s は文字列、%v は値を標準の形式で、%d は整数。\n は改行。
		fmt.Fprintf(stderr, "詳しくは '%s --help' を参照してください\n", target.CommandPath())
		return ExitUsage
	}
	// errors.Is(err, X) は、err が X と同じエラー(包みの中も含む)かを調べる。
	// prompt.ErrAborted は、huh の画面で Ctrl-C されたときのエラー。
	// このときだけ終了コードを 130 にして、「ユーザーが自分で止めた」ことをシェルに伝える。
	if errors.Is(err, prompt.ErrAborted) {
		fmt.Fprintln(stderr, "ws: 中断しました")
		return ExitInterrupt
	}
	// *gitexec.ExitError は、git コマンドが 0 以外で終了したことを表すエラー型(* はポインタ)。
	// git 自身のエラーメッセージ(Stderr)をそのまま出す方が、原因が分かりやすい。
	if gitErr, ok := errors.AsType[*gitexec.ExitError](err); ok {
		fmt.Fprint(stderr, gitErr.Stderr)
	} else {
		fmt.Fprintf(stderr, "ws: %v\n", err)
	}
	return ExitFailure
}
