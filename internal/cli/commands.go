package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/ayame113/my-workspace-manager/internal/app"
)

const taskFlagUsage = "対象の task。省略するとカレントディレクトリが属する task(. も同じ)"

func (r *runner) rootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:               "ws",
		Short:             "git worktree をタスク単位で管理する",
		Version:           version(),
		SilenceUsage:      true,
		SilenceErrors:     true,
		PersistentPreRunE: r.beforeRun,
		RunE:              requireSubcommand,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
	}
	root.Flags().BoolP("version", "V", false, "バージョンを表示する")
	root.SetVersionTemplate("{{.Version}}\n")
	root.AddCommand(
		r.newCommand(),
		r.cloneCommand(),
		r.lsCommand(),
		r.lintCommand(),
		r.compactCommand(),
		r.worktreeCommand(),
	)
	return root
}

func requireSubcommand(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return usagef("不明なサブコマンドです: %s", args[0])
	}
	return usagef("サブコマンドを指定してください")
}

// beforeRun runs after cobra has parsed arguments, so anything cobra rejects earlier is a usage error.
func (r *runner) beforeRun(cmd *cobra.Command, args []string) error {
	r.started = true
	var err error
	cmd.Flags().Visit(func(f *pflag.Flag) {
		// 値を取るフラグが次のフラグを値として食べた場合(--task --fix など)を使い方の誤りにする
		if f.Value.Type() == "string" && strings.HasPrefix(f.Value.String(), "-") && err == nil {
			err = usagef("--%s に値が必要です(受け取った値: %s)", f.Name, f.Value.String())
		}
	})
	return err
}

func (r *runner) printf(format string, args ...any) {
	fmt.Fprintf(r.env.Stdout, format, args...)
}

func (r *runner) newCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "new <task>",
		Short: "tasks/<task>/ を作る",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := r.app.NewTask(args[0])
			if err != nil {
				return err
			}
			if result.Created {
				r.printf("作成しました: %s\n", result.Dir)
			} else {
				r.printf("task '%s' は既に存在します: %s\n", args[0], result.Dir)
			}
			return nil
		},
	}
}

func (r *runner) cloneCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "clone <url> [<repo>]",
		Short: "bare repo を repos/ に用意する(repo 名は省略すると URL から導出)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo := ""
			if len(args) == 2 {
				repo = args[1]
			}
			bare, err := r.app.Clone(cmd.Context(), args[0], repo)
			if err != nil {
				return err
			}
			r.printf("作成しました: %s\n", bare)
			return nil
		},
	}
}

func (r *runner) lsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "全 task の一覧(1 task 1 行)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			tasks, err := r.app.ListTasks(cmd.Context())
			if err != nil {
				return err
			}
			r.printTasks(tasks)
			return nil
		},
	}
}

func (r *runner) lintCommand() *cobra.Command {
	var fix bool
	cmd := &cobra.Command{
		Use:   "lint",
		Short: "workspace 全体の規約違反と壊れた worktree を検査する",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			summary, err := r.app.LintWorkspace(cmd.Context(), fix, r.printIssue(fix))
			return r.finishLint(summary, fix, err)
		},
	}
	cmd.Flags().BoolVar(&fix, "fix", false, "自動で直せる問題を直す")
	return cmd
}

func (r *runner) compactCommand() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "compact",
		Short: "全 task の node_modules 内の同一ファイルを clonefile で共有し、容量を減らす",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := r.app.Compact(cmd.Context(), dryRun, r.printCompactEvent)
			if err != nil {
				return err
			}
			r.printCompactResult(result)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "置き換えずに、対象件数と削減見込みだけ表示する")
	return cmd
}

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

func addTaskFlag(cmd *cobra.Command, task *string) {
	cmd.Flags().StringVar(task, "task", "", taskFlagUsage)
}

func (r *runner) worktreeAddCommand() *cobra.Command {
	var opts app.AddOptions
	cmd := &cobra.Command{
		Use:   "add",
		Short: "worktree を作る(引数が足りなければ対話で尋ねる)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return r.runAdd(cmd, opts)
		},
	}
	addTaskFlag(cmd, &opts.Task)
	cmd.Flags().StringVar(&opts.Repo, "repo", "", "repos/ にある bare repo の名前")
	cmd.Flags().StringVar(&opts.Branch, "branch", "", "ブランチ名")
	cmd.Flags().StringVar(&opts.Base, "base", "", "--create の起点(origin/main など)")
	cmd.Flags().BoolVarP(&opts.Create, "create", "c", false, "新規ブランチを作る")
	cmd.Flags().BoolVar(&opts.ReadOnly, "readonly", false, "origin/<branch> を detached で _readonly/ に作る")
	return cmd
}

// resolveTaskForPrompt resolves the task from flags or the current directory.
// When it cannot and a prompt is possible, it returns an empty task so that the prompter asks for it.
func (r *runner) resolveTaskForPrompt(task string) (resolved string, ask bool, err error) {
	resolved, err = r.app.ResolveTask(task)
	if _, outside := errors.AsType[*app.OutsideTaskError](err); outside && r.interactive() {
		return "", true, nil
	}
	return resolved, false, err
}

func (r *runner) runAdd(cmd *cobra.Command, opts app.AddOptions) error {
	if opts.Base != "" && !opts.Create {
		return usagef("--base は --create と一緒に指定してください")
	}
	if opts.ReadOnly && (opts.Create || opts.Base != "") {
		return usagef("--readonly は --create / --base と併用できません")
	}
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
	addTaskFlag(cmd, &opts.Task)
	cmd.Flags().StringVar(&opts.Repo, "repo", "", "repos/ にある bare repo の名前")
	cmd.Flags().StringVar(&opts.Branch, "branch", "", "現在のブランチ名")
	cmd.Flags().StringVar(&opts.To, "to", "", "新しいブランチ名")
	return cmd
}

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

func (r *runner) worktreeLsCommand() *cobra.Command {
	var task string
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "task 内の worktree の一覧",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := r.app.ResolveTask(task)
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
	addTaskFlag(cmd, &task)
	return cmd
}

func (r *runner) worktreeLintCommand() *cobra.Command {
	var task string
	var fix bool
	cmd := &cobra.Command{
		Use:   "lint",
		Short: "task 内の規約違反と壊れた worktree を検査する",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := r.app.ResolveTask(task)
			if err != nil {
				return err
			}
			summary, err := r.app.LintTask(cmd.Context(), resolved, fix, r.printIssue(fix))
			return r.finishLint(summary, fix, err)
		},
	}
	addTaskFlag(cmd, &task)
	cmd.Flags().BoolVar(&fix, "fix", false, "自動で直せる問題を直す")
	return cmd
}

func (r *runner) worktreePullCommand() *cobra.Command {
	var task string
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "fetch して task 内の worktree を追従させる",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := r.app.Pull(cmd.Context(), task, r.printPullEntry)
			if err != nil {
				return err
			}
			if result.Total == 0 {
				r.printf("worktree はありません\n")
			}
			if result.Failed > 0 {
				return exitError{ExitFailure}
			}
			return nil
		},
	}
	addTaskFlag(cmd, &task)
	return cmd
}
