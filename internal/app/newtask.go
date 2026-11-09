// newtask.go: ws new の中身です。task 名や新規ブランチ名をランダムに作る処理と、
// task を作ったうえで各 repo に worktree をまとめて作る NewTaskWithWorktrees を置いています。
// Go 特有の書き方(エラー処理、:=、スライスなど)の説明は app.go に書いてあるので、先にそちらを見てください。

package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/ayame113/my-workspace-manager/internal/fsx"
	"github.com/ayame113/my-workspace-manager/internal/gitexec"
	"github.com/ayame113/my-workspace-manager/internal/layout"
)

// const は定数の宣言です。ランダムな名前が既存のものと被り続けても、無限ループにならないよう試行回数に上限を設けています。
const maxRandomNameAttempts = 100

// randomNameNotTaken は「ランダムな名前を作り、taken が false を返すものが出るまで繰り返す」共通処理です。
// taken は「名前を受け取り、使用済みか(とエラー)を返す関数」で、引数として関数を渡しています(TypeScript のコールバックと同じ)。
// task 名なら「ディレクトリがあるか」、ブランチ名なら「git にあるか」と、被りの判定だけを呼び出し側から差し込めます。
// prefix は先頭に付ける文字列です。
func (a *App) randomNameNotTaken(taken func(name string) (bool, error), prefix string) (string, error) {
	// `for range n` は「n 回繰り返す」ループです(Go 1.22 以降。変数が要らないときの書き方で、TypeScript の for (let i=0; i<n; i++) に近い)。
	for range maxRandomNameAttempts {
		// a.intn は乱数を返す関数です。App に持たせてあるので、テストでは固定値に差し替えられます。
		name := prefix + layout.RandomName(a.intn)
		isTaken, err := taken(name)
		if err != nil {
			return "", err
		}
		if !isTaken {
			return name, nil
		}
	}
	// 上限回数試しても見つからなかった場合のエラーです。errors.New は固定メッセージのエラーを作ります。
	return "", errors.New("重ならないランダムな名前を作れませんでした")
}

// 日本語の補足: repos の名前を前置きして「何のための task か」が名前から分かるようにします。
// repos は ws new で選んだ repo 名のスライスです(0 個でもよい)。
// RandomTaskName returns a task name that has no directory under tasks/.
// The name is prefixed with the repo names (see layout.RepoTaskPrefix), so the task says what it is for.
func (a *App) RandomTaskName(repos []string) (string, error) {
	// prefix が空でなければ、後ろにハイフンを足して連結しやすくします。`+=` は文字列の連結代入です。
	prefix := layout.RepoTaskPrefix(repos)
	if prefix != "" {
		prefix += "-"
	}
	// 無名関数(func(name string) (bool, error) { ... })をその場で引数に渡しています。task のディレクトリが既にあれば「使用済み」です。
	// 戻り値の error は常に nil ですが、randomNameNotTaken が要求する関数の形に合わせています。
	return a.randomNameNotTaken(func(name string) (bool, error) {
		return fsx.Exists(a.layout.TaskDir(name)), nil
	}, prefix)
}

// 日本語の補足: すべての repo で共通の 1 つの名前にするため、どの repo のローカルにも origin にも無い名前を選びます。
// (task 内の全 repo で同じブランチ名にそろえたい、という要件によります。)
// RandomBranchName returns a branch name that exists neither locally nor on origin in any of repos.
func (a *App) RandomBranchName(ctx context.Context, repos []string) (string, error) {
	// make([]string, n) は長さ n の文字列スライスを作ります(中身は ""。ゼロ値)。bares[i] で添え字を指定して代入できます。
	bares := make([]string, len(repos))
	// bare repo が全部存在するかをここで先に確認し、パスを集めておきます。
	for i, repo := range repos {
		bare, err := a.requireBare(ctx, repo)
		if err != nil {
			return "", err
		}
		bares[i] = bare
	}
	// 被り判定の関数を渡します。ctx や bares は外側の変数で、クロージャとして使えます。
	// 末尾の引数 "" は prefix で、ブランチ名には前置きを付けない。
	return a.randomNameNotTaken(func(name string) (bool, error) {
		for _, bare := range bares {
			// []string{...} はスライスのリテラルです(TypeScript の [...])。ローカルブランチと origin のブランチの両方を調べます。
			for _, ref := range []string{"refs/heads/", "refs/remotes/origin/"} {
				exists, err := gitexec.RefExists(ctx, a.git, bare, ref+name)
				// `err != nil || exists` は、エラーが起きたか、既に存在した時点で判定を終えて結果を返す書き方です。
				// (exists, err) をそのまま返せるのは、どちらも「そこで確定する」値だからです。
				if err != nil || exists {
					return exists, err
				}
			}
		}
		return false, nil
	}, "")
}

