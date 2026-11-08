///|
async test "agent guard, clone and add" {
  let s = Sandbox::new()
  defer s.cleanup()
  let remote = "file://\{s.root}/remotes/myrepo.git"
  s.env["CLAUDECODE"] = "1"
  ignore(s.ws(["clone", remote]))
  ignore(s.ws(["pull", "--task", "t-one"]))
  s.env.remove("CLAUDECODE")
  s.setup_ws(["new", "t-one"])
  ignore(
    s.ws(["add", "--task", "t-one", "--repo", "myrepo", "--branch", "foo"]),
  )
  s.make_remote("myrepo", ["release/wave3.1", "a/b"])
  ignore(s.ws(["clone", "\{remote}/"]))
  let bare = "\{s.ws_root}/repos/myrepo.git"
  s.note(s.git(bare, ["config", "branch.autoSetupMerge"]))
  s.note("local branches: \{s.git(bare, ["for-each-ref", "refs/heads"])}")

  let add = (args : Array[String]) => {
    s.ws(["add", "--task", "t-one", "--repo", "myrepo", ..args])
  }
  ignore(add(["--branch", "release/wave3.1"]))
  s.note(
    s.git("\{s.tasks}/t-one/myrepo--release-wave3-1", [
      "rev-parse", "--abbrev-ref", "@{u}",
    ]),
  )
  ignore(s.git(bare, ["branch", "localonly", "origin/review"]))
  ignore(add(["--branch", "localonly"]))
  ignore(add(["--branch", "nosuch"]))
  s.note(
    "duplicate rc=\{s.exec(ws_binary.wait(), ["add", "--task", "t-one", "--repo", "myrepo", "--branch", "release/wave3.1"]).code}",
  )
  ignore(add(["--branch", "fresh", "-c", "--base", "review"]))
  ignore(add(["--branch", "fresh", "--create", "--base", "origin/review"]))
  s.note(
    "fresh upstream rc=\{s.exec("git", ["-C", "\{s.tasks}/t-one/myrepo--fresh", "rev-parse", "@{u}"]).code}",
  )
  ignore(add(["--branch", "localonly", "-c", "--base", "origin/review"]))
  ignore(add(["--branch", "日本語", "-c", "--base", "origin/review"]))
  ignore(
    s.ws(["add", "--task", "t-one", "--repo", "my--repo", "--branch", "x"]),
  )
  ignore(add(["--readonly", "--branch", "release/wave3.1"]))
  ignore(add(["--readonly", "--branch", "nosuch"]))
  s.setup_ws(["new", "t-two"])
  ignore(
    s.ws([
      "add", "--branch", "q1", "-c", "--base", "origin/review", "--repo", "myrepo",
      "--task", "t-two",
    ]),
  )
  ignore(
    s.ws([
      "add", "--readonly", "--branch", "release/wave3.1", "--task", "t-two", "--repo",
      "myrepo",
    ]),
  )
  ignore(s.ws(["add", "--task", "t-opt", "--repo", "myrepo", "--branch", "x"]))
  ignore(s.ws(["ls", "--all"]))
  inspect(
    s.transcript.to_string(),
    content=(
      #|! ws: ws clone は ssh のパスフレーズ入力が必要なため、エージェントからは実行できません。ユーザーに別のターミナルで実行してもらってください
      #|rc=1
      #|! ws: ws pull は ssh のパスフレーズ入力が必要なため、エージェントからは実行できません。ユーザーに別のターミナルで実行してもらってください
      #|rc=1
      #|! ws: bare repo 'myrepo' がありません。別ターミナルで次を実行してください:
      #|rc=1
      #|! ws: $S/root/repos/myrepo.git は既に存在します
      #|rc=1
      #|simple
      #|local branches: 
      #|作成しました: $S/root/tasks/t-one/myrepo--release-wave3-1
      #|rc=0
      #|origin/release/wave3.1
      #|作成しました: $S/root/tasks/t-one/myrepo--localonly
      #|rc=0
      #|! ws: ローカルに 'nosuch' も 'origin/nosuch' もありません。リモートにあるはずなら、別ターミナルで ws pull --task <task> か git fetch が必要です。新規ブランチなら --branch <branch> --create --base <start-point> を使ってください
      #|rc=1
      #|duplicate rc=1
      #|! ws: --base 'review' を解決できません(origin/main のように明示してください)
      #|rc=1
      #|作成しました: $S/root/tasks/t-one/myrepo--fresh
      #|rc=0
      #|fresh upstream rc=128
      #|! ws: ローカルブランチ 'localonly' は既に存在します
      #|rc=1
      #|! ws: ブランチ名 '日本語' から slug を作れません
      #|rc=1
      #|! ws: repo 名が不正です('--' を含まず英数字 . _ - のみ): 'my--repo'
      #|rc=1
      #|作成しました: $S/root/tasks/t-one/_readonly/myrepo--release-wave3-1
      #|rc=0
      #|! ws: ローカルに 'nosuch' も 'origin/nosuch' もありません。リモートにあるはずなら、別ターミナルで ws pull --task <task> か git fetch が必要です。新規ブランチなら --branch <branch> --create --base <start-point> を使ってください
      #|rc=1
      #|作成しました: $S/root/tasks/t-two/myrepo--q1
      #|rc=0
      #|作成しました: $S/root/tasks/t-two/_readonly/myrepo--release-wave3-1
      #|rc=0
      #|! ws: task 't-opt' がありません。先に 'ws new t-opt' を実行してください
      #|rc=1
      #|[t-one]
      #|work   myrepo--fresh                                           fresh                                         clean
      #|work   myrepo--localonly                                       localonly                                     clean
      #|work   myrepo--release-wave3-1                                 release/wave3.1                               clean
      #|ro     myrepo--release-wave3-1                                 detached@0b8354e                              clean
      #|[t-two]
      #|work   myrepo--q1                                              q1                                            clean
      #|ro     myrepo--release-wave3-1                                 detached@0b8354e                              clean
      #|rc=0
      #|
    ),
  )
}

