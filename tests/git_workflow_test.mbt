///|
async fn setup_remotes(sandbox : Sandbox) -> Unit {
  let seed = "\{sandbox.root}/seed"
  @fs.mkdir(seed, recursive=true)
  @fs.mkdir("\{sandbox.root}/remotes", recursive=true)
  @fs.mkdir("\{sandbox.root}/outside", recursive=true)
  ignore(sandbox.git(seed, ["init", "-q", "-b", "review", "."]))
  sandbox.write("\{seed}/f", "1\n")
  ignore(sandbox.git(seed, ["add", "f"]))
  ignore(sandbox.git(seed, ["commit", "-qm", "init"]))
  for
    branch in [
      "release/wave3.1", "feature/ABC-123/add_user_api", "a/b", "pull/ff", "pull/dirty",
      "pull/div",
    ] {
    ignore(sandbox.git(seed, ["branch", branch]))
  }
  ignore(
    sandbox.git(seed, ["clone", "-q", "--bare", ".", "../remotes/myrepo.git"]),
  )
  ignore(
    sandbox.git(seed, ["clone", "-q", "--bare", ".", "../remotes/other.git"]),
  )
}

///|
async fn push_change(sandbox : Sandbox, branch : String, line : String) -> Unit {
  let seed = "\{sandbox.root}/seed"
  ignore(sandbox.git(seed, ["checkout", "-q", branch]))
  let content = @fs.read_text("\{seed}/f")
  sandbox.write("\{seed}/f", content + line + "\n")
  ignore(sandbox.git(seed, ["commit", "-qam", line]))
  ignore(sandbox.git(seed, ["push", "-q", "../remotes/myrepo.git", branch]))
}

