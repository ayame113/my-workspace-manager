// このファイルは、ws のサブコマンド(new / clone / ls / describe / open / lint / compact / worktree / skill)を
// cobra で定義する。各コマンドが「どんな引数とフラグを受け取り、何を呼ぶか」を組み立てている。
// 実際の処理は internal/app に任せ、ここでは引数の検証、対話するかどうかの判断、結果の表示を担当する。
// 対話する条件は stdin と stderr がどちらも端末のとき(r.interactive())。そうでなければ、足りない引数は使い方の誤り(終了コード 2)にする。
package cli

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/ayame113/my-workspace-manager/internal/app"
	"github.com/ayame113/my-workspace-manager/internal/prompt"
)

// taskFlagUsage is the help text shared by every --task flag.
//
// taskFlagUsage は、各コマンドの --task フラグのヘルプ文。複数のコマンドで同じ文言を使うので定数にしてある。
// const は定数宣言で、TypeScript の const に近い(ただし Go の const は数値や文字列などの単純な値だけ)。
// ws worktree 系のコマンドは --task を省略すると、カレントディレクトリが属する task を対象にする。
const taskFlagUsage = "対象の task。省略するとカレントディレクトリが属する task(. も同じ)"

// rootCommand builds the root `ws` command with all subcommands attached.
//
// rootCommand は ws コマンド全体(コマンドの木のルート)を組み立てて返す。Run から 1 回だけ呼ばれる。
// cobra では、1 つのコマンドを cobra.Command 構造体で表し、AddCommand でサブコマンドをぶら下げて木にする。
// `ws worktree add` なら、root → worktree → add と辿る。
// 以降のコマンド定義も同じ形で、`func (r *runner) xxxCommand() *cobra.Command` が cobra.Command のポインタを返す。
// r のメソッドにしているのは、コマンドの RunE の中から r.app や r.env を使うため(クロージャで r を捕まえる)。
func (r *runner) rootCommand() *cobra.Command {
	// &cobra.Command{...} は構造体を作ってポインタを得る書き方。フィールドは「名前: 値」で書く。
	// 主なフィールド:
	//   Use: 使い方の書式。最初の単語がコマンド名で、続きは引数の説明([] は省略可、<> は必須、... は複数)。
	//   Short: 一覧(--help)に出る 1 行の説明。
	//   Version: 設定すると --version フラグが使えるようになる。
	//   SilenceUsage / SilenceErrors: エラー時に cobra が自動で usage やエラーを表示するのを止める。
	//     表示は exitCode で自前で行い、終了コードも自分で決めるため。
	//   PersistentPreRunE: このコマンドと、そのすべてのサブコマンドの実行前に呼ばれる関数(beforeRun 参照)。
	//   RunE: コマンドが実行されたときに呼ばれる関数。E が付くのは「error を返せる版」(付かない Run は error を返せない)。
	//     ここでは、ws だけ、または未知のサブコマンドで実行されたとき用に requireSubcommand を設定する。
	root := &cobra.Command{
		Use:               "ws",
		Short:             "git worktree をタスク単位で管理する",
		Version:           version(),
		SilenceUsage:      true,
		SilenceErrors:     true,
		PersistentPreRunE: r.beforeRun,
		RunE:              requireSubcommand,
	}
	// フラグ(--version / -V)を登録する。BoolP の P は「短縮形(1 文字)も付ける」の意味。
	// 引数は(フラグ名、短縮形、既定値、説明)。Flags() はそのコマンド専用のフラグ集合。
	root.Flags().BoolP("version", "V", false, "バージョンを表示する")
	// --version の出力形式。"{{.Version}}" は Go のテンプレート構文で、バージョン文字列に置き換わる。
	root.SetVersionTemplate("{{.Version}}\n")
	root.AddCommand(
		r.newCommand(),
		r.cloneCommand(),
		r.lsCommand(),
		r.describeCommand(),
		r.openCommand(),
		r.lintCommand(),
		r.compactCommand(),
		r.worktreeCommand(),
		r.skillCommand(),
	)
	// 全コマンドを登録し終えた後に、ファイル名補完を無効にする(completion.go 参照)。
	disableFileCompletion(root)
	return root
}

// requireSubcommand is the RunE of commands that only group subcommands (ws, ws worktree, ws skill).
//
// requireSubcommand は、サブコマンドをまとめるだけのコマンド(ws、ws worktree、ws skill)の RunE。
// ここに来るのは、サブコマンドが指定されなかった場合か、cobra が該当するサブコマンドを見つけられなかった場合で、
// どちらも使い方の誤りとして usagef で返す(終了コード 2)。
// 関数の引数 cmd は使わないが、cobra が RunE に渡す形(func(*cobra.Command, []string) error)に合わせるため書いてある。
func requireSubcommand(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return usagef("不明なサブコマンドです: %s", args[0])
	}
	return usagef("サブコマンドを指定してください")
}

