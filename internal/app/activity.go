// activity.go: 「worktree や task を最後にいつ作業したか」の推定と、更新時刻での並べ替えを置いています。
// git の index などの更新日時を見て推定します(ファイルを編集しただけでは変わりません)。ws ls などの「最近作業した順」の元になります。
// Go 特有の書き方の説明は app.go に書いてあります。

package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// worktreeActivity は、worktree の最終作業時刻を返す関数です。読み取れなければ time.Time{}(ゼロ値。「不明」の意味)を返します。
// node_modules 全体を走査すると遅いので、git が add / commit / checkout / status で書き換えるファイルの更新時刻だけを数回 stat して推定します。
// worktreeActivity estimates when a worktree was last worked on from the files git rewrites on
// add, commit, checkout and status, so that it costs a few stats instead of walking node_modules.
// It returns the zero time when none of them can be read.
func worktreeActivity(worktree string) time.Time {
	// 作業用 worktree では .git は「実体への場所を書いたファイル」で、通常の git リポジトリではディレクトリです。
	// ファイルとして読めた場合だけ、書かれた先(gitdir:)に差し替えます。
	gitdir := filepath.Join(worktree, ".git")
	// `if data, err := os.ReadFile(...); err == nil {` は、読めたとき(エラーが nil)だけ中に入る形で、普段の `err != nil` とは逆です。
	// 読めなければ(= .git がディレクトリなら)そのまま gitdir を使います。
	if data, err := os.ReadFile(gitdir); err == nil {
		// strings.CutPrefix は「先頭が "gitdir: " ならそれを除いた残りと true を返す」関数です。
		target, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir: ")
		if !ok {
			// 想定外の中身なら、推定できないのでゼロ値の時刻を返します。
			return time.Time{}
		}
		// 相対パスで書かれている場合は、worktree からの相対と見て絶対パスにします。
		if !filepath.IsAbs(target) {
			target = filepath.Join(worktree, target)
		}
		gitdir = target
	}
	// time.Time{} はゼロ値で、「まだ 1 つも読めていない」状態から始めて、新しいものに更新していきます。
	var latest time.Time
	// 調べる 3 ファイルのうち、いちばん新しい更新時刻を採用します。index は add など、logs/HEAD は commit / checkout など、HEAD は切り替えで更新されます。
	for _, name := range []string{"index", "logs/HEAD", "HEAD"} {
		// `info, err := os.Stat(...)` でファイル情報を取り、エラー無し(存在する)かつ現在の最新より新しければ更新します。ModTime が更新時刻で、After は「より後か」の比較です。
		if info, err := os.Stat(filepath.Join(gitdir, name)); err == nil && info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	return latest
}

// modTime は path の更新時刻を返します。読めなければゼロ値を返します。
func modTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// newestFirst は items を updated で得た時刻の新しい順に並べ替えます(その場で並び替える。戻り値は無い)。
// 同じ時刻のものは元の順序(= 名前順)を保ちます(stable 安定ソート)。
// [T any] は「型パラメータ」で、ジェネリクスです。TypeScript の <T> と同じで、T はどんな型でもよく、呼び出し側の items の型から自動で決まります。
// updated は「T を受け取り time.Time を返す関数」で、要素から並べ替えのキーを取り出す役です。
// newestFirst sorts by updated descending, keeping the existing (name) order for ties.
func newestFirst[T any](items []T, updated func(T) time.Time) {
	// SortStableFunc は比較関数を渡して並べ替えます。比較関数は「負 / 0 / 正」の int を返す JavaScript の sort と同じ約束です。
	// ここは b と a を逆に比べることで降順(新しい順)にしています。Compare は time.Time の比較メソッドです。
	slices.SortStableFunc(items, func(a, b T) int {
		return updated(b).Compare(updated(a))
	})
}

// taskActivity は task の最終作業時刻を返します。worktree があればその中で最新のもの、無ければ task ディレクトリ自体の更新時刻です。
// entries は呼び出し側で取得済みの worktree 一覧で、git を再度呼ばないために引数で受け取ります。
// taskActivity is the newest activity among the task's worktrees, or the task directory's mtime when it has none.
func (a *App) taskActivity(task string, entries []WorktreeEntry) time.Time {
	if len(entries) == 0 {
		return modTime(a.layout.TaskDir(task))
	}
	var latest time.Time
	for _, entry := range entries {
		if entry.Updated.After(latest) {
			latest = entry.Updated
		}
	}
	return latest
}
