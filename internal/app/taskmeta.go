// taskmeta.go は task ごとのメタデータ(いまは 1 行の description)を、`tasks/<task>/.ws-task.json` に読み書きする。
// `ws describe` や、`ws ls` の説明表示・検索から使われる。
// ws が知らないキーを消さずに書き戻す(新しい ws が追加したキーを、古い ws が消してしまわない)のが設計上の要点。
package app

// `encoding/json` は Go 標準の JSON ライブラリ(JS の JSON.parse / stringify に相当)。
// `io/fs` はファイルシステムの共通の型とエラー(fs.ErrNotExist など)を持つ。
import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	// 保存するファイル名と、JSON のキー名。keyInstructions(`_comment`)は、ファイルを開いた人やエージェントに
	// 「直接編集せず ws describe を使う」と伝えるための注意書きで、書き込みのたびに付け直す。
	taskMetaFile    = ".ws-task.json"
	keyDescription  = "description"
	keyInstructions = "_comment"
)

// taskMetaPath は task のメタデータファイルのパスを返す。
// `func (a *App) ...` の `(a *App)` はレシーバで、TypeScript のクラスメソッドの `this`(a が this)に相当する。
// `*` はポインタ(コピーでなく本体を参照する)を表す。
func (a *App) taskMetaPath(task string) string {
	return filepath.Join(a.layout.TaskDir(task), taskMetaFile)
}

// readTaskMeta returns every key of the task's metadata file, including ones this version of ws does not know,
// so that writing it back never drops what a newer ws stored. A missing file is an empty map.
// 戻り値は「キー → 生の JSON」の map と error。Go の関数は複数の値を返せる。
// `json.RawMessage` は「中身を解釈せず、JSON のバイト列のまま持つ」型。TypeScript の `Record<string, unknown>` に近いが、
// 値を構造体や文字列に変換する前の状態を保てるので、知らないキーの値も、そのまま元の形で書き戻せる。
func (a *App) readTaskMeta(task string) (map[string]json.RawMessage, error) {
	path := a.taskMetaPath(task)
	// ファイル全体を []byte(バイト列)として読む。
	data, err := os.ReadFile(path)
	// errors.Is は、エラー(が包んでいるエラーも含めて)が特定のエラーかを調べる。
	// 「ファイルが無い」は異常ではなく、メタデータが未設定という意味なので、空の map を返す。
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	// 空の map の作り方。nil の map には書き込めないので、`{}` で初期化する。
	meta := map[string]json.RawMessage{}
	// Unmarshal は JSON のバイト列を、渡した変数の中に読み込む(JSON.parse に近い)。
	// 結果を書き込めるように、変数そのものではなくポインタ(`&meta`)を渡す。
	// `fmt.Errorf` の `%w` は、元のエラーを包み込んで(wrap)新しいエラーを作る。
	// 包んだエラーは、後から errors.Is / errors.As で取り出して判定できる。TypeScript の `new Error(msg, { cause: err })` に近い。
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("%s を読めません: %w", path, err)
	}
	return meta, nil
}