// beforeRun runs after cobra has parsed arguments, so anything cobra rejects earlier is a usage error.
//
// beforeRun は PersistentPreRunE として、すべてのコマンドの実行直前に呼ばれる。
// cobra が引数とフラグの解析を終えた後で呼ばれるので、r.started = true にすることで、
// 「解析で弾かれた失敗(未知のフラグなど)は started が false のまま」と区別できる。exitCode はこれで使い方の誤りを判定する。
// もう 1 つの役割は、--task --repo のように「値を取るフラグが次のフラグを値として食べた」ケースを使い方の誤りにすること。
func (r *runner) beforeRun(cmd *cobra.Command, args []string) error {
	r.started = true
	// var は、型を明示して変数を宣言する書き方(初期値なし)。error 型のゼロ値は nil なので、「まだエラーなし」になる。
	var err error
	// Visit は、コマンドラインで実際に指定されたフラグだけを 1 つずつ関数に渡す。
	// 渡している無名関数(func(f *pflag.Flag) { ... })は、外側の変数 err を書き換えられる(クロージャ)。
	// Go では、コールバックの中から外側の変数を直接更新して結果を持ち出すことが多い。
	cmd.Flags().Visit(func(f *pflag.Flag) {
		// 値を取るフラグが次のフラグを値として食べた場合(--task --repo など)を使い方の誤りにする
		if f.Value.Type() == "string" && strings.HasPrefix(f.Value.String(), "-") && err == nil {
			err = usagef("--%s に値が必要です(受け取った値: %s)", f.Name, f.Value.String())
		}
	})
	return err
}

// printf writes formatted text to stdout.
//
// printf は stdout に書式付きで出力する。このファイルや output.go のメッセージ表示はすべてこれを通す。
// r.env.Stdout を経由することで、テストでは出力をバッファに集めて検証できる(os.Stdout に直接書かない)。
// fmt.Fprintf は、第 1 引数の出力先(io.Writer)に書式付きで書く関数。fmt.Printf の「出力先を指定できる版」。
func (r *runner) printf(format string, args ...any) {
	fmt.Fprintf(r.env.Stdout, format, args...)
}

// newCommand defines `ws new [<task>] [--description <description>]`.
//
// newCommand は ws new を定義する。task(~/ws/tasks/<task>/ のディレクトリ)を作るコマンド。
// 引数ありなら task を作るだけで、対話しない。引数なしなら、対話の環境のときだけ、
// task 名と repo を選んで worktree までまとめて作る。対話できなければ使い方の誤り。
func (r *runner) newCommand() *cobra.Command {
	// --description の値の入れ物。後で StringVar がこの変数のアドレスを受け取り、フラグの値をここに書き込む。
	var description string
	// Args は、位置引数の個数や中身を検証する設定。cobra.MaximumNArgs(1) は「0 個か 1 個まで」。
	// 範囲外だと cobra が RunE を呼ぶ前にエラーにする(その場合は started が false のままなので、使い方の誤りになる)。
	// ほかに cobra.NoArgs(0 個)、RangeArgs(最小, 最大)、OnlyValidArgs(ValidArgs に含まれるものだけ)などがある。
	// RunE に渡している func(cmd *cobra.Command, args []string) error { ... } は無名関数(関数リテラル)。
	// TypeScript の `(cmd, args) => { ... }` に近く、外側の変数(r、description)をクロージャで使える。
	// args はフラグを除いた位置引数のスライス。
	cmd := &cobra.Command{
		Use:   "new [<task>]",
		Short: "tasks/<task>/ を作る(引数なしなら対話で task 名と repo を選び、worktree もまとめて作る)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				return r.runNewTask(args[0], description)
			}
			if !r.interactive() {
				return usagef("task 名が必要です")
			}
			return r.runNewInteractive(cmd)
		},
	}
	// フラグを登録する。StringVar(変数のポインタ, フラグ名, 既定値, 説明)。
	// &description は description のアドレス(ポインタ)で、cobra が解析した値をそこに書き込む。
	// Go では、関数に「この変数に結果を入れてほしい」と頼むときにポインタを渡す。
	cmd.Flags().StringVar(&description, "description", "", "task の 1 行の説明(新しく作ったときだけ書き込む)")
	return cmd
}

// runNewTask creates the task directory, and stores the description when it is newly created.
//
// runNewTask は引数付きの ws new <task> の本体。task を作るだけで、worktree は作らない。
// 既に同名の task があれば、何も変えずにその旨を表示して正常終了する(何度実行しても同じ結果になる冪等な動き)。
// description は新しく作ったときだけ書き込み、既存の task の説明は上書きしない。
func (r *runner) runNewTask(task, description string) error {
	// result と err の 2 つを返す関数の結果を、:= でまとめて受け取る。
	result, err := r.app.NewTask(task)
	if err != nil {
		return err
	}
	if !result.Created {
		r.printf("task '%s' は既に存在します: %s\n", task, result.Dir)
		if description != "" {
			r.printf("説明は変更していません。変えるなら ws describe --task %s を使ってください\n", task)
		}
		return nil
	}
	r.printf("作成しました: %s\n", result.Dir)
	if description != "" {
		return r.setDescription(task, description)
	}
	return nil
}

