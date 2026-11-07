///|
fn task_option() -> @argparse.OptionArg {
  OptionArg(
    "task",
    about="対象の task。省略するとカレントディレクトリが属する task(. も同じ)",
  )
}

///|
fn all_flag() -> @argparse.FlagArg {
  FlagArg("all", about="全 task が対象", conflicts_with=["task"])
}

///|
let command : @argparse.Command = Command(
  "ws",
  about="git worktree をタスク単位で管理する",
  subcommand_required=true,
  subcommands=[
    Command("new", about="tasks/<task>/ を作る", positionals=[
      PositionArg("task", num_args=ValueRange(lower=1, upper=1)),
    ]),
    Command("clone", about="bare repo を repos/ に用意する", positionals=[
      PositionArg("url", num_args=ValueRange(lower=1, upper=1)),
      PositionArg("repo", num_args=ValueRange(lower=0, upper=1)),
    ]),
    Command(
      "add",
      about="worktree を作る",
      options=[
        task_option(),
        OptionArg("repo", required=true),
        OptionArg("branch", required=true),
        OptionArg("base", about="--create の起点", requires=["create"]),
      ],
      flags=[
        FlagArg("create", short='c', about="新規ブランチを作る", requires=[
          "base",
        ]),
        FlagArg(
          "readonly",
          about="origin/<branch> を detached で _readonly/ に作る",
          conflicts_with=["create", "base"],
        ),
        FlagArg("no-install", about="node_modules を用意しない"),
      ],
    ),
    Command(
      "pull",
      about="fetch して task 内の worktree を追従させる",
      options=[task_option()],
    ),
    Command("ls", about="worktree の一覧", options=[task_option()], flags=[
      all_flag(),
    ]),
    Command("npm", about="npm のラッパー", subcommand_required=true, subcommands=[
      Command(
        "ci",
        about="node_modules を共有キャッシュ経由で用意する",
        positionals=[
          PositionArg(
            "npm-args",
            num_args=ValueRange(lower=0),
            allow_hyphen_values=true,
          ),
        ],
      ),
    ]),
    Command(
      "lint",
      about="規約違反と node_modules の状態を検査する",
      options=[task_option()],
      flags=[all_flag(), FlagArg("fix")],
    ),
  ],
)

///|
/// `ws` のコマンドライン(プログラム名を除く)を実行し、終了コードを返す。使い方の誤りは 2。
pub async fn run(argv : Array[String]) -> Int32 {
  match @argparse.parse_outcome(command, argv~) {
    Parsed({ subcommand: Some((name, args)), .. }) =>
      dispatch(name, args) catch {
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
    Help(text) | Version(text) => {
      @sys.log(text)
      0
    }
    Failed(error) => {
      @sys.log_error("\{error}")
      2
    }
    Parsed(_) => 2
  }
}

///|
async fn dispatch(name : String, args : @argparse.Matches) -> Int32 {
  let value = (key : String) => args.values.get(key).map(values => values[0])
  let flag = (key : String) => args.flags.get(key).unwrap_or(false)
  let hint = "--task <task> か --all を指定してください"
  match name {
    "new" => cmd_new(value("task").unwrap())
    "clone" => cmd_clone(value("url").unwrap(), value("repo"))
    "add" =>
      cmd_add(
        @layout.resolve_existing_task(
          value("task"),
          "--task <task> を指定してください",
        ),
        value("repo").unwrap(),
        value("branch").unwrap(),
        base=value("base"),
        read_only=flag("readonly"),
        install=!flag("no-install"),
      )
    "pull" => cmd_pull(value("task"))
    "ls" =>
      cmd_ls(
        if flag("all") {
          None
        } else {
          Some(@layout.resolve_existing_task(value("task"), hint))
        },
      )
    "npm" => {
      guard! args.subcommand is Some((_, ci))
      cmd_npm_ci(ci.values.get("npm-args").unwrap_or([]))
    }
    "lint" =>
      @lint.run(
        if flag("all") {
          None
        } else {
          Some(@layout.resolve_existing_task(value("task"), hint))
        },
        flag("fix"),
      )
    _ => 2
  }
}
