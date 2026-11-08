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

///|
let worktree_command : @argparse.Command = Command(
  "worktree",
  about="task 内の worktree を操作する",
  subcommand_required=true,
  subcommands=[
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
      ],
    ),
    Command(
      "rename",
      about="作業用 worktree のブランチ名を変える(slug が変わればディレクトリも移動する)",
      options=[
        task_option(),
        OptionArg(
          "repo",
          about="repos/ にある bare repo の名前",
          required=true,
        ),
        OptionArg("branch", about="現在のブランチ名", required=true),
        OptionArg("to", about="新しいブランチ名", required=true),
      ],
    ),
    Command("ls", about="task 内の worktree の一覧", options=[
      task_option(),
    ]),
    Command(
      "lint",
      about="task 内の規約違反と壊れた worktree を検査する",
      options=[task_option()],
      flags=[FlagArg("fix", about="自動で直せる問題を直す")],
    ),
    Command(
      "pull",
      about="fetch して task 内の worktree を追従させる",
      options=[task_option()],
    ),
  ],
)

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
    Command("ls", about="全 task の一覧(1 task 1 行)"),
    Command(
      "compact",
      about="全 task の node_modules 内の同一ファイルを clonefile で共有し、容量を減らす",
      flags=[
        FlagArg(
          "dry-run",
          about="置き換えずに、対象件数と削減見込みだけ表示する",
        ),
      ],
    ),
    Command(
      "lint",
      about="workspace 全体の規約違反と壊れた worktree を検査する",
      flags=[FlagArg("fix", about="自動で直せる問題を直す")],
    ),
    worktree_command,
  ],
)

///|
#derive(Debug)
priv enum Invocation {
  New(task~ : String)
  Clone(url~ : String, repo~ : String?)
  WorktreeAdd(
    task~ : String?,
    repo~ : String,
    branch~ : String,
    base~ : String?,
    read_only~ : Bool
  )
  WorktreeRename(
    task~ : String?,
    repo~ : String,
    branch~ : String,
    to~ : String
  )
  WorktreePull(task~ : String?)
  WorktreeLs(task~ : String?)
  WorktreeLint(task~ : String?, fix~ : Bool)
  Ls
  Compact(dry_run~ : Bool)
  Lint(fix~ : Bool)
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
  let invocation = match name {
    "worktree" => {
      guard args.subcommand is Some((worktree_name, worktree_args)) else {
        return Usage(worktree_command.render_help())
      }
      match parse_worktree(worktree_name, worktree_args) {
        Some(invocation) => invocation
        None => return Usage(worktree_command.render_help())
      }
    }
    "new" => New(task=text(args, "task").unwrap_or(""))
    "clone" =>
      Clone(url=text(args, "url").unwrap_or(""), repo=text(args, "repo"))
    "ls" => Ls
    "lint" => Lint(fix=flag(args, "fix"))
    "compact" => Compact(dry_run=flag(args, "dry-run"))
    _ => return Usage(command.render_help())
  }
  Run(invocation)
}

///|
fn text(args : @argparse.Matches, key : String) -> String? {
  args.values.get(key).bind(values => values.get(0))
}

///|
fn flag(args : @argparse.Matches, key : String) -> Bool {
  args.flags.get(key).unwrap_or(false)
}

///|
fn parse_worktree(name : String, args : @argparse.Matches) -> Invocation? {
  let invocation = match name {
    "add" =>
      WorktreeAdd(
        task=text(args, "task"),
        repo=text(args, "repo").unwrap_or(""),
        branch=text(args, "branch").unwrap_or(""),
        base=text(args, "base"),
        read_only=flag(args, "readonly"),
      )
    "rename" =>
      WorktreeRename(
        task=text(args, "task"),
        repo=text(args, "repo").unwrap_or(""),
        branch=text(args, "branch").unwrap_or(""),
        to=text(args, "to").unwrap_or(""),
      )
    "ls" => WorktreeLs(task=text(args, "task"))
    "lint" => WorktreeLint(task=text(args, "task"), fix=flag(args, "fix"))
    "pull" => WorktreePull(task=text(args, "task"))
    _ => return None
  }
  Some(invocation)
}