// runNewInteractive asks for the task name and repos, creates the task with a worktree per repo, and optionally opens it.
//
// runNewInteractive は引数なしの ws new の本体。対話で聞いた内容を元に task と worktree を作る。流れは次のとおり。
//  1. 対話で task 名、repo、description、作成後に開くかを聞く
//  2. task を作り、選んだ各 repo に新規ブランチの worktree を作る
//  3. 途中で worktree の作成に失敗しても、作成済みのものは残して、どこまでできたかを表示して終了コード 1 にする
//  4. 作成に成功し、開くことが選ばれていれば、エディタ(Zed)で task を開く
func (r *runner) runNewInteractive(cmd *cobra.Command) error {
	chosen, err := r.prompter().New(cmd.Context())
	if err != nil {
		return err
	}
	result, err := r.app.NewTaskWithWorktrees(cmd.Context(), chosen.Task, chosen.Repos)
	// worktree の作成に失敗した場合でも、result.Dir があれば task のディレクトリはできている。
	// その場合も、作成済みの分は表示して description も書く。
	if result.Dir != "" {
		r.printNewTask(result)
		if chosen.Description != "" {
			// `_, descErr := ...` は、1 つ目の戻り値を使わないので捨てる。
			// errors.Join は、複数のエラーを 1 つにまとめる。err が nil なら、descErr だけが残る。
			if _, descErr := r.app.SetDescription(chosen.Task, chosen.Description); descErr != nil {
				return errors.Join(err, descErr)
			}
		}
	}
	// *app.WorktreeCreateError は、worktree の作成に失敗したことを表すエラー型(* はポインタ)。
	// この場合は、何がどこまで作られたかを表示した上で、exitError で終了コード 1 にする(メッセージは表示済み)。
	if worktreeErr, ok := errors.AsType[*app.WorktreeCreateError](err); ok {
		fmt.Fprintf(r.env.Stderr, "ws: %v\n", worktreeErr)
		fmt.Fprintf(r.env.Stderr, "作成済みのものは残しています(%d / %d repo)\n", len(result.Worktrees), len(chosen.Repos))
		return exitError{ExitFailure}
	}
	// err が非 nil なら、そのエラーを返して終わる。成功していても、開かないと選ばれていれば、ここで終わる(err は nil)。
	if err != nil || !chosen.Open {
		return err
	}
	return r.openTask(cmd, chosen.Task)
}

// cloneCommand defines `ws clone <url> [<repo>]`.
//
// cloneCommand は ws clone を定義する。リモートのリポジトリを、bare repo として ~/ws/repos/<repo>.git に clone する。
// bare repo は作業ツリーを持たない git リポジトリで、ws はここから git worktree を作って作業用のディレクトリにする。
// repo 名を省略すると、URL から導出する。
func (r *runner) cloneCommand() *cobra.Command {
	// Args の RangeArgs(1, 2) は「1 個以上 2 個以下」。
	return &cobra.Command{
		Use:   "clone <url> [<repo>]",
		Short: "bare repo を repos/ に用意する(repo 名は省略すると URL から導出)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// 初期値 "" を入れた変数を、条件によって上書きする。Go には三項演算子が無いので、この形で書く。
			repo := ""
			if len(args) == 2 {
				repo = args[1]
			}
			// cmd.Context() は、Run の ctx(ExecuteContext で渡したもの)。
			// 時間のかかる git の実行に、中断を伝えるために渡す。
			bare, err := r.app.Clone(cmd.Context(), args[0], repo)
			if err != nil {
				return err
			}
			r.printf("作成しました: %s\n", bare)
			return nil
		},
	}
}

// lsCommand defines `ws ls [<語>]`.
//
// lsCommand は ws ls を定義する。全 task の一覧を、最近作業した順に表示する。
// 語を渡すと、task 名か description に含むものだけに絞る(大文字小文字は区別しない)。
// 端末で実行すると絞り込みの選択画面になり、選んだ task のディレクトリだけを stdout に出す。
// 端末でなければ(エージェントやスクリプトなど)、一覧をそのまま出す。
func (r *runner) lsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "ls [<語>]",
		Short: "全 task の一覧(1 task 1 行、最近作業した順)。語を渡すと task 名か説明に含むものだけ。端末からは文字入力で絞り込んで選び、その task のディレクトリを出力する",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tasks, err := r.app.ListTasks(cmd.Context())
			if err != nil {
				return err
			}
			if len(args) == 1 {
				// slices.DeleteFunc は、関数が true を返した要素を取り除いたスライスを返す。
				// 「含まない」ものを消すので、`!containsFold(...)` と否定している。
				// 検索対象は task 名と description を空白でつないだ 1 つの文字列。
				tasks = slices.DeleteFunc(tasks, func(task app.TaskSummary) bool {
					return !containsFold(task.Name+" "+task.Description, args[0])
				})
			}
			if r.interactive() {
				return r.pickTask(cmd, tasks)
			}
			r.printTasks(tasks)
			return nil
		},
	}
}

