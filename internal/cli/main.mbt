///|
async fn resolve(target : Target) -> String? {
  match target {
    AllTasks => None
    Task(task) =>
      Some(
        @layout.resolve_existing_task(
          task, "--task <task> か --all を指定してください",
        ),
      )
  }
}

///|
async fn execute(invocation : Invocation) -> Int32 {
  match invocation {
    New(task~) => cmd_new(task)
    Clone(url~, repo~) => cmd_clone(url, repo)
    Add(task~, repo~, branch~, base~, read_only~) =>
      cmd_add(
        @layout.resolve_existing_task(
          task, "--task <task> を指定してください",
        ),
        repo,
        branch,
        base~,
        read_only~,
      )
    Rename(task~, repo~, branch~, to~) =>
      cmd_rename(
        @layout.resolve_existing_task(
          task, "--task <task> を指定してください",
        ),
        repo,
        branch,
        to,
      )
    Pull(task~) => cmd_pull(task)
    Ls(target) => cmd_ls(resolve(target))
    Compact(dry_run~) => @compact.run(dry_run~)
    Lint(target, fix~) => @lint.run(resolve(target), fix)
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