// writeTaskMeta はメタデータを保存する。meta が空になったらファイル自体を削除する。
// `_comment`(注意書き)は保存のたびに付け直すため、いったん meta から取り除いてから空かどうかを判定する
// (注意書きだけのファイルを残さないため)。
//
// 保存は「同じディレクトリに一時ファイルを作って書き込み、rename で置き換える」方式。
// rename は原子的(途中の状態が他から見えない)なので、書き込み途中の中断でファイルが壊れない。
func (a *App) writeTaskMeta(task string, meta map[string]json.RawMessage) error {
	path := a.taskMetaPath(task)
	// 組み込み関数 delete は map からキーを削除する(JS の `delete obj[key]`)。キーが無くてもエラーにならない。
	delete(meta, keyInstructions)
	// 説明が 1 つも無ければファイルは不要。無ければ(ErrNotExist)、そのまま成功として扱う。
	if len(meta) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	// 注意書きの文字列を JSON の値に変換する。`%[1]s` は「1 番目の引数」を指す書式で、同じ値(task)を 2 か所に使うために番号を付けている。
	instructions, err := marshalReadable(fmt.Sprintf(
		"このファイルは ws が管理する。直接編集せず、説明の表示は `ws describe --task %[1]s`、変更は `ws describe --task %[1]s \"<1 行の説明>\"` で行う。", task))
	if err != nil {
		return err
	}
	// 注意書きを meta に戻す。
	meta[keyInstructions] = instructions
	data, err := marshalReadable(meta)
	if err != nil {
		return err
	}
	// 最終的な置き先と同じディレクトリに、一時ファイルを作る。別のファイルシステム(ボリューム)にあると、rename が原子的にならない、
	// または失敗するため。`*` はランダムな文字列に置き換わる。
	tmp, err := os.CreateTemp(filepath.Dir(path), taskMetaFile+".tmp-*")
	if err != nil {
		return err
	}
	// defer は「この関数が終わるとき(成功でも失敗でも)に実行する」予約。TypeScript の `try { ... } finally { ... }` の finally に近い。
	// 成功時は rename でファイルが移動済みなので削除は空振りし、失敗時は一時ファイルの後始末になる。
	defer os.Remove(tmp.Name())
	// 書き込みに失敗したら、閉じてからエラーを返す(defer による削除が後で実行される)。
	// `_` は書き込んだバイト数を使わないという意味。
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// 一時ファイルを本来のパスに rename して、原子的に置き換える。
	return os.Rename(tmp.Name(), path)
}

// marshalReadable keeps <, > and & as they are, since people and agents read this file.
// v を、人が読みやすい形(インデント付き、HTML のエスケープなし)の JSON に変換する。
// `any` は「任意の型」を表す(TypeScript の `unknown` / `any` に近い。Go の interface{} の別名)。
// Go 標準の json.Marshal は `<`、`>`、`&` を `\u003c` のようにエスケープするので、Encoder の設定でそれを止めている。
func marshalReadable(v any) (json.RawMessage, error) {
	// bytes.Buffer はバイト列を溜めるバッファで、Encoder の出力先にする。
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(v); err != nil {
		return nil, err
	}
	// Encode は末尾に改行を付けるので、保存したファイルは改行で終わる。
	return buf.Bytes(), nil
}

// Description returns the task's one-line description, or "" when it has none.
// Description は task の description を返す。無ければ空文字。
func (a *App) Description(task string) (string, error) {
	meta, err := a.readTaskMeta(task)
	if err != nil {
		return "", err
	}
	// map のキーの存在確認(`v, ok := m[k]`)。キーが無いときは ok が false になる。
	raw, ok := meta[keyDescription]
	if !ok {
		return "", nil
	}
	// `var 名前 型` で、ゼロ値(文字列なら "")のまま変数を宣言する。この変数に JSON の値を読み込む。
	// JSON の値が文字列でなければ Unmarshal が失敗するので、手で編集された壊れたファイルを検出できる。
	var description string
	if err := json.Unmarshal(raw, &description); err != nil {
		return "", fmt.Errorf("%s の %s が文字列ではありません", a.taskMetaPath(task), keyDescription)
	}
	return description, nil
}

// SetDescription stores description as a single line (whitespace runs, including newlines, become one space).
// An empty description removes it. It returns the stored value.
// SetDescription は description を保存し、保存した値(整形後)を返す。
// 他のキーは、読み込んだ meta をそのまま使うので保たれる。
func (a *App) SetDescription(task, description string) (string, error) {
	meta, err := a.readTaskMeta(task)
	if err != nil {
		return "", err
	}
	// Fields は空白(改行、タブを含む)区切りで分割して空要素を除く。それを半角スペース 1 つで連結し直して、1 行に整える。
	description = strings.Join(strings.Fields(description), " ")
	if description == "" {
		delete(meta, keyDescription)
	} else {
		raw, err := marshalReadable(description)
		if err != nil {
			return "", err
		}
		meta[keyDescription] = raw
	}
	// 保存の結果(エラーまたは nil)をそのまま返す。
	return description, a.writeTaskMeta(task, meta)
}