///|
async test "agent guard, clone and add" {
  let log = Transcript::new()
  let sandbox = Sandbox::new()
  defer sandbox.cleanup()
  setup_remotes(sandbox)
  let remote = "file://\{sandbox.root}/remotes/myrepo.git"
  let agent = Map([("CLAUDECODE", Some("1"))])
  log.record(sandbox.ws(["clone", remote], env_changes=agent).render())
  ignore(sandbox.ws(["new", "t-one"]))
  log.record(sandbox.ws(["pull", "t-one"], env_changes=agent).render())
  log.record(sandbox.exists("\{sandbox.ws_root}/repos"))

  log.record(
    sandbox
    .ws(["add", "--task", "t-one", "--repo", "myrepo", "--branch", "foo"])
    .render(),
  )
  log.record(
    sandbox
    .ws([
      "add", "--task", "t-one", "--repo", "myrepo", "--readonly", "--branch", "foo",
    ])
    .render(),
  )

  log.record(sandbox.ws(["clone", remote]).render())
  log.record(
    sandbox.ws(["clone", "file://\{sandbox.root}/remotes/other.git/"]).render(),
  )
  log.record(sandbox.ws(["clone", remote]).render())
  let bare = "\{sandbox.ws_root}/repos/myrepo.git"
  log.record(sandbox.git(bare, ["config", "branch.autoSetupMerge"]))
  log.record(sandbox.git(bare, ["for-each-ref", "refs/heads"]))

  let tasks = "\{sandbox.ws_root}/tasks"
  log.record(
    sandbox
    .ws([
      "add", "--task", "t-one", "--repo", "myrepo", "--branch", "release/wave3.1",
    ])
    .render(),
  )
  log.record(
    sandbox.git("\{tasks}/t-one/myrepo--release-wave3-1", [
      "rev-parse", "--abbrev-ref", "@{u}",
    ]),
  )
  ignore(sandbox.git(bare, ["branch", "localonly", "origin/review"]))
  log.record(
    sandbox
    .ws(["add", "--task", "t-one", "--repo", "myrepo", "--branch", "localonly"])
    .render(),
  )
  log.record(
    sandbox
    .ws(["add", "--task", "t-one", "--repo", "myrepo", "--branch", "nosuch"])
    .render(),
  )
  log.record(
    sandbox.ws([
      "add", "--task", "t-one", "--repo", "myrepo", "--branch", "release/wave3.1",
    ]).code,
  )

  log.record(
    sandbox.ws([
      "add", "--task", "t-one", "--repo", "myrepo", "--branch", "fresh", "-c",
    ]).code,
  )
  log.record(
    sandbox
    .ws([
      "add", "--task", "t-one", "--repo", "myrepo", "--branch", "fresh", "-c", "--base",
      "review",
    ])
    .render(),
  )
  log.record(
    sandbox
    .ws([
      "add", "--task", "t-one", "--repo", "myrepo", "--branch", "fresh", "-c", "--base",
      "origin/review",
    ])
    .render(),
  )
  log.record(
    sandbox.exec("git", [
      "-C",
      "\{tasks}/t-one/myrepo--fresh",
      "rev-parse",
      "--abbrev-ref",
      "@{u}",
    ]).code,
  )
  log.record(
    sandbox
    .ws([
      "add", "--task", "t-one", "--repo", "myrepo", "--branch", "localonly", "-c",
      "--base", "origin/review",
    ])
    .render(),
  )
  log.record(
    sandbox
    .ws([
      "add", "--task", "t-one", "--repo", "myrepo", "--branch", "x", "--base", "-x",
      "-c",
    ])
    .render(),
  )
  log.record(
    sandbox
    .ws([
      "add", "--task", "t-one", "--repo", "myrepo", "--branch", "-x", "--base", "origin/review",
      "-c",
    ])
    .render(),
  )
  log.record(
    sandbox
    .ws([
      "add", "--task", "t-one", "--repo", "myrepo", "--branch", "日本語", "--base",
      "origin/review", "-c",
    ])
    .render(),
  )
  log.record(
    sandbox
    .ws(["add", "--task", "t-one", "--repo", "my--repo", "--branch", "x"])
    .render(),
  )

  log.record(
    sandbox
    .ws([
      "add", "--task", "t-one", "--repo", "myrepo", "--readonly", "--branch", "release/wave3.1",
    ])
    .render(),
  )
  ignore(sandbox.ws(["new", "t-two"]))
  log.record(
    sandbox
    .ws([
      "add", "--task", "t-two", "--repo", "myrepo", "--readonly", "--branch", "release/wave3.1",
    ])
    .render(),
  )
  log.record(
    sandbox
    .ws([
      "add", "--task", "t-two", "--repo", "myrepo", "--readonly", "--branch", "nosuch",
    ])
    .render(),
  )
  log.record(
    sandbox
    .ws([
      "add", "--branch", "q1", "-c", "--base", "origin/review", "--repo", "myrepo",
      "--task", "t-two",
    ])
    .render(),
  )
  log.record(
    sandbox
    .ws([
      "add", "--readonly", "--branch", "release/wave3.1", "--task", "t-opt", "--repo",
      "myrepo",
    ])
    .render(),
  )
  log.record(
    sandbox
    .ws([
      "add", "--task", "t-two", "--repo", "myrepo", "--branch", "cr1", "--create",
      "--base", "origin/review",
    ])
    .render(),
  )
  log.record(sandbox.ws(["ls", "--task", "t-two"]).render())
  log.record(sandbox.ws(["ls", "--all"]).render())
  inspect(
    log.text(),
    content=(
      #|! ws: ws clone は ssh のパスフレーズ入力が必要なため、エージェントからは実行できません。ユーザーに別のターミナルで実行してもらってください
      #|rc=1
      #|! ws: ws pull は ssh のパスフレーズ入力が必要なため、エージェントからは実行できません。ユーザーに別のターミナルで実行してもらってください
      #|rc=1
      #|false
      #|! ws: bare repo 'myrepo' がありません。別ターミナルで次を実行してください:
      #|!   ws clone <url> myrepo
      #|rc=1
      #|! ws: bare repo 'myrepo' がありません。別ターミナルで次を実行してください:
      #|!   ws clone <url> myrepo
      #|rc=1
      #|作成しました: $S/root/repos/myrepo.git
      #|rc=0
      #|作成しました: $S/root/repos/other.git
      #|rc=0
      #|! ws: $S/root/repos/myrepo.git は既に存在します
      #|rc=1
      #|simple
      #|
      #|作成しました: $S/root/tasks/t-one/myrepo--release-wave3-1
      #|rc=0
      #|origin/release/wave3.1
      #|作成しました: $S/root/tasks/t-one/myrepo--localonly
      #|rc=0
      #|! ws: ローカルに 'nosuch' も 'origin/nosuch' もありません。リモートにあるはずなら、別ターミナルで ws pull --task <task> か git fetch が必要です。新規ブランチなら --branch <branch> --create --base <start-point> を使ってください
      #|rc=1
      #|1
      #|2
      #|! ws: --base 'review' を解決できません(origin/main のように明示してください)
      #|rc=1
      #|作成しました: $S/root/tasks/t-one/myrepo--fresh
      #|rc=0
      #|128
      #|! ws: ローカルブランチ 'localonly' は既に存在します
      #|rc=1
      #|! usage:
      #|!   ws new <task>
      #|!   ws clone <url> [<repo>]
      #|!   ws add [--task <task>] --repo <repo> --branch <branch> [--no-install]
      #|!   ws add [--task <task>] --repo <repo> --branch <branch> --create --base <start-point> [--no-install]
      #|!   ws add [--task <task>] --repo <repo> --branch <branch> --readonly [--no-install]
      #|!   ws pull [--task <task>]
      #|!   ws ls [--task <task> | --all]
      #|!   ws npm ci [-- <npm ci の引数>]
      #|!   ws lint [--task <task> | --all] [--fix]
      #|! --create の短縮形は -c
      #|! --task を省略するとカレントディレクトリが属する task を使う(--task . も同じ)
      #|rc=2
      #|! usage:
      #|!   ws new <task>
      #|!   ws clone <url> [<repo>]
      #|!   ws add [--task <task>] --repo <repo> --branch <branch> [--no-install]
      #|!   ws add [--task <task>] --repo <repo> --branch <branch> --create --base <start-point> [--no-install]
      #|!   ws add [--task <task>] --repo <repo> --branch <branch> --readonly [--no-install]
      #|!   ws pull [--task <task>]
      #|!   ws ls [--task <task> | --all]
      #|!   ws npm ci [-- <npm ci の引数>]
      #|!   ws lint [--task <task> | --all] [--fix]
      #|! --create の短縮形は -c
      #|! --task を省略するとカレントディレクトリが属する task を使う(--task . も同じ)
      #|rc=2
      #|! ws: ブランチ名 '日本語' から slug を作れません
      #|rc=1
      #|! ws: repo 名が不正です('--' を含まず英数字 . _ - のみ): 'my--repo'
      #|rc=1
      #|作成しました: $S/root/tasks/t-one/_readonly/myrepo--release-wave3-1
      #|rc=0
      #|作成しました: $S/root/tasks/t-two/_readonly/myrepo--release-wave3-1
      #|rc=0
      #|! ws: ローカルに 'nosuch' も 'origin/nosuch' もありません。リモートにあるはずなら、別ターミナルで ws pull --task <task> か git fetch が必要です。新規ブランチなら --branch <branch> --create --base <start-point> を使ってください
      #|rc=1
      #|作成しました: $S/root/tasks/t-two/myrepo--q1
      #|rc=0
      #|! ws: task 't-opt' がありません。先に 'ws new t-opt' を実行してください
      #|rc=1
      #|作成しました: $S/root/tasks/t-two/myrepo--cr1
      #|rc=0
      #|work   myrepo--cr1                                             cr1                                           clean
      #|work   myrepo--q1                                              q1                                            clean
      #|ro     myrepo--release-wave3-1                                 detached@0b8354e                              clean
      #|rc=0
      #|[t-one]
      #|work   myrepo--fresh                                           fresh                                         clean
      #|work   myrepo--localonly                                       localonly                                     clean
      #|work   myrepo--release-wave3-1                                 release/wave3.1                               clean
      #|ro     myrepo--release-wave3-1                                 detached@0b8354e                              clean
      #|[t-two]
      #|work   myrepo--cr1                                             cr1                                           clean
      #|work   myrepo--q1                                              q1                                            clean
      #|ro     myrepo--release-wave3-1                                 detached@0b8354e                              clean
      #|rc=0
      #|
    ),
  )
}