// lintCommand defines `ws lint`.
//
// lintCommand は ws lint を定義する。workspace 全体を検査して、規約に反するもの(壊れた設定やディレクトリ構成)を報告する。
// 報告するだけで、修正はしない。問題があれば終了コード 1 になる(reportIssues 参照)。
func (r *runner) lintCommand() *cobra.Command {
	// Args の NoArgs は「位置引数を受け取らない」。
	cmd := &cobra.Command{
		Use:   "lint",
		Short: "workspace 全体の規約違反と壊れた worktree を検査する",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return r.reportIssues(r.app.LintWorkspace(cmd.Context()))
		},
	}
	return cmd
}

// compactCommand defines `ws compact [--dry-run]`.
//
// compactCommand は ws compact を定義する。全 task の node_modules の中にある同じ内容のファイルを、
// APFS(macOS のファイルシステム)の clonefile で共有して、ディスク容量を減らす。
// --dry-run を付けると、何も置き換えずに対象件数と削減見込みだけを表示する。
func (r *runner) compactCommand() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "compact",
		Short: "全 task の node_modules 内の同一ファイルを clonefile で共有し、容量を減らす",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// r.printCompactEvent は、処理の途中経過を受け取るコールバック。メソッドを値として渡している。
			result, err := r.app.Compact(cmd.Context(), dryRun, r.printCompactEvent)
			if err != nil {
				return err
			}
			r.printCompactResult(result)
			return nil
		},
	}
	// BoolVar(変数のポインタ, フラグ名, 既定値, 説明)。--dry-run を付ければ dryRun が true になる。
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "置き換えずに、対象件数と削減見込みだけ表示する")
	return cmd
}

// worktreeCommand defines `ws worktree`, the parent of the worktree subcommands.
//
// worktreeCommand は ws worktree を定義する。worktree 系の操作をまとめる親コマンドで、自分では何もしない。
// 配下の add / rename / ls / lint / pull は、task 内の worktree が対象になる。
// worktree は、task の中にある 1 つのブランチ用の作業ディレクトリ(実体は git worktree)。
// 親コマンドは RunE に requireSubcommand を設定して、サブコマンドなしの実行を使い方の誤りにしている。
func (r *runner) worktreeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worktree",
		Short: "task 内の worktree を操作する",
		RunE:  requireSubcommand,
	}
	cmd.AddCommand(
		r.worktreeAddCommand(),
		r.worktreeRenameCommand(),
		r.worktreeLsCommand(),
		r.worktreeLintCommand(),
		r.worktreePullCommand(),
	)
	return cmd
}

// addTaskFlag registers the --task flag on cmd, storing the value into task, with task name completion.
//
// addTaskFlag は、worktree 系コマンドに共通の --task フラグを登録する。値は task が指す変数に入る。
// RegisterFlagCompletionFunc(フラグ名, 補完関数) で、そのフラグの値を Tab 補完できるようにする(task 名の候補。completion.go 参照)。
// 登録に失敗したときのエラー(フラグが存在しないときなど)は、ここでは起こらないので、
// `_ =` で戻り値を捨てている(Go では、使わない戻り値を明示的に捨てるときにこう書く)。
// r.completeTasks は、メソッドを値として渡している。
func (r *runner) addTaskFlag(cmd *cobra.Command, task *string) {
	cmd.Flags().StringVar(task, "task", "", taskFlagUsage)
	_ = cmd.RegisterFlagCompletionFunc("task", r.completeTasks)
}

// worktreeAddCommand defines `ws worktree add`.
//
// worktreeAddCommand は ws worktree add を定義する。task の中に worktree を作る。作り方は 3 通り。
//   - 既存のブランチをチェックアウトする(--repo と --branch)
//   - 新しいブランチを作る(-c / --create と --base)
//   - 参照専用の readonly worktree を作る(--readonly)
//
// git は 1 つのブランチを複数の worktree で同時にチェックアウトできないが、--readonly なら detached(ブランチに属さない状態)で
// origin/<branch> を見るだけなので、main のように複数の task から参照するブランチでも使える。
// 引数が足りなければ対話で尋ねる(runAdd 参照)。
func (r *runner) worktreeAddCommand() *cobra.Command {
	// 全フラグの値を、app.AddOptions 構造体のフィールドにそのまま入れる。
	// `var opts app.AddOptions` は、すべてのフィールドがゼロ値(文字列は "" で bool は false)の状態で宣言する。
	var opts app.AddOptions
	cmd := &cobra.Command{
		Use:   "add",
		Short: "worktree を作る(引数が足りなければ対話で尋ねる)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// opts は値渡しで runAdd に渡る。runAdd の中で書き換えても、ここには影響しない。
			return r.runAdd(cmd, opts)
		},
	}
	r.addTaskFlag(cmd, &opts.Task)
	cmd.Flags().StringVar(&opts.Repo, "repo", "", "repos/ にある bare repo の名前")
	cmd.Flags().StringVar(&opts.Branch, "branch", "", "ブランチ名")
	cmd.Flags().StringVar(&opts.Base, "base", "", "--create の起点(origin/main など)")
	// BoolVarP は、短縮形(-c)も付けられる BoolVar。
	cmd.Flags().BoolVarP(&opts.Create, "create", "c", false, "新規ブランチを作る")
	cmd.Flags().BoolVar(&opts.ReadOnly, "readonly", false, "origin/<branch> を detached で _readonly/ に作る(main など、複数の task から参照するブランチ用)")
	// 補完関数の登録。completeAddBranch / completeBase は、補完時に opts.Repo を読むために &opts(ポインタ)を受け取る。
	// フラグの解析後に opts に入る値を、補完の時点で参照できる。
	_ = cmd.RegisterFlagCompletionFunc("repo", r.completeRepos)
	_ = cmd.RegisterFlagCompletionFunc("branch", r.completeAddBranch(&opts))
	_ = cmd.RegisterFlagCompletionFunc("base", r.completeBase(&opts))
	return cmd
}

