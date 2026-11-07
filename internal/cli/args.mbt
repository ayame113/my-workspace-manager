///|
// moon には外部ツールなしで moon.mod の version を埋め込む手段が無いので二重管理する。一致は tests/ で検査している
let version = "0.1.0"

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
  version~,
  subcommand_required=true,
  subcommands=[
    Command("new", about="tasks/<task>/ を作る", positionals=[
      PositionArg("task", num_args=ValueRange(lower=1, upper=1)),
    ]),
    Command("clone", about="bare repo を repos/ に用意する", positionals=[
      PositionArg(
        "url",
        about="リモートの URL",
        num_args=ValueRange(lower=1, upper=1),
      ),
      PositionArg(
        "repo",
        about="repo 名。省略すると URL から導出",
        num_args=ValueRange(lower=0, upper=1),
      ),
    ]),
    Command(
      "add",
      about="worktree を作る",
      options=[
        task_option(),
        OptionArg(
          "repo",
          about="repos/ にある bare repo の名前",
          required=true,
        ),
        OptionArg("branch", about="ブランチ名", required=true),
        OptionArg("base", about="--create の起点(origin/main など)", requires=[
          "create",
        ]),
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
            about="npm ci にそのまま渡す引数(指定するとキャッシュを使わない)",
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
      flags=[
        all_flag(),
        FlagArg("fix", about="自動で直せる問題を直す"),
      ],
    ),
  ],
)

///|
/// `ls` / `lint` の対象。
#derive(Debug)
priv enum Target {
  AllTasks
  /// `None` ならカレントディレクトリが属する task
  Task(String?)
}

///|
#derive(Debug)
priv enum Invocation {
  New(task~ : String)
  Clone(url~ : String, repo~ : String?)
  Add(
    task~ : String?,
    repo~ : String,
    branch~ : String,
    base~ : String?,
    read_only~ : Bool,
    install~ : Bool
  )
  Pull(task~ : String?)
  Ls(Target)
  NpmCi(npm_args~ : Array[String])
  Lint(Target, fix~ : Bool)
}

///|
// Debug はテストのスナップショット用
#derive(Debug)
#warnings("-unused_value")
priv enum ParseResult {
  Run(Invocation)
  /// help / version。stdout に出して正常終了する
  Print(String)
  /// 使い方の誤り。stderr に出して終了コード 2
  Usage(String)
}

///|
fn parse(argv : Array[String]) -> ParseResult {
  let matches = match @argparse.parse_outcome(command, argv~) {
    Parsed(matches) => matches
    Help(text) | Version(text) => return Print(text)
    Failed(error) => return Usage("\{error}")
  }
  // argparse は required/num_args を検証済みなので、ここで値が欠けることは無い
  guard matches.subcommand is Some((name, args)) else {
    return Usage(command.render_help())
  }
  let text = (key : String) => {
    args.values.get(key).bind(values => values.get(0))
  }
  let flag = (key : String) => args.flags.get(key).unwrap_or(false)
  let target = () => if flag("all") { AllTasks } else { Task(text("task")) }
  let invocation = match name {
    "new" => New(task=text("task").unwrap_or(""))
    "clone" => Clone(url=text("url").unwrap_or(""), repo=text("repo"))
    "add" =>
      Add(
        task=text("task"),
        repo=text("repo").unwrap_or(""),
        branch=text("branch").unwrap_or(""),
        base=text("base"),
        read_only=flag("readonly"),
        install=!flag("no-install"),
      )
    "pull" => Pull(task=text("task"))
    "ls" => Ls(target())
    "lint" => Lint(target(), fix=flag("fix"))
    "npm" if args.subcommand is Some((_, ci)) =>
      NpmCi(npm_args=ci.values.get("npm-args").unwrap_or([]))
    _ => return Usage(command.render_help())
  }
  Run(invocation)
}