///|
async test "pull" {
  let log = Transcript::new()
  let sandbox = Sandbox::new()
  defer sandbox.cleanup()
  setup_remotes(sandbox)
  ignore(sandbox.ws(["clone", "file://\{sandbox.root}/remotes/myrepo.git"]))
  ignore(sandbox.ws(["clone", "file://\{sandbox.root}/remotes/other.git"]))
  ignore(sandbox.ws(["new", "t-pull"]))
  log.record(sandbox.ws(["pull", "--task", "t-pull"]).render())
  let add = async fn(args : Array[String]) {
    let outcome = sandbox.ws(["add", "--task", "t-pull", ..args])
    if outcome.code != 0 {
      fail("ws add \{args.join(" ")} failed: \{outcome.render()}")
    }
  }
  add(["--repo", "myrepo", "--branch", "pull/ff"])
  add(["--repo", "myrepo", "--branch", "feature/ABC-123/add_user_api"])
  add(["--repo", "myrepo", "--branch", "pull/dirty"])
  add(["--repo", "myrepo", "--branch", "pull/div"])
  add(["--repo", "myrepo", "--branch", "noup", "-c", "--base", "origin/review"])
  add(["--repo", "other", "--branch", "o1", "-c", "--base", "origin/review"])
  add(["--repo", "myrepo", "--readonly", "--branch", "release/wave3.1"])
  add(["--repo", "other", "--readonly", "--branch", "review"])
  let task_dir = "\{sandbox.ws_root}/tasks/t-pull"
  sandbox.write("\{task_dir}/myrepo--pull-dirty/f", "1\nd\n")
  sandbox.write("\{task_dir}/myrepo--pull-div/f", "1\nlocal\n")
  ignore(
    sandbox.git("\{task_dir}/myrepo--pull-div", ["commit", "-qam", "local"]),
  )
  push_change(sandbox, "pull/ff", "u1")
  push_change(sandbox, "pull/dirty", "u2")
  push_change(sandbox, "pull/div", "u3")
  push_change(sandbox, "review", "u4")
  push_change(sandbox, "release/wave3.1", "u5")

  let trace = "\{sandbox.root}/trace.txt"
  log.record(
    sandbox
    .ws(
      ["pull", "--task", "t-pull"],
      env_changes=Map([("GIT_TRACE", Some(trace))]),
    )
    .render(),
  )
  let fetches = @fs.read_text(trace)
    .split("\n")
    .filter(line => line.contains("built-in: git") && line.contains(" fetch"))
    .count()
  log.record(fetches)
  log.record(
    sandbox.git("\{task_dir}/myrepo--pull-ff", ["rev-parse", "HEAD"]) ==
    sandbox.git("\{sandbox.root}/remotes/myrepo.git", ["rev-parse", "pull/ff"]),
  )
  log.record(
    sandbox.git("\{task_dir}/myrepo--pull-ff", ["log", "--oneline", "--merges"]),
  )
  log.record(sandbox.ws(["pull", "--task", "t-pull"]).render())
  log.record(
    sandbox.ws(["pull"], cwd="\{task_dir}/_readonly/myrepo--release-wave3-1").code,
  )

  ignore(sandbox.ws(["new", "t-multi"]))
  ignore(
    sandbox.ws([
      "add", "--task", "t-multi", "--repo", "myrepo", "--readonly", "--branch", "a/b",
    ]),
  )
  ignore(sandbox.git("\{sandbox.root}/seed", ["branch", "a-b", "review"]))
  ignore(
    sandbox.git("\{sandbox.root}/seed", [
      "push", "-q", "../remotes/myrepo.git", "a-b",
    ]),
  )
  log.record(sandbox.ws(["pull", "--task", "t-multi"]).render())

  @fs.rmdir("\{sandbox.ws_root}/repos/other.git", recursive=true)
  log.record(sandbox.ws(["pull", "--task", "t-pull"]).render())
  inspect(
    log.text(),
    content=(
      #|worktree はありません
      #|rc=0
      #|work  myrepo--feature-abc-123-add-user-api                    up-to-date feature/ABC-123/add_user_api
      #|work  myrepo--noup                                            skipped    upstream なし
      #|work  myrepo--pull-dirty                                      skipped    未コミット変更あり
      #|work  myrepo--pull-div                                        skipped    pull/div は upstream と分岐しています(fast-forward 不可)
      #|work  myrepo--pull-ff                                         updated    pull/ff (259089a)
      #|work  other--o1                                               skipped    upstream なし
      #|ro    myrepo--release-wave3-1                                 updated    origin/release/wave3.1 (f7ef26e)
      #|ro    other--review                                           up-to-date origin/review
      #|rc=0
      #|2
      #|true
      #|
      #|work  myrepo--feature-abc-123-add-user-api                    up-to-date feature/ABC-123/add_user_api
      #|work  myrepo--noup                                            skipped    upstream なし
      #|work  myrepo--pull-dirty                                      skipped    未コミット変更あり
      #|work  myrepo--pull-div                                        skipped    pull/div は upstream と分岐しています(fast-forward 不可)
      #|work  myrepo--pull-ff                                         up-to-date pull/ff
      #|work  other--o1                                               skipped    upstream なし
      #|ro    myrepo--release-wave3-1                                 up-to-date origin/release/wave3.1
      #|ro    other--review                                           up-to-date origin/review
      #|rc=0
      #|0
      #|ro    myrepo--a-b                                             error      slug に一致する origin ブランチが 2 件: a-b a/b
      #|rc=1
      #|work  myrepo--feature-abc-123-add-user-api                    up-to-date feature/ABC-123/add_user_api
      #|work  myrepo--noup                                            skipped    upstream なし
      #|work  myrepo--pull-dirty                                      skipped    未コミット変更あり
      #|work  myrepo--pull-div                                        skipped    pull/div は upstream と分岐しています(fast-forward 不可)
      #|work  myrepo--pull-ff                                         up-to-date pull/ff
      #|work  other--o1                                               error      bare repo 'other' がありません。別ターミナルで次を実行してください:
      #|  ws clone <url> other
      #|ro    myrepo--release-wave3-1                                 up-to-date origin/release/wave3.1
      #|ro    other--review                                           error      bare repo 'other' がありません。別ターミナルで次を実行してください:
      #|  ws clone <url> other
      #|rc=1
      #|
    ),
  )
}

