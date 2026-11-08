///|
async fn resolve_task(task : String?) -> String {
  @layout.resolve_existing_task(
    task, "--task <task> を指定してください",
  )
}

///|
async fn execute(invocation : Invocation) -> Int32 {
  match invocation {
    New(task~) => cmd_new(task)
    Clone(url~, repo~) => cmd_clone(url, repo)
    WorktreeAdd(task~, repo~, branch~, base~, read_only~) =>
      cmd_add(resolve_task(task), repo, branch, base~, read_only~)
    WorktreeRename(task~, repo~, branch~, to~) =>
      cmd_rename(resolve_task(task), repo, branch, to)
    WorktreePull(task~) => cmd_pull(task)
    WorktreeLs(task~) => cmd_worktree_ls(resolve_task(task))
    WorktreeLint(task~, fix~) => @lint.run_task(resolve_task(task), fix)
    Ls => cmd_ls()
    Compact(dry_run~) => @compact.run(dry_run~)
    Lint(fix~) => @lint.run_workspace(fix)
  }
}

///|
/// `ws` のコマンドライン(プログラム名を除く)を実行し、終了コードを返す。
/// 0: 成功(help / --version を含む)、1: 実行時の失敗(lint で未解決の問題がある場合を含む)、
/// 2: 使い方の誤り。
pub async fn run(argv : Array[String]) -> Int32 {
  match parse(argv) {
    Print(text) => {
      @sys.log(text)
      0
    }
    Usage(text) => {
      @sys.log_error(text)
      2
    }
    Run(invocation) =>
      execute(invocation) catch {
        @errors.GitError(stderr) => {
          @sys.eprint(stderr)
          1
        }
        @errors.WsError(message) => {
          @sys.log_error("ws: \{message}")
          1
        }
        error => {
          @sys.log_error("ws: 予期しないエラー: \{error}")
          1
        }
      }
  }
}