// ValidateNewTaskName は、task 名が規則に合い、かつ同名のディレクトリがまだ無いことを確認します。問題なければ nil を返します。
// ValidateNewTaskName accepts a valid task name that has no directory yet.
func (a *App) ValidateNewTaskName(task string) error {
	if err := layout.ValidateTaskName(task); err != nil {
		return err
	}
	if fsx.Exists(a.layout.TaskDir(task)) {
		return fmt.Errorf("task '%s' は既に存在します", task)
	}
	return nil
}

// NewTaskOptions は ws new に渡す指定をまとめた構造体です。Repos は worktree を作る repo 名の一覧です。
type NewTaskOptions struct {
	Task        string
	Repos       []string
	Description string
}

// CreatedWorktree は、作成済みの worktree 1 つ分(どの repo か、どこに作ったか)です。
type CreatedWorktree struct {
	Repo string
	Path string
}

// NewTaskWithWorktreesResult は NewTaskWithWorktrees の結果です。Branch は全 repo に共通で付けたブランチ名です。
type NewTaskWithWorktreesResult struct {
	Dir       string
	Branch    string
	Worktrees []CreatedWorktree
}

// NewTaskWithWorktrees creates a task and, in each of repos, a worktree on one new branch based on the default base.
// Everything is checked before the first change. If a worktree fails midway, what was created stays and is returned along with the error.
// 引数 ctx は context.Context(キャンセルなどを伝える仕組み)です。
// repos が空なら task のディレクトリだけが作られます。戻り値の結果は、失敗したときも「どこまで作れたか」を呼び出し側が表示できるよう返します。
func (a *App) NewTaskWithWorktrees(ctx context.Context, task string, repos []string) (NewTaskWithWorktreesResult, error) {
	if err := a.ValidateNewTaskName(task); err != nil {
		return NewTaskWithWorktreesResult{}, err
	}
	// 各 repo の起点(base)を先に全部調べておきます。1 つでも無い repo があれば、何も作らずエラーで終了するためです。
	bases := make([]string, len(repos))
	// `for i, repo := range repos` は添え字と要素を同時に取ります。bases[i] が repos[i] に対応します。
	for i, repo := range repos {
		if _, err := a.requireBare(ctx, repo); err != nil {
			return NewTaskWithWorktreesResult{}, err
		}
		// DefaultBase は origin/HEAD、無ければ origin/main を返し、どちらも無ければ空文字を返します。
		base, err := a.DefaultBase(ctx, repo)
		if err != nil {
			return NewTaskWithWorktreesResult{}, err
		}
		if base == "" {
			return NewTaskWithWorktreesResult{}, fmt.Errorf("repo '%s' に起点にできる origin/HEAD も origin/main もありません", repo)
		}
		bases[i] = base
	}
	// ブランチ名は repo が 1 つ以上あるときだけ作ります。repo が無ければ worktree も作らないので不要です。
	// `var err error` を if の中で宣言しているのは、外側で定義済みの err を使い回さないためです。
	branch := ""
	if len(repos) > 0 {
		var err error
		if branch, err = a.RandomBranchName(ctx, repos); err != nil {
			return NewTaskWithWorktreesResult{}, err
		}
	}
	// ここまで検査に通ったので、実際にディレクトリを作り始めます(ここから先の変更は元に戻さない)。
	created, err := a.NewTask(task)
	if err != nil {
		return NewTaskWithWorktreesResult{}, err
	}
	// ここからが「作る」処理です。Dir と Branch を入れた結果を作り、あとで worktree を足していきます。
	result := NewTaskWithWorktreesResult{Dir: created.Dir, Branch: branch}
	for i, repo := range repos {
		// Create: true で、新規ブランチを起点(bases[i])から作ります。作業用 worktree が task 直下に作られます。
		path, err := a.Add(ctx, AddOptions{Task: task, Repo: repo, Branch: branch, Base: bases[i], Create: true})
		if err != nil {
			// 失敗したら、すでに作った分を残したまま、どの repo で失敗したかを包んだエラー(WorktreeCreateError)と、ここまでの結果を返します。
			return result, &WorktreeCreateError{Repo: repo, Err: err}
		}
		// append で、作れた worktree を結果に追加します。
		result.Worktrees = append(result.Worktrees, CreatedWorktree{Repo: repo, Path: path})
	}
	return result, nil
}

// WorktreeCreateError は「どの repo の worktree 作成に失敗したか」を持つエラー型です。Err には元のエラーを入れます。
// cli 層が errors.As でこの型を取り出し、「どこまで作れたか」と合わせて表示します。
type WorktreeCreateError struct {
	Repo string
	Err  error
}

// Error() を持つので error として扱えます(app.go の OutsideTaskError と同じ)。%v は値を文字列にして埋め込む指定です。
func (e *WorktreeCreateError) Error() string {
	return fmt.Sprintf("repo '%s' の worktree を作成できませんでした: %v", e.Repo, e.Err)
}

// Unwrap は errors.Is / errors.As が「中身の元のエラー」をたどるための決まったメソッド名です。
// これを定義すると、WorktreeCreateError の奥にある元のエラーも判定の対象になります(TypeScript の error.cause をたどれるようにするイメージ)。
func (e *WorktreeCreateError) Unwrap() error { return e.Err }