///|
async test "task resolution from the current directory" {
  let log = Transcript::new()
  let sandbox = Sandbox::new()
  defer sandbox.cleanup()
  setup_remotes(sandbox)
  ignore(sandbox.ws(["clone", "file://\{sandbox.root}/remotes/myrepo.git"]))
  ignore(sandbox.ws(["new", "t-one"]))
  let task_dir = "\{sandbox.ws_root}/tasks/t-one"
  ignore(
    sandbox.ws([
      "add", "--task", "t-one", "--repo", "myrepo", "--branch", "fresh", "-c", "--base",
      "origin/review",
    ]),
  )
  ignore(
    sandbox.ws([
      "add", "--task", "t-one", "--repo", "myrepo", "--readonly", "--branch", "review",
    ]),
  )
  @fs.mkdir("\{task_dir}/myrepo--fresh/sub", recursive=true)
  for
    cwd in [task_dir, "\{task_dir}/myrepo--fresh/sub", "\{task_dir}/_readonly"] {
    log.record(sandbox.ws(["ls"], cwd~).render())
  }
  log.record(
    sandbox
    .ws(
      [
        "add", "--repo", "myrepo", "--branch", "dotc", "-c", "--base", "origin/review",
      ],
      cwd="\{task_dir}/myrepo--fresh",
    )
    .render(),
  )
  log.record(sandbox.ws(["ls", "--task", "."], cwd=task_dir).code)
  let outside = "\{sandbox.root}/outside"
  log.record(sandbox.ws(["ls"], cwd=outside).render())
  log.record(sandbox.ws(["ls", "--task", "."], cwd=outside).render())
  log.record(
    sandbox
    .ws(["add", "--repo", "myrepo", "--branch", "zz"], cwd=outside)
    .render(),
  )
  log.record(sandbox.ws(["pull"], cwd="\{sandbox.ws_root}/repos").render())
  log.record(sandbox.ws(["lint"], cwd="\{sandbox.ws_root}/tasks").render())
  log.record(sandbox.ws(["ls", "--task", "nope"]).render())
  log.record(sandbox.ws(["new", "."]).render())
  log.record(sandbox.ws(["new", "t-one"]).render())
  inspect(
    log.text(),
    content=(
      #|work   myrepo--fresh                                           fresh                                         clean
      #|ro     myrepo--review                                          detached@0b8354e                              clean
      #|rc=0
      #|work   myrepo--fresh                                           fresh                                         clean
      #|ro     myrepo--review                                          detached@0b8354e                              clean
      #|rc=0
      #|work   myrepo--fresh                                           fresh                                         clean
      #|ro     myrepo--review                                          detached@0b8354e                              clean
      #|rc=0
      #|作成しました: $S/root/tasks/t-one/myrepo--dotc
      #|rc=0
      #|0
      #|! ws: カレントディレクトリが $S/root/tasks/<task>/ 配下ではありません。--task <task> か --all を指定してください
      #|rc=1
      #|! ws: カレントディレクトリが $S/root/tasks/<task>/ 配下ではありません
      #|rc=1
      #|! ws: カレントディレクトリが $S/root/tasks/<task>/ 配下ではありません。--task <task> を指定してください
      #|rc=1
      #|! ws: カレントディレクトリが $S/root/tasks/<task>/ 配下ではありません。--task <task> を指定してください
      #|rc=1
      #|! ws: カレントディレクトリが $S/root/tasks/<task>/ 配下ではありません。--task <task> か --all を指定してください
      #|rc=1
      #|! ws: task 'nope' がありません。先に 'ws new nope' を実行してください
      #|rc=1
      #|! ws: task 名は [a-z0-9-] のみ(先頭末尾・連続ハイフン不可): '.'
      #|rc=1
      #|task 't-one' は既に存在します: $S/root/tasks/t-one
      #|rc=0
      #|
    ),
  )
}