// resolveTaskForPrompt resolves the task from flags or the current directory.
// When it cannot and a prompt is possible, it returns an empty task so that the prompter asks for it.
//
// resolveTaskForPrompt は、対象の task を決める。--task があればそれ、無ければカレントディレクトリが属する task を使う。
// task の外にいて決められないときは、対話できるなら「あとで Prompter に尋ねさせる」ために、
// 空の task と ask = true を返す。対話できないなら、決められなかったエラーをそのまま返す。
//
// 戻り値は (resolved, ask, err) の 3 つで、名前が付いている。これは「名前付き戻り値」で、
// 関数の先頭で、その名前の変数がゼロ値で宣言される(resolved は ""、ask は false、err は nil)。
// 名前はドキュメント代わりにもなり、どの値が何かが分かりやすくなる。
func (r *runner) resolveTaskForPrompt(task string) (resolved string, ask bool, err error) {
	// 名前付き戻り値の変数には、`:=` ではなく `=` で代入する(すでに宣言済みなので)。
	resolved, err = r.app.ResolveTask(task)
	// *app.OutsideTaskError は「どの task の中でもない場所にいる」ことを表すエラー型。
	// 対話できるなら、このエラーは握りつぶして、task を尋ねる流れに切り替える。
	if _, outside := errors.AsType[*app.OutsideTaskError](err); outside && r.interactive() {
		return "", true, nil
	}
	return resolved, false, err
}

// resolveTaskOrChoose resolves the task, and lets the user pick one when the current directory is outside any task and a prompt is possible.
//
// resolveTaskOrChoose は、task を決めて返す。task の外にいて、対話できるなら、一覧から選ばせる。
// ws worktree ls / lint / pull や ws describe、ws open のように、「task が決まりさえすればよい」コマンドで使う。
// 選ばれた名前は、r.app.ResolveTask で改めて検証してから返す。
func (r *runner) resolveTaskOrChoose(cmd *cobra.Command, task string) (string, error) {
	resolved, ask, err := r.resolveTaskForPrompt(task)
	// err があるか、尋ねる必要がなければ、決まった値(またはエラー)をそのまま返す。
	if err != nil || !ask {
		return resolved, err
	}
	chosen, err := r.prompter().ChooseTask(cmd.Context())
	if err != nil {
		return "", err
	}
	return r.app.ResolveTask(chosen)
}

// runAdd validates the flags of ws worktree add, asks for missing values when interactive, and creates the worktree.
//
// runAdd は ws worktree add の本体。流れは次のとおり。
//  1. フラグの組み合わせを検証する(--base は --create と一緒、--readonly は --create / --base と併用不可)
//  2. 足りないフラグがあるのに対話できなければ、使い方の誤りにする
//  3. task を決める(task の外なら、対話で選ばせる)
//  4. 足りない分があれば Prompter に尋ねる(指定済みの値は保たれる)
//  5. すべて揃った opts で worktree を作って、できたパスを表示する
//
// opts は値渡し(コピー)なので、この関数の中で書き換えても呼び出し元の opts には影響しない。
func (r *runner) runAdd(cmd *cobra.Command, opts app.AddOptions) error {
	if opts.Base != "" && !opts.Create {
		return usagef("--base は --create と一緒に指定してください")
	}
	if opts.ReadOnly && (opts.Create || opts.Base != "") {
		return usagef("--readonly は --create / --base と併用できません")
	}
	// 必要なフラグが揃っているかどうか。--readonly でも --repo と --branch は必要で、--create のときだけ --base も必要。
	missingFlags := opts.Repo == "" || opts.Branch == "" || (opts.Create && opts.Base == "")
	if missingFlags && !r.interactive() {
		return usagef("--repo と --branch が必要です(--create には --base も必要です)")
	}
	task, askTask, err := r.resolveTaskForPrompt(opts.Task)
	if err != nil {
		return err
	}
	opts.Task = task
	if missingFlags || askTask {
		// `if 代入; 条件` の形。opts を、Prompter が埋めた結果で上書きする。
		// ここの `=` は、すでにある変数 opts と err への代入(:= だと新しい変数になってしまう)。
		if opts, err = r.prompter().Add(cmd.Context(), opts); err != nil {
			return err
		}
	}
	path, err := r.app.Add(cmd.Context(), opts)
	if err != nil {
		return err
	}
	r.printf("作成しました: %s\n", path)
	return nil
}

