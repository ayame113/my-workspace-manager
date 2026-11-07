///|
let usage =
  #|usage:
  #|  ws new <task>
  #|  ws clone <url> [<repo>]
  #|  ws add [--task <task>] --repo <repo> --branch <branch> [--no-install]
  #|  ws add [--task <task>] --repo <repo> --branch <branch> --create --base <start-point> [--no-install]
  #|  ws add [--task <task>] --repo <repo> --branch <branch> --readonly [--no-install]
  #|  ws pull [--task <task>]
  #|  ws ls [--task <task> | --all]
  #|  ws npm ci [-- <npm ci の引数>]
  #|  ws lint [--task <task> | --all] [--fix]
  #|
  #|--create の短縮形は -c
  #|--task を省略するとカレントディレクトリが属する task を使う(--task . も同じ)
  #|

///|
async fn dispatch(argv : Array[String]) -> Int32 {
  guard argv is [subcommand, .. rest] else { raise @errors.UsageError }
  let args = rest.to_owned()
  match subcommand {
    "--help" | "-h" => {
      @sys.write_stdout(usage)
      0
    }
    "new" => cmd_new(args)
    "clone" => cmd_clone(args)
    "add" => cmd_add(args)
    "pull" => cmd_pull(args)
    "ls" => cmd_ls(args)
    "npm" => cmd_npm(args)
    "lint" => cmd_lint(args)
    _ => raise @errors.UsageError
  }
}

///|
/// `ws` のコマンドライン `argv`(プログラム名を除く)を実行し、終了コードを返す。
pub async fn run(argv : Array[String]) -> Int32 {
  dispatch(argv) catch {
    @errors.UsageError => {
      @sys.write_stderr(usage)
      2
    }
    @errors.GitError(stderr) => {
      @sys.write_stderr(stderr)
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
