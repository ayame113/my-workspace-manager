// compact_index.go は `ws compact` の「処理済みファイルの記録(インデックス)」の読み書きを担当する。
// 記録は `$WS_ROOT/.compact-index` にタブ区切りのテキストで保存する。
// 2 回目以降の compact は、記録と一致する(変更されていない)ファイルのハッシュ計算と置き換えを省ける。
package app

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/ayame113/my-workspace-manager/internal/fsx"
)

// インデックスファイルの 1 行目に書くバージョン文字列。形式を変えたらここを上げる。
// 一致しない古い形式のファイルは、読み込み時に「空」として扱われ、全件を処理し直す。
const compactIndexHeader = "ws-compact-index-v2"

// indexEntry records the state of a processed file. A matching inode, ctime and size mean the content is unchanged,
// so the recorded hash can be reused. ctime is used instead of mtime because npm pins mtime to a fixed date on extraction.
// フィールドは inode、ctime(ナノ秒)、サイズ、ハッシュ、共有済みかどうか。
// フィールド名が小文字で始まるので、パッケージの外からは見えない(非公開)。
type indexEntry struct {
	inode   uint64
	ctimeNs int64
	size    int64
	// 空なら、サイズが一意で計算していない
	hash string
	// 同じキーの代表のクローンに置き換え済み
	shared bool
}

// matches は、記録された状態が今のファイルの状態(stat)と一致するかを返す。
// レシーバが `*` なしの `(e indexEntry)` なので、構造体のコピーを受け取る(書き換えても呼び出し元には影響しない)。
// 小さな読み取り専用のメソッドではこの形にすることが多い。
func (e indexEntry) matches(stat fsx.Stat) bool {
	return e.inode == stat.Ino && e.ctimeNs == stat.CtimeNs && e.size == stat.Size
}

// loadCompactIndex returns an empty index when the file is missing or corrupt, which reprocesses every file.
// 引数は読み込むファイルのパス。戻り値は「ファイルパス → 記録」の map(TypeScript の `Record<string, IndexEntry>`)。
// エラーを返さず、どんな失敗でも空の map を返す設計。インデックスは単なる高速化の記録なので、
// 失敗したら捨てて全件を処理し直せばよいため。
//
// ファイルの形式: 1 行目がヘッダ、2 行目以降が 1 ファイル 1 行で、
// `inode<TAB>ctime<TAB>size<TAB>hash(無ければ-)<TAB>shared(0/1)<TAB>パス`。パスは最後なので、パスにタブが入っていても読める。
func loadCompactIndex(path string) map[string]indexEntry {
	// 空の map を作る。
	entries := map[string]indexEntry{}
	// ファイル全体をバイト列([]byte)として読む。ファイルが無い場合もここでエラーになり、空の map を返す。
	data, err := os.ReadFile(path)
	if err != nil {
		return entries
	}
	// []byte を string に変換し、改行で分割して行のスライスにする。
	lines := strings.Split(string(data), "\n")
	if lines[0] != compactIndexHeader {
		return map[string]indexEntry{}
	}
	// `lines[1:]` はスライスの 1 番目以降(ヘッダを除く)。TypeScript の `lines.slice(1)` に近い。
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		// 最大 6 個に分割する。6 個目(パス)の中にタブがあっても、それ以上は分割されない。
		// 形式が想定と違えば、ファイル全体を信用せず空の map を返す。
		fields := strings.SplitN(line, "\t", 6)
		if len(fields) != 6 {
			return map[string]indexEntry{}
		}
		// 文字列を数値に変換する(10 進数、64 ビット)。変換できなければ 2 つ目の戻り値にエラーが入る。
		// 3 つの変換をまとめて実行し、後でまとめて確認するために、err1 〜 err3 と別々の名前にしている。
		inode, err1 := strconv.ParseUint(fields[0], 10, 64)
		ctimeNs, err2 := strconv.ParseInt(fields[1], 10, 64)
		size, err3 := strconv.ParseInt(fields[2], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil {
			return map[string]indexEntry{}
		}
		// ハッシュが計算されていないものは、ファイル上では "-" で表し、メモリ上では空文字で表す。
		hash := fields[3]
		if hash == "-" {
			hash = ""
		}
		// map への代入。キーはパス。
		entries[fields[5]] = indexEntry{inode: inode, ctimeNs: ctimeNs, size: size, hash: hash, shared: fields[4] == "1"}
	}
	return entries
}

// saveCompactIndex writes to a temporary file and renames it so an interruption never leaves a broken index.
// 引数は保存先のパスと、保存する内容。戻り値は失敗時のエラー(成功なら nil)。
//
// 一時ファイルに書いてから rename で置き換える理由: ファイルに直接書き込むと、書き込み中に中断(Ctrl-C や電源断)された場合、
// 壊れたインデックスが残る。rename は原子的(途中の状態が見えない)なので、
// 読み手は常に「古い完全なファイル」か「新しい完全なファイル」のどちらかを見る。
func saveCompactIndex(path string, entries map[string]indexEntry) error {
	// strings.Builder は文字列を効率よく組み立てるための型(TypeScript で文字列の配列に push して最後に join するのに近い)。
	// `var` で宣言するだけで、ゼロ値のまま使える。
	var text strings.Builder
	text.WriteString(compactIndexHeader + "\n")
	// map のキーをソートして順に処理する。Go の map は反復順が不定なので、ソートしておけば、
	// 同じ内容なら毎回同じファイルができる(差分が安定する)。
	for _, file := range slices.Sorted(maps.Keys(entries)) {
		entry := entries[file]
		// 複数の変数を 1 行で宣言・代入できる。
		shared, hash := "0", entry.hash
		if entry.shared {
			shared = "1"
		}
		if hash == "" {
			hash = "-"
		}
		// Fprintf は書式付きで、指定した書き込み先(ここでは text。`&` でポインタを渡している)に出力する。
		// `%d` は整数、`%s` は文字列。
		fmt.Fprintf(&text, "%d\t%d\t%d\t%s\t%s\t%s\n", entry.inode, entry.ctimeNs, entry.size, hash, shared, file)
	}
	// 同じディレクトリに一時ファイルを書く。別ディレクトリ(別ボリューム)だと rename が原子的にならない、または失敗するため。
	temporary := path + ".tmp"
	// 0o644 は 8 進数(0o 接頭辞)のファイル権限で、所有者は読み書き、他は読み取りのみ。
	if err := os.WriteFile(temporary, []byte(text.String()), 0o644); err != nil {
		return err
	}
	// 一時ファイルを本来のパスに rename して、原子的に置き換える。
	return os.Rename(temporary, path)
}