// worktreeRenameCommand defines `ws worktree rename`.
//
// worktreeRenameCommand は ws worktree rename を定義する。作業用 worktree のブランチ名を変える。
// ブランチ名から決まるディレクトリ名(slug)が変わるときは、worktree のディレクトリも移動する。
// ローカルだけの操作で、リモートには触れない。引数が足りなければ対話で尋ねる(runRename 参照)。
func (r *runner) worktreeRenameCommand() *cobra.Command {
	var opts app.RenameOptions
	cmd := &cobra.Command{
		Use:   "rename",
		Short: "作業用 worktree のブランチ名を変える(slug が変わればディレクトリも移動する。引数が足りなければ対話で尋ねる)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return r.runRename(cmd, opts)
		},
	}
	r.addTaskFlag(cmd, &opts.Task)
	cmd.Flags().StringVar(&opts.Repo, "repo", "", "repos/ にある bare repo の名前")
	cmd.Flags().StringVar(&opts.Branch, "branch", "", "現在のブランチ名")
	cmd.Flags().StringVar(&opts.To, "to", "", "新しいブランチ名")
	_ = cmd.RegisterFlagCompletionFunc("repo", r.completeRepos)
	_ = cmd.RegisterFlagCompletionFunc("branch", r.completeRenameBranch(&opts))
	return cmd
}

// runRename validates the flags of ws worktree rename, asks for missing values when interactive, and renames the branch.
//
// runRename は ws worktree rename の本体。runAdd と同じ流れ
// (足りないフラグを確認、task を決める、必要なら対話で尋ねる、実行して結果を表示する)。
func (r *runner) runRename(cmd *cobra.Command, opts app.RenameOptions) error {
	missingFlags := opts.Repo == "" || opts.Branch == "" || opts.To == ""
	if missingFlags && !r.interactive() {
		return usagef("--repo、--branch、--to が必要です")
	}
	task, askTask, err := r.resolveTaskForPrompt(opts.Task)
	if err != nil {
		return err
	}
	opts.Task = task
	if missingFlags || askTask {
		if opts, err = r.prompter().Rename(cmd.Context(), opts); err != nil {
			return err
		}
	}
	result, err := r.app.Rename(cmd.Context(), opts)
	if err != nil {
		return err
	}
	r.printRename(result)
	return nil
}

// worktreeLsCommand defines `ws worktree ls`.
//
// worktreeLsCommand は ws worktree ls を定義する。task 内の worktree を一覧表示する(種類、名前、HEAD、clean / dirty)。
// --task を省略して task の外にいる場合は、対話できるなら task を選ばせる。
func (r *runner) worktreeLsCommand() *cobra.Command {
	var task string
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "task 内の worktree の一覧",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := r.resolveTaskOrChoose(cmd, task)
			if err != nil {
				return err
			}
			entries, err := r.app.ListWorktrees(cmd.Context(), resolved)
			if err != nil {
				return err
			}
			r.printWorktrees(entries)
			return nil
		},
	}
	r.addTaskFlag(cmd, &task)
	return cmd
}

// worktreeLintCommand defines `ws worktree lint`.
//
// worktreeLintCommand は ws worktree lint を定義する。ws lint の task 限定版で、指定した task の分だけを検査して報告する。
func (r *runner) worktreeLintCommand() *cobra.Command {
	var task string
	cmd := &cobra.Command{
		Use:   "lint",
		Short: "task 内の規約違反と壊れた worktree を検査する",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := r.resolveTaskOrChoose(cmd, task)
			if err != nil {
				return err
			}
			return r.reportIssues(r.app.LintTask(cmd.Context(), resolved))
		},
	}
	r.addTaskFlag(cmd, &task)
	return cmd
}

// worktreePullCommand defines `ws worktree pull`.
//
// worktreePullCommand は ws worktree pull を定義する。リモートを fetch して、task 内の各 worktree をリモートに追従させる。
// worktree ごとの結果は、1 件終わるたびに printPullEntry で表示される。
// 1 つでも失敗した worktree があれば、終了コード 1 にする(他の worktree の処理は続ける)。
func (r *runner) worktreePullCommand() *cobra.Command {
	var task string
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "fetch して task 内の worktree を追従させる",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := r.resolveTaskOrChoose(cmd, task)
			if err != nil {
				return err
			}
			result, err := r.app.Pull(cmd.Context(), resolved, r.printPullEntry)
			if err != nil {
				return err
			}
			if result.Total == 0 {
				r.printf("worktree はありません\n")
			}
			// 表示は printPullEntry で済ませているので、メッセージを重ねず、終了コードだけを 1 にする。
			if result.Failed > 0 {
				return exitError{ExitFailure}
			}
			return nil
		},
	}
	r.addTaskFlag(cmd, &task)
	return cmd
}