///|
async test "usage errors" {
  let log = Transcript::new()
  let sandbox = Sandbox::new()
  defer sandbox.cleanup()
  let usage_code = async fn(args : Array[String]) -> Int32 {
    sandbox.ws(args).code
  }
  log.record(sandbox.ws([]).render())
  log.record(sandbox.ws(["--help"]).stdout == sandbox.ws(["bogus"]).stderr)
  log.record(
    [
      usage_code(["bogus"]),
      usage_code(["rm", "t-one", "x"]),
      usage_code(["new"]),
      usage_code(["new", "a", "b"]),
      usage_code([
        "add", "--task", "t", "--repo", "r", "--bogus", "--branch", "b",
      ]),
      usage_code(["add", "--task", "t", "--branch", "x"]),
      usage_code(["add", "--task", "t", "--repo", "r"]),
      usage_code(["add", "--task", "t", "--repo", "r", "--branch", "x", "extra"]),
      usage_code([
        "add", "--task", "t", "--repo", "r", "--branch", "x", "--base", "origin/review",
      ]),
      usage_code(["add", "--task", "t", "--repo", "r", "--branch", "x", "-c"]),
      usage_code([
        "add", "--task", "t", "--repo", "r", "--branch", "x", "--readonly", "--base",
        "o",
      ]),
      usage_code([
        "add", "--task", "t", "--repo", "r", "--branch", "x", "--readonly", "-c",
        "--base", "o",
      ]),
      usage_code(["ls", "--all", "--task", "t"]),
      usage_code(["lint", "--all", "--task", "t"]),
      usage_code(["lint", "a", "b"]),
      usage_code(["lint", "--task", "--bogus"]),
      usage_code(["npm"]),
      usage_code(["npm", "install"]),
      usage_code(["npm", "ci", "--x"]),
      usage_code(["clone"]),
    ]
    .map(code => code.to_string())
    .join(" "),
  )
  log.record(usage_code(["-h"]))
  inspect(
    log.text(),
    content=(
      #|! usage:
      #|!   ws new <task>
      #|!   ws clone <url> [<repo>]
      #|!   ws add [--task <task>] --repo <repo> --branch <branch> [--no-install]
      #|!   ws add [--task <task>] --repo <repo> --branch <branch> --create --base <start-point> [--no-install]
      #|!   ws add [--task <task>] --repo <repo> --branch <branch> --readonly [--no-install]
      #|!   ws pull [--task <task>]
      #|!   ws ls [--task <task> | --all]
      #|!   ws npm ci [-- <npm ci の引数>]
      #|!   ws lint [--task <task> | --all] [--fix]
      #|! --create の短縮形は -c
      #|! --task を省略するとカレントディレクトリが属する task を使う(--task . も同じ)
      #|rc=2
      #|true
      #|2 2 2 2 2 2 2 2 2 2 2 2 2 2 2 2 2 2 2 2
      #|0
      #|
    ),
  )
}
