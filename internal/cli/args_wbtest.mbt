///|
test "parse typed invocations" {
  @debug.debug_inspect(
    parse(["add", "--branch", "b", "--repo", "r", "-c", "--base", "origin/main"]),
    content=(
      #|Run(
      #|  Add(
      #|    task=None,
      #|    repo="r",
      #|    branch="b",
      #|    base=Some("origin/main"),
      #|    read_only=false,
      #|    install=true,
      #|  ),
      #|)
    ),
  )
  @debug.debug_inspect(
    parse([
      "add", "--task", "t", "--repo", "r", "--branch", "b", "--readonly", "--no-install",
    ]),
    content=(
      #|Run(
      #|  Add(
      #|    task=Some("t"),
      #|    repo="r",
      #|    branch="b",
      #|    base=None,
      #|    read_only=true,
      #|    install=false,
      #|  ),
      #|)
    ),
  )
  @debug.debug_inspect(
    parse(["clone", "file:///x/r.git"]),
    content=(
      #|Run(Clone(url="file:///x/r.git", repo=None))
    ),
  )
  @debug.debug_inspect(
    parse(["ls", "--all"]),
    content=(
      #|Run(Ls(AllTasks))
    ),
  )
  @debug.debug_inspect(
    parse(["lint", "--task", ".", "--fix"]),
    content=(
      #|Run(Lint(Task(Some(".")), fix=true))
    ),
  )
  @debug.debug_inspect(
    parse(["npm", "ci", "--", "--omit=dev"]),
    content=(
      #|Run(NpmCi(npm_args=["--omit=dev"]))
    ),
  )
}

///|
fn first_line(result : ParseResult) -> String {
  match result {
    Usage(text) => "usage: " + text.split("\n").next().unwrap().to_owned()
    Print(text) => "print: " + text.split("\n").next().unwrap().to_owned()
    Run(_) => "run"
  }
}

///|
test "usage errors" {
  inspect(
    [
      [],
      ["bogus"],
      ["new"],
      ["add", "--branch", "b"],
      ["add", "--repo", "r", "--branch", "b", "-c"],
      ["add", "--repo", "r", "--branch", "b", "--base", "origin/main"],
      ["add", "--repo", "r", "--branch", "b", "--readonly", "-c", "--base", "o"],
      ["ls", "--all", "--task", "t"],
      ["lint", "--task", "--fix"],
      ["npm"],
    ]
    .map(argv => first_line(parse(argv)))
    .join("\n"),
    content=(
      #|usage: error: the following required argument was not provided: 'subcommand'
      #|usage: error: unexpected value 'bogus' found; no more were expected
      #|usage: error: 'task' requires at least 1 values but only 0 were provided
      #|usage: error: the following required argument was not provided: 'repo'
      #|usage: error: the following required argument was not provided: 'base' (required by 'create')
      #|usage: error: the following required argument was not provided: 'create' (required by 'base')
      #|usage: error: conflicting arguments: readonly and create
      #|usage: error: conflicting arguments: all and task
      #|usage: error: a value is required for '--task' but none was supplied
      #|usage: error: the following required argument was not provided: 'subcommand'
    ),
  )
  inspect(
    first_line(parse(["--version"])),
    content=(
      #|print: 0.1.0
    ),
  )
}

///|
test "help" {
  let texts = ["add", "ls", "lint", "npm"].map(subcommand => {
    match parse([subcommand, "--help"]) {
      Print(text) => text
      _ => "no help for \{subcommand}"
    }
  })
  inspect(
    [command.render_help(), ..texts].join("\n----\n"),
    content=(
      #|Usage: ws <command>
      #|
      #|git worktree をタスク単位で管理する
      #|
      #|Commands:
      #|  new    tasks/<task>/ を作る
      #|  clone  bare repo を repos/ に用意する
      #|  add    worktree を作る
      #|  pull   fetch して task 内の worktree を追従させる
      #|  ls     worktree の一覧
      #|  npm    npm のラッパー
      #|  lint   規約違反と node_modules の状態を検査する
      #|  help   Print help for the subcommand(s).
      #|
      #|Options:
      #|  -h, --help     Show help information.
      #|  -V, --version  Show version information.
      #|
      #|----
      #|Usage: ws add --repo <repo> --branch <branch> [options]
      #|
      #|worktree を作る
      #|
      #|Options:
      #|  -h, --help         Show help information.
      #|  -c, --create       新規ブランチを作る
      #|  --readonly         origin/<branch> を detached で _readonly/ に作る
      #|  --no-install       node_modules を用意しない
      #|  --task <task>      対象の task。省略するとカレントディレクトリが属する task(. も同じ)
      #|  --repo <repo>      repos/ にある bare repo の名前
      #|  --branch <branch>  ブランチ名
      #|  --base <base>      --create の起点(origin/main など)
      #|----
      #|Usage: ws ls [options]
      #|
      #|worktree の一覧
      #|
      #|Options:
      #|  -h, --help     Show help information.
      #|  --all          全 task が対象
      #|  --task <task>  対象の task。省略するとカレントディレクトリが属する task(. も同じ)
      #|----
      #|Usage: ws lint [options]
      #|
      #|規約違反と node_modules の状態を検査する
      #|
      #|Options:
      #|  -h, --help     Show help information.
      #|  --all          全 task が対象
      #|  --fix          自動で直せる問題を直す
      #|  --task <task>  対象の task。省略するとカレントディレクトリが属する task(. も同じ)
      #|----
      #|Usage: ws npm <command>
      #|
      #|npm のラッパー
      #|
      #|Commands:
      #|  ci    node_modules を共有キャッシュ経由で用意する
      #|  help  Print help for the subcommand(s).
      #|
      #|Options:
      #|  -h, --help  Show help information.
    ),
  )
}