// skillCommand defines `ws skill`, the parent of the skill subcommands.
//
// skillCommand は ws skill を定義する。AI エージェント(Claude Code や Codex)向けのスキルを扱う親コマンド。
// worktreeCommand と同じく、自分では何もせず、サブコマンド(install)を束ねる。
func (r *runner) skillCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "エージェント向けのスキルを扱う",
		RunE:  requireSubcommand,
	}
	cmd.AddCommand(r.skillInstallCommand())
	return cmd
}

// skillInstallCommand defines `ws skill install [claude|codex]...`.
//
// skillInstallCommand は ws skill install を定義する。ws に同梱したスキル(ws の使い方を説明したもの)を、
// ユーザー単位のスキルディレクトリ(claude なら ~/.claude/skills、codex なら ~/.agents/skills)に書き出す。
// すでにあれば上書きするので、ws を更新したときの更新にも使う。引数なしで対話できるなら、インストール先を選ばせる。
func (r *runner) skillInstallCommand() *cobra.Command {
	// ValidArgs は、位置引数として許される値の一覧(補完の候補にもなる)。
	// Args の OnlyValidArgs は「ValidArgs に含まれない値はエラーにする」という検証。この 2 つはセットで使う。
	return &cobra.Command{
		Use:       "install [claude|codex]...",
		Short:     "ws-task スキルをユーザー単位でインストール・更新する(claude: ~/.claude/skills、codex: ~/.agents/skills。引数なしなら対話で選ぶ)",
		Args:      cobra.OnlyValidArgs,
		ValidArgs: app.SkillTargets,
		RunE: func(cmd *cobra.Command, args []string) error {
			// slices.Values(args) → slices.Sorted で並べ替えて、slices.Compact で隣り合う重複を取り除く。
			// 並べ替えてから Compact するのは、Compact が「隣り合う」重複しか取らないため。
			// `ws skill install claude claude` のように重複して指定されても、1 回だけ処理できる。
			targets := slices.Compact(slices.Sorted(slices.Values(args)))
			if len(targets) == 0 {
				if !r.interactive() {
					return usagef("インストール先(%s)を指定してください", strings.Join(app.SkillTargets, " / "))
				}
				chosen, err := r.prompter().ChooseSkillTargets(cmd.Context())
				if err != nil {
					return err
				}
				targets = chosen
			}
			// 対話で何も選ばなかった場合。エラーではなく正常終了にする。
			if len(targets) == 0 {
				r.printf("何もインストールしませんでした\n")
				return nil
			}
			for _, target := range targets {
				installed, err := r.app.InstallSkills(target)
				if err != nil {
					return err
				}
				for _, dir := range installed {
					r.printf("インストールしました(%s): %s\n", target, dir)
				}
			}
			return nil
		},
	}
}

// containsFold reports whether text contains word, ignoring case.
//
// containsFold は、text に word が含まれるかを、大文字小文字を区別せずに調べる。
// 両方を小文字にそろえてから strings.Contains(部分文字列を含むか)で比べている。
// ws ls <語> の絞り込みで使う。
func containsFold(text, word string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(word))
}

// describeCommand defines `ws describe [<説明>]`.
//
// describeCommand は ws describe を定義する。task の 1 行の説明(description)を表示・設定する。
// 説明は tasks/<task>/.ws-task.json に保存され、AI エージェントが作業内容を記録して、後から検索するために使う。
//   - 説明を渡した場合: その内容を書き込む("" なら消す)
//   - 省略した場合: 対話できなければ現在の説明を表示するだけ。対話できれば編集画面を出す
func (r *runner) describeCommand() *cobra.Command {
	var task string
	cmd := &cobra.Command{
		Use:   "describe [<説明>]",
		Short: "task の 1 行の説明を書き込む(\"\" で消す)。説明を省略すると表示し、対話できるなら編集する",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := r.resolveTaskOrChoose(cmd, task)
			if err != nil {
				return err
			}
			if len(args) == 1 {
				return r.setDescription(resolved, args[0])
			}
			current, err := r.app.Description(resolved)
			if err != nil {
				return err
			}
			// 対話できない(エージェントやパイプ)ときは、表示だけ。説明が空なら何も出さない。
			if !r.interactive() {
				if current != "" {
					r.printf("%s\n", current)
				}
				return nil
			}
			edited, err := r.prompter().Describe(cmd.Context(), resolved, current)
			if err != nil {
				return err
			}
			return r.setDescription(resolved, edited)
		},
	}
	r.addTaskFlag(cmd, &task)
	return cmd
}

// setDescription stores the description of the task and reports what was stored.
//
// setDescription は task の description を保存して、結果を表示する。
// 保存された値(stored)が空なら、消したことを表示する。
// stored は、保存の際に整形(前後の空白の除去など)された後の値で、渡した description と違うことがある。
func (r *runner) setDescription(task, description string) error {
	stored, err := r.app.SetDescription(task, description)
	if err != nil {
		return err
	}
	if stored == "" {
		r.printf("task '%s' の説明を消しました\n", task)
	} else {
		r.printf("task '%s' の説明: %s\n", task, stored)
	}
	return nil
}

