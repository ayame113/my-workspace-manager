// ws コマンドのエントリポイント。ここには処理を書かず、引数と環境を internal/cli に渡して終了コードで終わるだけにしている。
//
// パッケージ間の依存の向きは次のとおり(矢印の先は「使われる側」)。
//
//	cmd/ws → cli → prompt → app → layout / gitexec / fsx
//	          └──────────→ app
//
// cli は引数の解析と出力、prompt は対話入力(huh)、app は各コマンドの処理本体、
// layout はパスと命名規則、gitexec は git の実行、fsx は macOS 固有のファイル操作を担当する。
// TypeScript で言えば、この main.go は package.json の "bin" が指す薄いスクリプトに近い。
//
// Go では「package main」だけが実行可能プログラムになる。ほかのパッケージはライブラリとして import される。
package main

// import は括弧でまとめて書く。標準ライブラリ(context, os)と、自分のモジュール内のパッケージを空行で分けるのが慣習。
// "github.com/ayame113/..." はモジュールパス(go.mod の module 名)+ ディレクトリで、TypeScript の相対 import に相当する。
import (
	"context"
	"os"

	"github.com/ayame113/my-workspace-manager/internal/cli"
)

// main はプログラムの起点で、引数も戻り値も持たない(package main の func main だけが特別に起動される)。
//
// cli.Run は終了コード(int)を返す。os.Exit にそれを渡すと、プロセスがその値で即座に終了する。
// 終了コードは 0 が成功、1 が実行時の失敗、2 が使い方の誤り、130 が対話入力の中断。
// Node.js の process.exit(code) と同じで、main から return しても終了コードは伝わらないため、必ず os.Exit を呼ぶ。
//
//   - context.Background() は「キャンセルも期限も無い空の context」。
//     context.Context はキャンセルや期限を呼び出しの連鎖に伝える仕組みで、TypeScript の AbortSignal に近い。
//     Go では I/O を行う関数の第 1 引数に ctx を渡す習慣がある。ここが最上位なので空の context を作って渡している。
//   - os.Args は実行ファイル名を含む引数の配列(process.argv に相当)。os.Args[1:] で先頭の実行ファイル名を除く。
//   - cli.OSEnv() は環境変数など OS への依存をまとめたもの。テストでは別の値を渡せるように、ここで明示的に渡している。
func main() {
	os.Exit(cli.Run(context.Background(), os.Args[1:], cli.OSEnv()))
}
