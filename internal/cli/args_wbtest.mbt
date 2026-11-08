///|
test "parse typed invocations" {
  @debug.debug_inspect(
    parse([
      "worktree", "add", "--branch", "b", "--repo", "r", "-c", "--base", "origin/main",
    ]),
    content=(
      #|Run(
      #|  WorktreeAdd(
      #|    task=None,
      #|    repo="r",
      #|    branch="b",
      #|    base=Some("origin/main"),
      #|    read_only=false,
      #|  ),
      #|)
    ),
  )
  @debug.debug_inspect(
    parse([
      "worktree", "add", "--task", "t", "--repo", "r", "--branch", "b", "--readonly",
    ]),
    content=(
      #|Run(
      #|  WorktreeAdd(
      #|    task=Some("t"),
      #|    repo="r",
      #|    branch="b",
      #|    base=None,
      #|    read_only=true,
      #|  ),
      #|)
    ),
  )
  @debug.debug_inspect(
    parse([
      "worktree", "rename", "--repo", "r", "--branch", "old", "--to", "new",
    ]),
    content=(
      #|Run(WorktreeRename(task=None, repo="r", branch="old", to="new"))
    ),
  )
  @debug.debug_inspect(
    parse([
      "worktree", "rename", "--to", "b/c", "--task", "t", "--branch", "a", "--repo",
      "r",
    ]),
    content=(
      #|Run(WorktreeRename(task=Some("t"), repo="r", branch="a", to="b/c"))
    ),
  )
  @debug.debug_inspect(
    parse(["clone", "file:///x/r.git"]),
    content=(
      #|Run(Clone(url="file:///x/r.git", repo=None))
    ),
  )
  @debug.debug_inspect(
    parse(["ls"]),
    content=(
      #|Run(Ls)
    ),
  )
  @debug.debug_inspect(
    parse(["lint", "--fix"]),
    content=(
      #|Run(Lint(fix=true))
    ),
  )
  @debug.debug_inspect(
    parse(["worktree", "ls", "--task", "t"]),
    content=(
      #|Run(WorktreeLs(task=Some("t")))
    ),
  )
  @debug.debug_inspect(
    parse(["worktree", "lint", "--task", ".", "--fix"]),
    content=(
      #|Run(WorktreeLint(task=Some("."), fix=true))
    ),
  )
  @debug.debug_inspect(
    parse(["worktree", "pull"]),
    content=(
      #|Run(WorktreePull(task=None))
    ),
  )
  @debug.debug_inspect(
    parse(["compact", "--dry-run"]),
    content=(
      #|Run(Compact(dry_run=true))
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
      ["worktree", "add", "--branch", "b"],
      ["worktree", "add", "--repo", "r", "--branch", "b", "-c"],
      [
        "worktree", "add", "--repo", "r", "--branch", "b", "--base", "origin/main",
      ],
      [
        "worktree", "add", "--repo", "r", "--branch", "b", "--readonly", "-c", "--base",
        "o",
      ],
      ["worktree", "rename", "--repo", "r", "--branch", "a"],
      ["worktree", "rename", "--repo", "r", "--to", "b"],
      ["worktree", "rename", "--branch", "a", "--to", "b"],
      [
        "worktree", "rename", "--repo", "r", "--branch", "a", "--to", "b", "extra",
      ],
      ["ls", "--all"],
      ["lint", "--task", "t"],
      ["worktree"],
      ["worktree", "bogus"],
      ["add", "--repo", "r", "--branch", "b"],
      ["worktree", "ls", "--all"],
      ["worktree", "lint", "--task", "--fix"],
      ["compact", "--task", "t"],
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
      #|usage: error: the following required argument was not provided: 'to'
      #|usage: error: the following required argument was not provided: 'branch'
      #|usage: error: the following required argument was not provided: 'repo'
      #|usage: error: unexpected value 'extra' found; no more were expected
      #|usage: error: unexpected argument '--all' found
      #|usage: error: unexpected argument '--task' found
      #|usage: error: the following required argument was not provided: 'subcommand'
      #|usage: error: unexpected value 'bogus' found; no more were expected
      #|usage: error: unexpected argument '--repo' found
      #|usage: error: unexpected argument '--all' found
      #|usage: error: a value is required for '--task' but none was supplied
      #|usage: error: unexpected argument '--task' found
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
  let texts = [
    ["worktree"],
    ["worktree", "add"],
    ["worktree", "rename"],
    ["worktree", "ls"],
    ["worktree", "lint"],
    ["worktree", "pull"],
    ["ls"],
    ["lint"],
    ["compact"],
  ].map(subcommand => {
    match parse([..subcommand, "--help"]) {
      Print(text) => text
      _ => "no help for \{subcommand.join(" ")}"
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
      #|  new       tasks/<task>/ を作る
      #|  clone     bare repo を repos/ に用意する
      #|  ls        全 task の一覧(1 task 1 行)
      #|  compact   全 task の node_modules 内の同一ファイルを clonefile で共有し、容量を減らす
      #|  lint      workspace 全体の規約違反と壊れた worktree を検査する
      #|  worktree  task 内の worktree を操作する
      #|  help      Print help for the subcommand(s).
      #|
      #|Options:
      #|  -h, --help     Show help information.
      #|  -V, --version  Show version information.
      #|
      #|----
      #|Usage: ws worktree <command>
      #|
      #|task 内の worktree を操作する
      #|
      #|Commands:
      #|  add     worktree を作る
      #|  rename  作業用 worktree のブランチ名を変える(slug が変わればディレクトリも移動する)
      #|  ls      task 内の worktree の一覧
      #|  lint    task 内の規約違反と壊れた worktree を検査する
      #|  pull    fetch して task 内の worktree を追従させる
      #|  help    Print help for the subcommand(s).
      #|
      #|Options:
      #|  -h, --help  Show help information.
      #|----
      #|Usage: ws worktree add --repo <repo> --branch <branch> [options]
      #|
      #|worktree を作る
      #|
      #|Options:
      #|  -h, --help         Show help information.
      #|  -c, --create       新規ブランチを作る
      #|  --readonly         origin/<branch> を detached で _readonly/ に作る
      #|  --task <task>      対象の task。省略するとカレントディレクトリが属する task(. も同じ)
      #|  --repo <repo>      repos/ にある bare repo の名前
      #|  --branch <branch>  ブランチ名
      #|  --base <base>      --create の起点(origin/main など)
      #|----
      #|Usage: ws worktree rename --repo <repo> --branch <branch> --to <to> [options]
      #|
      #|作業用 worktree のブランチ名を変える(slug が変わればディレクトリも移動する)
      #|
      #|Options:
      #|  -h, --help         Show help information.
      #|  --task <task>      対象の task。省略するとカレントディレクトリが属する task(. も同じ)
      #|  --repo <repo>      repos/ にある bare repo の名前
      #|  --branch <branch>  現在のブランチ名
      #|  --to <to>          新しいブランチ名
      #|----
      #|Usage: ws worktree ls [options]
      #|
      #|task 内の worktree の一覧
      #|
      #|Options:
      #|  -h, --help     Show help information.
      #|  --task <task>  対象の task。省略するとカレントディレクトリが属する task(. も同じ)
      #|----
      #|Usage: ws worktree lint [options]
      #|
      #|task 内の規約違反と壊れた worktree を検査する
      #|
      #|Options:
      #|  -h, --help     Show help information.
      #|  --fix          自動で直せる問題を直す
      #|  --task <task>  対象の task。省略するとカレントディレクトリが属する task(. も同じ)
      #|----
      #|Usage: ws worktree pull [options]
      #|
      #|fetch して task 内の worktree を追従させる
      #|
      #|Options:
      #|  -h, --help     Show help information.
      #|  --task <task>  対象の task。省略するとカレントディレクトリが属する task(. も同じ)
      #|----
      #|Usage: ws ls
      #|
      #|全 task の一覧(1 task 1 行)
      #|
      #|Options:
      #|  -h, --help  Show help information.
      #|----
      #|Usage: ws lint [options]
      #|
      #|workspace 全体の規約違反と壊れた worktree を検査する
      #|
      #|Options:
      #|  -h, --help  Show help information.
      #|  --fix       自動で直せる問題を直す
      #|----
      #|Usage: ws compact [options]
      #|
      #|全 task の node_modules 内の同一ファイルを clonefile で共有し、容量を減らす
      #|
      #|Options:
      #|  -h, --help  Show help information.
      #|  --dry-run   置き換えずに、対象件数と削減見込みだけ表示する
    ),
  )
}