// pickTask lets the user choose one of tasks and prints its directory.
//
// pickTask は、ws ls の対話の部分。task の一覧から絞り込みで 1 つ選ばせて、その task のディレクトリを stdout に出す。
// 選択画面は stderr に描かれ、stdout にはディレクトリだけが出る。そのため、
// シェルで `cd "$(ws ls)"` のように、結果を cd に使える(README の wcd 参照)。
// 該当する task が 0 件のときは、選ばせずにエラーにする。
func (r *runner) pickTask(cmd *cobra.Command, tasks []app.TaskSummary) error {
	if len(tasks) == 0 {
		// errors.New は、固定のメッセージの error を作る。書式が要るときは fmt.Errorf。
		return errors.New("該当する task がありません")
	}
	// 選択肢の表示文字列(Label)は、printTasks と同じ整形(列をそろえた 1 行)を使う。
	rows := taskRows(tasks)
	// make([]T, n) は、長さ n で、全要素がゼロ値のスライスを作る。この後、添字で代入して埋める。
	choices := make([]prompt.Choice, len(tasks))
	for i, task := range tasks {
		// Label は画面に出す文字列、Value は選ばれたときに返す値(task 名)。
		choices[i] = prompt.Choice{Label: rows[i], Value: task.Name}
	}
	chosen, err := r.prompter().PickTask(cmd.Context(), choices)
	if err != nil {
		return err
	}
	r.printf("%s\n", r.app.TaskDir(chosen))
	return nil
}

// openCommand defines `ws open [<task>]`.
//
// openCommand は ws open を定義する。task のディレクトリを、エディタの Zed で開く。
// task を省略すると、カレントディレクトリが属する task を開く。task の外にいる場合は、対話できれば選ばせる。
func (r *runner) openCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "open [<task>]",
		Short: "task のディレクトリを zed で開く(省略するとカレントディレクトリの task。task の外なら対話で選ぶ)",
		Args:  cobra.MaximumNArgs(1),
		// ValidArgsFunction は、位置引数の補完候補を動的に返す関数を設定するフィールド。
		// 戻り値の ShellCompDirectiveNoFileComp は「ファイル名の補完をしないで」という指示。
		// task は 1 つしか取らないので、すでに 1 つ入力されていれば(len(args) > 0)、候補を出さない。
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return r.completeTasks(cmd, args, toComplete)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			task := ""
			if len(args) == 1 {
				task = args[0]
			}
			resolved, err := r.resolveTaskOrChoose(cmd, task)
			if err != nil {
				return err
			}
			return r.openTask(cmd, resolved)
		},
	}
}

// openTask opens the directory of the task in the editor.
//
// openTask は task のディレクトリをエディタで開き、開いたことを表示する。ws open と、ws new の最後で使う。
// エディタを開く処理は env.OpenEditor に差し替えられるようになっていて、nil なら Zed を使う。
// テストで本物の Zed を起動しないための仕組み。
func (r *runner) openTask(cmd *cobra.Command, task string) error {
	dir := r.app.TaskDir(task)
	// 関数も値なので、変数に入れて切り替えられる(TypeScript の関数と同じ)。
	// `open` という名前の変数は、後から呼び出すことで openInZed とテスト用の差し替えを同じ書き方で扱う。
	open := r.env.OpenEditor
	if open == nil {
		open = openInZed
	}
	if err := open(cmd.Context(), dir); err != nil {
		return err
	}
	r.printf("zed で開きました: %s\n", dir)
	return nil
}

// openInZed runs the `zed` command on dir.
//
// openInZed は zed コマンドを実行して、dir を開く。
// exec.CommandContext は外部コマンドを表すオブジェクトを作る(まだ実行しない)。
// ctx が中断されると、起動したプロセスも止まる。
// CombinedOutput でコマンドを実行して、stdout と stderr をまとめて受け取る。
// 起動に失敗したときは、その出力もエラーメッセージに含めて、原因が分かるようにする。
func openInZed(ctx context.Context, dir string) error {
	zed := exec.CommandContext(ctx, "zed", dir)
	if out, err := zed.CombinedOutput(); err != nil {
		// exec.ErrNotFound は、PATH に zed が見つからなかったときのエラー。
		// その場合は、インストールを促す分かりやすいメッセージにする。
		if errors.Is(err, exec.ErrNotFound) {
			return errors.New("zed が見つかりません。Zed の CLI(zed コマンド)をインストールしてください")
		}
		// %w は、元のエラーを包んで(ラップして)新しいエラーを作る書式指定子。
		// 後から errors.Is / errors.AsType で、包まれた元のエラーを調べられる。TypeScript の `new Error(msg, { cause: err })` に近い。
		// %s は out(コマンドの出力。[]byte だが %s で文字列として表示される)。
		return fmt.Errorf("zed を起動できません: %w\n%s", err, out)
	}
	return nil
}
