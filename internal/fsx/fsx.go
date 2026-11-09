// Package fsx は、標準ライブラリだけでは足りない OS(macOS)固有のファイル操作と、小さなファイル系の補助関数をまとめる。
// 主に ws compact が使う。APFS の clonefile(コピーオンライトの複製)と、変更検知に使う lstat の生の情報(inode、ctime)を提供する。
// 依存の向きは app → fsx。fsx は他の internal パッケージを import しない。
package fsx

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"slices"

	// golang.org/x/sys/unix は OS のシステムコールを直接呼ぶための公式の拡張パッケージ(標準ライブラリの外にある)。
	// 外部の依存は go.mod に記録され、ここで import した名前は unix.〇〇 の形で使う。
	"golang.org/x/sys/unix"
)

// Stat は lstat で得られるファイル情報のうち、ws compact が必要とするものだけを取り出した構造体。
// OS が返す生の構造体(unix.Stat_t)は環境依存の型を含むので、扱いやすい型に変換して保持している。
// 型名と同じ名前のパッケージ(os.Stat など)があっても、パッケージ名で修飾されるので衝突しない。
//
// フィールドの意味は次のとおり。
//   - IsRegular: 通常ファイルかどうか(ディレクトリ、symlink などでは false)。
//   - Size: バイト数。
//   - Mode: 権限ビット(0o755 のような値)。ファイルの種類を表す上位ビットは除いてある。
//   - Dev: ファイルが載っているデバイス(ボリューム)の番号。Ino と組み合わせて「同じファイルか」を判定する。
//   - Ino: inode 番号。同じボリューム内でファイルを一意に表す。
//   - CtimeNs: inode の最終変更時刻(ctime)をナノ秒で表したもの。
//     npm は展開時に mtime を固定値にするので、変更検知には mtime ではなく ctime を使う。
type Stat struct {
	IsRegular bool
	Size      int64
	Mode      uint32
	Dev       int64
	Ino       uint64
	CtimeNs   int64
}

// Exists follows symlinks; any failure to stat counts as absent.
//
// path が存在すれば true を返す。symlink は辿るので、リンク先が無い壊れた symlink は false になる。
// 権限エラーなど存在以外の理由で stat に失敗した場合も false として扱う。
//
// os.Stat は (ファイル情報, error) を返す。ここでは情報は不要なので _ で捨て、error が nil かどうかだけを見ている。
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// RealPath は symlink をすべて解決した実際のパスを返す。存在しないパスだと error を返す。
// macOS では /tmp が /private/tmp への symlink であるように、同じ場所が複数のパスで表せるので、比較の前にこれで正規化する。
// Node.js の fs.realpathSync に相当する。
func RealPath(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}

// SubdirNames returns the sorted names of directories directly under dir, excluding symlinks.
//
// dir の直下にあるサブディレクトリの名前(パスではなく名前だけ)を、辞書順に並べて返す。
// dir が読めなければ error を返す。ディレクトリが空なら nil(長さ 0 のスライス)を返す。
func SubdirNames(dir string) ([]string, error) {
	// os.ReadDir は dir 直下のエントリを、名前順に並べて返す。
	// 「if err != nil { return ... }」は Go のエラー処理の基本形で、失敗したらすぐ呼び出し元へエラーを返す。
	// このパターンが関数のあちこちに出てくるが、TypeScript の try/catch を使わず、1 つずつ確認する書き方。
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		// ReadDir のエントリは symlink を辿らない。symlink の場合、IsDir() は false になる。
		// そのため symlink で張られたディレクトリは除外される。
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	// slices.Sort は与えたスライスをその場で並べ替える(破壊的)。結果を代入し直す必要は無い。
	slices.Sort(names)
	return names, nil
}

// SHA256Hex はファイルの内容の SHA-256 ハッシュを、16 進数の文字列で返す。
// ws compact が「内容が同じファイル」を見つけるために使う。ファイルを開けない、読めない場合は error を返す。
func SHA256Hex(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	// defer は「この関数を抜けるときに実行する」予約。return がどこにあっても必ず Close される。
	// TypeScript の try { ... } finally { file.close() } に近い。
	defer file.Close()
	hash := sha256.New()
	// io.Copy はファイルを少しずつ読み、hash に書き込む。巨大なファイルでも全体をメモリに載せない。
	// hash は書き込まれたデータを順次ハッシュ計算に取り込む(Writer として振る舞う)。
	// if の中で宣言した err は、外側の err とは別の変数(このブロックの中だけで有効)。
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	// hash.Sum(nil) は最終的なハッシュ値のバイト列を返す。これを hex.EncodeToString で "ab12..." の文字列にする。
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Clonefile makes a copy-on-write copy of src at dst, which must not exist. Symlinks are not followed.
//
// APFS の clonefile(2) で src の複製を dst に作る。複製は元とディスク上のデータブロックを共有し、
// どちらかを書き換えたときに初めて差分だけが別に確保される(コピーオンライト)。
// そのため、内容が同じファイルを clonefile で置き換えると、見た目はそのままで容量が減る。
// dst が既にあると失敗する。APFS でないボリュームでも失敗する。成功すれば nil を返す。
//
// unix.CLONE_NOFOLLOW は「src が symlink のときに、リンク先ではなく symlink 自体を対象にする」というフラグ。
func Clonefile(src, dst string) error {
	return unix.Clonefile(src, dst, unix.CLONE_NOFOLLOW)
}

// Lstat does not follow symlinks.
//
// path の情報を Stat にして返す。os.Lstat ではなく unix.Lstat を使うのは、
// inode 番号(Ino)や ctime など、標準の os.FileInfo では取れない OS 固有の情報が必要だから。
// symlink を辿らないので、symlink のパスを渡すと symlink 自体の情報が得られる。
func Lstat(path string) (Stat, error) {
	// var st unix.Stat_t は、中身がゼロ値の構造体を作る。Lstat がこの変数に結果を書き込む。
	// &st は st のポインタ(アドレス)で、関数に「ここに書いてね」と渡すために使う。
	// C の出力引数と同じ考え方。TypeScript の参照渡しのオブジェクトに近い(Go は基本的に値渡しなので、書き換えさせたいときはポインタにする)。
	var st unix.Stat_t
	if err := unix.Lstat(path, &st); err != nil {
		// 失敗時は、戻り値の Stat にもゼロ値(全フィールドが空)を返す決まり。
		return Stat{}, err
	}
	// 各フィールドの変換の要点は次のとおり。
	//   - IsRegular: Mode はファイルの種類(上位ビット)と権限(下位ビット)が 1 つの数に入っている。
	//     & はビット演算の AND で、S_IFMT でマスクして種類だけを取り出し、通常ファイル(S_IFREG)かを比べる。
	//   - Mode: 0o7777 は 8 進数のリテラルで、権限ビット(setuid などを含む下位 12 ビット)だけを残すマスク。
	//     型が合わないものは uint32(...) のように明示的に変換する。Go は暗黙の型変換をしない。
	//   - CtimeNs: 秒(Sec)とナノ秒(Nsec)に分かれている時刻を、ナノ秒 1 つの整数にまとめる。
	//     1_000_000_000 の _ は桁区切りで、値には影響しない。
	return Stat{
		IsRegular: st.Mode&unix.S_IFMT == unix.S_IFREG,
		Size:      st.Size,
		Mode:      uint32(st.Mode & 0o7777),
		Dev:       int64(st.Dev),
		Ino:       st.Ino,
		CtimeNs:   st.Ctim.Sec*1_000_000_000 + st.Ctim.Nsec,
	}, nil
}
