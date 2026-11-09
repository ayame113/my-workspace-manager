// skill.go は `ws skill install` の本体。ws のバイナリに埋め込んだ Agent Skill(skills/ ディレクトリ)を、
// Claude Code や Codex が読むユーザー単位のスキルディレクトリに書き出す。
// 埋め込みには Go の `embed` を使うので、ws は単体のバイナリだけで配布でき、実行時に別のファイルを必要としない。
package app

// `github.com/ayame113/my-workspace-manager/skills` は、このリポジトリの skills パッケージ。
// そこで `//go:embed` により、SKILL.md などのファイルがバイナリの中に取り込まれ、
// `skills.FS`(読み取り専用のファイルシステム)として参照できる。
import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ayame113/my-workspace-manager/skills"
)

// SkillTargets are the agents whose user-level skills directory ws can install into.
// `var` はパッケージ全体で使う変数の宣言。`[]string{...}` は文字列のスライス(可変長配列)のリテラル。
// 大文字で始まるので他のパッケージ(CLI の選択肢の表示)からも見える。
var SkillTargets = []string{"claude", "codex"}

// SkillsDir returns the user-level skills directory of target.
// `(a *App)` はレシーバ(TypeScript の `this`)。戻り値は「パスと error」の 2 つで、成功なら error は nil。
// Go に例外は無く、失敗しうる関数は error を最後の戻り値で返し、呼び出し側が確認する。
func (a *App) SkillsDir(target string) (string, error) {
	// 環境変数の取得。os.Getenv を直接呼ばず App のフィールド(関数)を経由しているのは、テストで差し替えられるようにするため。
	// 関数も値として持てる(TypeScript の関数型のプロパティ)。
	home := a.getenv("HOME")
	if home == "" {
		// errors.New は新しいエラー値を作る。エラーのときの 1 つ目の戻り値は、使われないのでゼロ値(空文字)にする。
		return "", errors.New("HOME が設定されていません")
	}
	// switch は target の値で分岐する。Go の switch は、break を書かなくても次の case に落ちない。
	// どの case にも当たらなければ、switch の後ろの return(不明な指定)に進む。
	switch target {
	case "claude":
		return filepath.Join(home, ".claude", "skills"), nil
	case "codex":
		// Codex は ~/.codex ではなく ~/.agents/skills からユーザー単位の skill を読む
		return filepath.Join(home, ".agents", "skills"), nil
	}
	// fmt.Errorf は書式付きでエラーを作る(`%s` は文字列の埋め込み)。
	return "", fmt.Errorf("不明なインストール先です: %s", target)
}

// InstallSkills writes the skills bundled in the binary into the skills directory of target,
// overwriting files of the same name, and returns the directory of each skill.
// Files that an older version wrote and this one no longer ships are left in place.
// 引数は書き出し先のエージェント("claude" か "codex")。戻り値は、書き出した各スキルのディレクトリのスライスと error。
// 途中で失敗した場合は、そこまでに書き出せた分(installed)と error を一緒に返す。
func (a *App) InstallSkills(target string) ([]string, error) {
	// `:=` は宣言と代入を一緒に行う書き方で、型は自動で決まる。
	// `if err != nil { return ... }` が、Go のエラー処理の定型(TypeScript なら try/catch で受ける場面)。
	base, err := a.SkillsDir(target)
	if err != nil {
		return nil, err
	}
	// 埋め込まれたファイルシステムのルート(".")直下の一覧を得る。ws-task のようなスキルごとのディレクトリが並ぶ。
	// fs は `io/fs` パッケージで、実ディスクでも埋め込みでも同じように扱える共通のインターフェース(interface)を持つ。
	entries, err := fs.ReadDir(skills.FS, ".")
	if err != nil {
		return nil, err
	}
	// nil のスライスとして宣言する。append で要素を足すと、自動で確保される(初期化しなくてよい)。
	var installed []string
	// range でスライスを回す。1 つ目はインデックス(不要なので `_` で捨てる)、2 つ目が要素。
	for _, entry := range entries {
		dir := filepath.Join(base, entry.Name())
		// `if 前置き; 条件 {` の形で、err をこの if の中だけの変数にしている。
		// `%w` で元のエラーを包んで返すと、呼び出し側が errors.Is などで元のエラーを調べられる。
		if err := copyEmbedded(entry.Name(), dir); err != nil {
			return installed, fmt.Errorf("%s に書き込めません: %w", dir, err)
		}
		// append は「要素を足した新しいスライス」を返すので、自分自身に代入し直す。
		installed = append(installed, dir)
	}
	return installed, nil
}

// copyEmbedded は、埋め込みファイルシステムの src ディレクトリ配下を、実ディスクの dst にコピーする。
// `src, dst string` は同じ型の引数をまとめた書き方。同名のファイルは上書きする。
func copyEmbedded(src, dst string) error {
	// WalkDir は、src 配下のファイルとディレクトリを再帰的に 1 つずつ辿り、そのたびに最後の引数の関数を呼ぶ。
	// ここで渡している `func(...) error { ... }` は無名関数(クロージャ)で、TypeScript のアロー関数に近い。
	// 外側の変数(src、dst)を使える。コールバックが error を返すと、走査はそこで止まって、その error が WalkDir の戻り値になる。
	// 引数の err は、辿る途中で起きた読み取りエラー。
	return fs.WalkDir(skills.FS, src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// src からの相対パス(例: `ws-task/SKILL.md` に対する `SKILL.md`)を求め、書き出し先のパスを組み立てるのに使う。
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		// ディレクトリなら、書き出し先に同名のディレクトリを作る(親ディレクトリも必要なら一緒に作る)。
		// WalkDir は親ディレクトリを先に訪れるので、ファイルを書くときには、その置き先はすでにある。
		// 0o755 は 8 進数のファイル権限で、所有者は読み書き実行、他は読み取りと実行。
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		// ファイルなら、埋め込みの中身を読み出し、書き出し先に書く。os.WriteFile は既存のファイルを上書きする。
		// 0o644 は、所有者が読み書き、他は読み取りのみ。
		data, err := fs.ReadFile(skills.FS, path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