///|
async test "pull" {
  let s = Sandbox::new()
  defer s.cleanup()
  s.make_remote("myrepo", [
    "feature/ABC-1", "pull/ff", "pull/dirty", "pull/div", "release/wave3.1", "a/b",
  ])
  s.make_remote("other", [])
  s.setup_ws(["new", "t-pull"])
  ignore(s.ws(["pull", "--task", "t-pull"]))
  for
    args in [
      ["--repo", "myrepo", "--branch", "pull/ff"],
      ["--repo", "myrepo", "--branch", "feature/ABC-1"],
      ["--repo", "myrepo", "--branch", "pull/dirty"],
      ["--repo", "myrepo", "--branch", "pull/div"],
      ["--repo", "myrepo", "--branch", "noup", "-c", "--base", "origin/review"],
      ["--repo", "other", "--branch", "o1", "-c", "--base", "origin/review"],
      ["--repo", "myrepo", "--readonly", "--branch", "release/wave3.1"],
      ["--repo", "other", "--readonly", "--branch", "review"],
    ] {
    s.setup_ws(["add", "--task", "t-pull", ..args])
  }
  let task_dir = "\{s.tasks}/t-pull"
  write("\{task_dir}/myrepo--pull-dirty/f", "dirty\n")
  write("\{task_dir}/myrepo--pull-div/f", "local\n")
  ignore(s.git("\{task_dir}/myrepo--pull-div", ["commit", "-qam", "local"]))
  for
    branch in ["pull/ff", "pull/dirty", "pull/div", "review", "release/wave3.1"] {
    s.push_change("myrepo", branch)
  }
  s.env["GIT_TRACE"] = "\{s.root}/trace.txt"
  ignore(s.ws(["pull", "--task", "t-pull"]))
  s.env.remove("GIT_TRACE")
  s.note(
    "fetches: \{@fs.read_text("\{s.root}/trace.txt").split("\n").filter(line => line.contains("built-in: git fetch ")).count()}",
  )
  s.note(
    "merge commits: \{s.git("\{task_dir}/myrepo--pull-ff", ["log", "--oneline", "--merges"])}",
  )
  ignore(s.ws(["pull"], cwd="\{task_dir}/_readonly/myrepo--release-wave3-1"))

  s.setup_ws(["new", "t-multi"])
  s.setup_ws([
    "add", "--task", "t-multi", "--repo", "myrepo", "--readonly", "--branch", "a/b",
  ])
  ignore(s.git("\{s.root}/seed-myrepo", ["branch", "a-b", "review"]))
  ignore(
    s.git("\{s.root}/seed-myrepo", [
      "push", "-q", "../remotes/myrepo.git", "a-b",
    ]),
  )
  ignore(s.ws(["pull", "--task", "t-multi"]))
  @fs.rmdir("\{s.ws_root}/repos/other.git", recursive=true)
  ignore(s.ws(["pull", "--task", "t-pull"]))
  inspect(
    s.transcript.to_string(),
    content=(
      #|worktree はありません
      #|rc=0
      #|work  myrepo--feature-abc-1                                   up-to-date feature/ABC-1
      #|work  myrepo--noup                                            skipped    upstream なし
      #|work  myrepo--pull-dirty                                      skipped    未コミット変更あり
      #|work  myrepo--pull-div                                        skipped    pull/div: fatal: Not possible to fast-forward, aborting.
      #|work  myrepo--pull-ff                                         updated    pull/ff (be6549a)
      #|work  other--o1                                               skipped    upstream なし
      #|ro    myrepo--release-wave3-1                                 updated    origin/release/wave3.1 (b891f07)
      #|ro    other--review                                           up-to-date origin/review
      #|rc=0
      #|fetches: 2
      #|merge commits: 
      #|work  myrepo--feature-abc-1                                   up-to-date feature/ABC-1
      #|work  myrepo--noup                                            skipped    upstream なし
      #|work  myrepo--pull-dirty                                      skipped    未コミット変更あり
      #|work  myrepo--pull-div                                        skipped    pull/div: fatal: Not possible to fast-forward, aborting.
      #|work  myrepo--pull-ff                                         up-to-date pull/ff
      #|work  other--o1                                               skipped    upstream なし
      #|ro    myrepo--release-wave3-1                                 up-to-date origin/release/wave3.1
      #|ro    other--review                                           up-to-date origin/review
      #|rc=0
      #|ro    myrepo--a-b                                             error      slug に一致する origin ブランチが 2 件: a-b a/b
      #|rc=1
      #|work  myrepo--feature-abc-1                                   up-to-date feature/ABC-1
      #|work  myrepo--noup                                            skipped    upstream なし
      #|work  myrepo--pull-dirty                                      skipped    未コミット変更あり
      #|work  myrepo--pull-div                                        skipped    pull/div: fatal: Not possible to fast-forward, aborting.
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
  let s = Sandbox::new()
  defer s.cleanup()
  s.make_remote("myrepo", [])
  s.setup_ws(["new", "t-one"])
  s.setup_ws([
    "add", "--task", "t-one", "--repo", "myrepo", "--branch", "fresh", "-c", "--base",
    "origin/review",
  ])
  let task_dir = "\{s.tasks}/t-one"
  @fs.mkdir("\{task_dir}/myrepo--fresh/sub")
  ignore(s.ws(["ls"], cwd="\{task_dir}/myrepo--fresh/sub"))
  ignore(
    s.ws(
      [
        "add", "--repo", "myrepo", "--branch", "dotc", "-c", "--base", "origin/review",
      ],
      cwd=task_dir,
    ),
  )
  ignore(s.ws(["ls", "--task", "."], cwd=task_dir))
  let outside = "\{s.root}/outside"
  ignore(s.ws(["ls"], cwd=outside))
  ignore(s.ws(["ls", "--task", "."], cwd=outside))
  ignore(s.ws(["pull"], cwd=s.tasks))
  ignore(s.ws(["ls", "--task", "nope"]))
  ignore(s.ws(["new", "."]))
  ignore(s.ws(["new", "t-one"]))
  inspect(
    s.transcript.to_string(),
    content=(
      #|work   myrepo--fresh                                           fresh                                         clean
      #|rc=0
      #|作成しました: $S/root/tasks/t-one/myrepo--dotc
      #|rc=0
      #|work   myrepo--dotc                                            dotc                                          clean
      #|work   myrepo--fresh                                           fresh                                         clean
      #|rc=0
      #|! ws: カレントディレクトリが $S/root/tasks/<task>/ 配下ではありません。--task <task> か --all を指定してください
      #|rc=1
      #|! ws: カレントディレクトリが $S/root/tasks/<task>/ 配下ではありません
      #|rc=1
      #|! ws: カレントディレクトリが $S/root/tasks/<task>/ 配下ではありません。--task <task> を指定してください
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
async test "usage errors exit with 2" {
  let s = Sandbox::new()
  defer s.cleanup()
  let codes = []
  for
    args in [
      [],
      ["bogus"],
      ["new"],
      ["new", "a", "b"],
      ["clone"],
      ["compact", "--task", "t"],
      ["add", "--repo", "r", "--bogus", "--branch", "b"],
      ["add", "--branch", "x"],
      ["add", "--repo", "r", "--branch", "x", "extra"],
      ["add", "--repo", "r", "--branch", "x", "--base", "origin/review"],
      ["add", "--repo", "r", "--branch", "x", "-c"],
      ["add", "--repo", "r", "--branch", "x", "--readonly", "-c", "--base", "o"],
      ["add", "--repo", "r", "--branch", "-x"],
      ["rename", "--repo", "r", "--branch", "a"],
      ["ls", "--all", "--task", "t"],
      ["lint", "--all", "--task", "t"],
      ["lint", "--task", "--bogus"],
    ] {
    codes.push(s.exec(ws_binary.wait(), args).code.to_string())
  }
  s.note(codes.join(" "))
  s.note("help rc=\{s.exec(ws_binary.wait(), ["--help"]).code}")
  inspect(
    s.transcript.to_string(),
    content=(
      #|2 2 2 2 2 2 2 2 2 2 2 2 2 2 2 2 2
      #|help rc=0
      #|
    ),
  )
}

///|
async fn Sandbox::rename(
  self : Sandbox,
  args : Array[String],
  cwd? : String,
) -> Outcome {
  self.ws(["rename", "--repo", "myrepo", ..args], cwd?)
}

///|
async test "rename" {
  let s = Sandbox::new()
  defer s.cleanup()
  s.make_remote("myrepo", ["tracked/old", "other"])
  s.setup_ws(["new", "t-ren"])
  let task_dir = "\{s.tasks}/t-ren"
  let bare = "\{s.ws_root}/repos/myrepo.git"
  let add = (args : Array[String]) => {
    s.setup_ws(["add", "--task", "t-ren", "--repo", "myrepo", ..args])
  }
  add(["--branch", "plain", "-c", "--base", "origin/review"])
  add(["--branch", "feat/x", "-c", "--base", "origin/review"])
  add(["--branch", "tracked/old"])
  add(["--branch", "other"])
  add(["--branch", "taken", "-c", "--base", "origin/review"])
  add(["--branch", "stay", "-c", "--base", "origin/review"])
  add(["--branch", "inside", "-c", "--base", "origin/review"])
  add(["--readonly", "--branch", "other"])
  ignore(
    s.rename(["--task", "t-ren", "--branch", "plain", "--to", "renamed/plain"]),
  )
  ignore(s.ws(["ls", "--task", "t-ren"]))
  ignore(s.ws(["lint", "--task", "t-ren"]))
  s.note(
    "branches: \{s.git(bare, ["branch", "--format=%(refname:short)"]).replace_all(old="\n", new=" ")}",
  )

  ignore(s.rename(["--branch", "feat/x", "--to", "feat--x"], cwd=task_dir))
  s.note(
    "slug unchanged: \{@fs.exists("\{task_dir}/myrepo--feat-x")} \{s.git("\{task_dir}/myrepo--feat-x", ["branch", "--show-current"])}",
  )
  ignore(s.ws(["lint", "--task", "t-ren"]))

  ignore(
    s.rename([
      "--task", "t-ren", "--branch", "tracked/old", "--to", "tracked/new",
    ]),
  )
  s.note(
    "upstream rc=\{s.exec("git", ["-C", "\{task_dir}/myrepo--tracked-new", "rev-parse", "@{u}"]).code}",
  )
  s.note(
    "remote branch kept: \{s.exec("git", ["-C", bare, "show-ref", "--verify", "--quiet", "refs/remotes/origin/tracked/old"]).code == 0}",
  )
  ignore(s.rename(["--task", "t-ren", "--branch", "plain", "--to", "x"]))

  ignore(s.rename(["--task", "t-ren", "--branch", "taken", "--to", "other"]))
  @fs.mkdir("\{task_dir}/myrepo--occupied")
  ignore(s.rename(["--task", "t-ren", "--branch", "taken", "--to", "occupied"]))
  ignore(
    s.rename(["--task", "t-ren", "--branch", "taken", "--to", "bad..name"]),
  )
  ignore(s.rename(["--task", "t-ren", "--branch", "taken", "--to", "@{-1}"]))
  ignore(s.rename(["--task", "t-ren", "--branch", "stay", "--to", "stay"]))
  ignore(s.rename(["--task", "t-ren", "--branch", "other", "--to", "other2"]))
  ignore(
    s.ws([
      "rename", "--task", "t-ren", "--repo", "nosuch", "--branch", "stay", "--to",
      "z",
    ]),
  )
  s.note(
    "taken dir kept: \{@fs.exists("\{task_dir}/myrepo--taken")} \{s.git("\{task_dir}/myrepo--taken", ["branch", "--show-current"])}",
  )

  ignore(
    s.rename(
      ["--branch", "inside", "--to", "outside"],
      cwd="\{task_dir}/myrepo--inside",
    ),
  )
  s.note("inside kept: \{@fs.exists("\{task_dir}/myrepo--inside")}")
  ignore(s.ws(["ls", "--task", "t-ren"]))
  inspect(
    s.transcript.to_string(),
    content=(
      #|リネームしました: plain → renamed/plain
      #|新しいパス: $S/root/tasks/t-ren/myrepo--renamed-plain
      #|rc=0
      #|work   myrepo--feat-x                                          feat/x                                        clean
      #|work   myrepo--inside                                          inside                                        clean
      #|work   myrepo--other                                           other                                         clean
      #|work   myrepo--renamed-plain                                   renamed/plain                                 clean
      #|work   myrepo--stay                                            stay                                          clean
      #|work   myrepo--taken                                           taken                                         clean
      #|work   myrepo--tracked-old                                     tracked/old                                   clean
      #|ro     myrepo--other                                           detached@0b8354e                              clean
      #|rc=0
      #|問題はありません
      #|rc=0
      #|branches: feat/x inside other renamed/plain stay taken tracked/old
      #|リネームしました: feat/x → feat--x
      #|rc=0
      #|slug unchanged: true feat--x
      #|問題はありません
      #|rc=0
      #|リネームしました: tracked/old → tracked/new
      #|新しいパス: $S/root/tasks/t-ren/myrepo--tracked-new
      #|origin/tracked/old は残っています。リモートに反映するには(別ターミナルで):
      #|  git push -u origin tracked/new
      #|  git push origin --delete tracked/old
      #|※ tracked/old の PR がある場合は、代わりに GitHub の Branches 画面でリネームし、
      #|  ws pull の後に git branch -u origin/tracked/new を実行してください
      #|rc=0
      #|upstream rc=128
      #|remote branch kept: true
      #|! ws: 作業用 worktree $S/root/tasks/t-ren/myrepo--plain が無いか、ブランチ 'plain' を checkout していません
      #|rc=1
      #|! ws: ローカルブランチ 'other' は既に存在します
      #|rc=1
      #|! ws: 移動先 $S/root/tasks/t-ren/myrepo--occupied は既に存在します
      #|rc=1
      #|! ws: ブランチ名 'bad..name' は git で使えません
      #|rc=1
      #|! ws: ブランチ名 '@{-1}' は git で使えません
      #|rc=1
      #|! ws: ローカルブランチ 'stay' は既に存在します
      #|rc=1
      #|リネームしました: other → other2
      #|新しいパス: $S/root/tasks/t-ren/myrepo--other2
      #|origin/other は残っています。リモートに反映するには(別ターミナルで):
      #|  git push -u origin other2
      #|  git push origin --delete other
      #|※ other の PR がある場合は、代わりに GitHub の Branches 画面でリネームし、
      #|  ws pull の後に git branch -u origin/other2 を実行してください
      #|rc=0
      #|! ws: bare repo 'nosuch' がありません。別ターミナルで次を実行してください:
      #|rc=1
      #|taken dir kept: true taken
      #|! ws: カレントディレクトリが $S/root/tasks/t-ren/myrepo--inside 配下のため、worktree を移動できません。別のディレクトリで実行してください
      #|rc=1
      #|inside kept: true
      #|work   myrepo--feat-x                                          feat--x                                       clean
      #|work   myrepo--inside                                          inside                                        clean
      #|work   myrepo--other2                                          other2                                        clean
      #|work   myrepo--renamed-plain                                   renamed/plain                                 clean
      #|work   myrepo--stay                                            stay                                          clean
      #|work   myrepo--taken                                           taken                                         clean
      #|work   myrepo--tracked-new                                     tracked/new                                   clean
      #|ro     myrepo--other                                           detached@0b8354e                              clean
      #|rc=0
      #|
    ),
  )
}
