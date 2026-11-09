// Package gitexec は、外部コマンドの git を実行する層。
// git の呼び出しを Runner という interface の裏に隠し、app パッケージは Runner だけを知っている(app → gitexec)。
// 本番では実際に git を起動する実装(New が返すもの)を使い、テストでは偽の Runner に差し替えられる。
// そのほかに、よく使う git の問い合わせ(ブランチ名の取得、未コミットの変更の有無など)を関数としてまとめている。
package gitexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Result は git を 1 回実行した結果。
// ExitCode は終了コード(0 が成功)、Stdout は標準出力(前後の空白を除いたもの)、Stderr は標準エラー出力(そのまま)。
type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// Runner は「git を実行する」ことだけを表す interface。
//
// Go の interface は、メソッドの一覧だけを定義した型。TypeScript の interface とよく似ているが、
// 「implements Runner」と明示しなくても、同じメソッドを持つ型は自動的に Runner として扱われる(構造的部分型)。
// これを使って、本番では execRunner(実際に git を起動)、テストでは偽物を渡す、という差し替えができる。
type Runner interface {
	// Run executes git in dir (or the current directory when dir is empty).
	// A non-zero exit is reported in Result; err is set only when git could not run.
	//
	// args は git に渡す引数(例: "status", "--porcelain")。「args ...string」は可変長引数で、
	// Run(ctx, dir, "status", "--porcelain") のように好きな個数を並べて渡せる(TypeScript の ...args: string[])。
	// 「git が非 0 で終了した」ことはエラー扱いにせず Result.ExitCode で返し、「git を起動できなかった」ときだけ err を返す。
	// これにより、呼び出し側は「ブランチが無い」のような想定内の失敗と、「git が入っていない」のような異常を区別できる。
	Run(ctx context.Context, dir string, args ...string) (Result, error)
}

// ExitError は git が非 0 で終了したことを表すエラー。Stderr に git のエラーメッセージを持つ。
//
// Go ではエラーも普通の型で、Error() string というメソッドを持てば error interface を満たす。
// 独自の型にすることで、呼び出し側が「git の失敗だった」と判別し、中身(Stderr)を取り出せる。
type ExitError struct {
	Stderr string
}

// Error は error interface を満たすためのメソッド。エラーメッセージとして git の標準エラー出力をそのまま返す。
// レシーバが *ExitError(ポインタ)なので、&ExitError{...} のポインタで使う。
func (e *ExitError) Error() string { return e.Stderr }

// execRunner は実際に git を起動する Runner の実装。状態を持たないので、フィールドは無い。
// 先頭が小文字なのでこのパッケージの外からは見えず、外からは New() 経由で Runner として受け取る。
type execRunner struct{}

// New は本番用の Runner(実際に git コマンドを起動するもの)を返す。
// 戻り値の型を実装の型(execRunner)ではなく interface(Runner)にしているので、呼び出し側は実装を知らなくてよい。
func New() Runner { return execRunner{} }

// Run は Runner interface の実装。git を dir で実行し、その結果を返す(契約は Runner.Run を参照)。
//
// メソッドのレシーバ (execRunner) に名前が無いのは、中身を使わないため。
// ctx がキャンセルされると git のプロセスも終了させられる。
func (execRunner) Run(ctx context.Context, dir string, args ...string) (Result, error) {
	if dir != "" {
		// git -C <dir> ... で「そのディレクトリで実行したことにする」。プロセス全体の cwd を変えないので、並行して使っても安全。
		// append([]string{"-C", dir}, args...) は配列を前に連結する。args... は「スライスを可変長引数として展開する」書き方で、
		// TypeScript の [ "-C", dir, ...args ] に相当する。
		args = append([]string{"-C", dir}, args...)
	}
	// exec.CommandContext は外部コマンドを表すオブジェクトを作る(この時点ではまだ実行されない)。
	// ctx を渡すと、ctx のキャンセルや期限切れで、実行中のコマンドを強制終了できる。Node.js の spawn に AbortSignal を渡すのと同じ。
	cmd := exec.CommandContext(ctx, "git", args...)
	// bytes.Buffer は書き込まれたバイト列をためておくバッファ。標準出力と標準エラー出力の受け皿にする。
	// 「var a, b T」は 2 つの変数を同じ型で宣言する。ゼロ値のまま使えるので、初期化は不要。
	var stdout, stderr bytes.Buffer
	// cmd.Stdout に Writer を設定すると、プロセスの出力がそこへ流れ込む。&stdout はバッファのポインタ(書き込み先を共有するため)。
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Run はコマンドを起動し、終了まで待つ。終了コードが 0 以外だと *exec.ExitError、起動できなかったときは別のエラーが返る。
	err := cmd.Run()
	// Stdout は前後の空白を除く(git の出力は末尾に改行が付くため)。Stderr はメッセージをそのまま残す。
	result := Result{Stdout: strings.TrimSpace(stdout.String()), Stderr: stderr.String()}
	// errors.AsType[*exec.ExitError](err) は「err が *exec.ExitError か」を調べて、そうならその値を取り出す(ジェネリクス)。
	// [*exec.ExitError] の角括弧は型引数で、TypeScript の関数呼び出し asType<ExitError>(err) と同じ見た目。
	// err が nil のときや別のエラーのときは ok が false になる。
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		// git は動いたが、非 0 で終了した場合。Result に終了コードを入れて、エラーにはしない。
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	// ExitError 以外のエラーは、git の実行ファイルが見つからないなど、起動自体の失敗。
	// %w でエラーを包むと、元のエラーを errors.Is / errors.As で取り出せる(TypeScript の new Error(msg, { cause }) に相当)。
	if err != nil {
		return Result{}, fmt.Errorf("git を実行できません: %w", err)
	}
	return result, nil
}

