// Package skills embeds the agent skills shipped with ws so that the binary can install them.
//
// ws のバイナリに skills/ws-task/ 以下のファイル(SKILL.md など)を丸ごと埋め込むパッケージ。
// 実行時にディスク上のファイルを探さなくてよいので、`go install` で入れた ws 単体で `ws skill install` が動く。
// 埋め込んだ内容は internal/app が読み出して、~/.claude/skills などに書き出す。
package skills

// embed パッケージは、下の embed.FS 型を使うために import している。
// //go:embed を使うファイルでは、この import が必須(使う型が無い場合は import _ "embed" と書く)。
import "embed"

// FS は skills/ws-task ディレクトリの中身を、読み取り専用の仮想ファイルシステムとして保持する。
//
// 下の「//go:embed ws-task」はコメントに見えるがコンパイラへの指示(ディレクティブ)で、
// 「このディレクトリをビルド時にバイナリへ取り込み、直後の変数に入れる」という意味になる。
// そのため、ディレクティブと変数宣言のあいだには何も挟めない(説明をこの位置に書いているのはそのため)。
// embed.FS は os と似た API(ReadFile、ReadDir など)で中身を読める。
// TypeScript で言うと、ビルド時にファイルを文字列として bundle に埋め込む仕組み(Vite の ?raw import など)に近い。
//
// Go では先頭が大文字の名前(FS)は他パッケージから見える(export される)。小文字だとパッケージ内だけで使える。
//
//go:embed ws-task
var FS embed.FS