// Output returns stdout, or an *ExitError carrying git's stderr on a non-zero exit.
//
// 「git を実行して標準出力を文字列で欲しい」場合の簡易版。r は Runner(本番でもテストでも同じ関数が使える)。
// git が失敗したら *ExitError を返すので、呼び出し側は if err != nil { return err } だけで済む。
// Run と違って、非 0 終了を呼び出し側が区別する必要が無いときに使う。
func Output(ctx context.Context, r Runner, dir string, args ...string) (string, error) {
	// args... で、受け取った可変長引数をそのまま Run に渡し直している。
	result, err := r.Run(ctx, dir, args...)
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", &ExitError{Stderr: result.Stderr}
	}
	return result.Stdout, nil
}

// Succeeds は git のコマンドが成功(終了コード 0)したかどうかを返す。
// 「ブランチがあるか」のように、成功か失敗かが答えになる問い合わせに使う。
// 非 0 終了は (false, nil) で、git を起動できなかったときは (false, err) になる。
func Succeeds(ctx context.Context, r Runner, dir string, args ...string) (bool, error) {
	result, err := r.Run(ctx, dir, args...)
	return err == nil && result.ExitCode == 0, err
}

// RefExists は bare リポジトリに ref(例: "refs/heads/main")があるかを返す。
// git show-ref --verify は完全な ref 名での存在確認で、--quiet は何も出力しない。ブランチ名が曖昧に解釈されることはない。
func RefExists(ctx context.Context, r Runner, bare, ref string) (bool, error) {
	return Succeeds(ctx, r, bare, "show-ref", "--verify", "--quiet", ref)
}

// IsDirty は worktree に未コミットの変更(未追跡ファイルを含む)があるかを返す。
func IsDirty(ctx context.Context, r Runner, worktree string) (bool, error) {
	// status は index を書き戻すことがあり、ws ls を実行しただけで更新順が変わってしまう
	// (--no-optional-locks は、その書き戻しをしないようにするオプション。「ws ls」の並び順は index の更新日時で決まる)。
	// --porcelain は人間向けでなく機械向けの安定した出力形式で、変更が無いと空文字になる。
	out, err := Output(ctx, r, worktree, "--no-optional-locks", "status", "--porcelain")
	return out != "", err
}

// CurrentBranch returns "" for a detached HEAD.
//
// worktree が今チェックアウトしているブランチ名を返す。detached HEAD(ブランチに乗っていない状態。readonly worktree が該当する)では
// symbolic-ref が失敗するので、エラーではなく空文字を返す。
func CurrentBranch(ctx context.Context, r Runner, worktree string) (string, error) {
	result, err := r.Run(ctx, worktree, "symbolic-ref", "-q", "--short", "HEAD")
	// git を起動できなかった(err)か、detached HEAD(非 0 終了)なら "" を返す。err が nil なら、それがそのまま nil として返る。
	if err != nil || result.ExitCode != 0 {
		return "", err
	}
	return result.Stdout, nil
}

// ShortHead は worktree の HEAD のコミットを短縮ハッシュ(7 桁程度)で返す。detached HEAD の表示などに使う。
func ShortHead(ctx context.Context, r Runner, worktree string) (string, error) {
	return Output(ctx, r, worktree, "rev-parse", "--short", "HEAD")
}

// ApplyRepoConfig は bare リポジトリに、ws で使う git の設定を書き込む。ws clone と worktree の作成時に呼ばれる。
func ApplyRepoConfig(ctx context.Context, r Runner, bare string) error {
	// 手で切ったブランチの upstream が base(origin/main 等)になると、エディタの push/sync で base に push してしまう
	// (branch.autoSetupMerge=simple にすると、同名のリモートブランチがあるときだけ upstream が自動で設定される)。
	// Output の戻り値(標準出力)は不要なので _ で捨て、エラーだけを返す。
	_, err := Output(ctx, r, bare, "config", "branch.autoSetupMerge", "simple")
	return err
}
