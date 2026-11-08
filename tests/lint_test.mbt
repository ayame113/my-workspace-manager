///|
async test "lint worktree rules" {
  let s = Sandbox::new()
  defer s.cleanup()
  s.make_remote("myrepo", ["b1", "c1", "c2", "c3", "d1"])
  s.make_remote("other", ["d2"])
  let add = async fn(task : String, args : Array[String]) {
    s.setup_ws(["worktree", "add", "--task", task, ..args])
  }
  s.setup_ws(["new", "t2"])
  for branch in ["c1", "c2", "c3"] {
    add("t2", ["--repo", "myrepo", "--branch", branch])
  }
  let t2 = "\{s.tasks}/t2"
  ignore(s.git("\{t2}/myrepo--c1", ["switch", "-q", "-c", "other"]))
  ignore(s.git("\{t2}/myrepo--c2", ["switch", "-q", "--detach"]))
  ignore(
    s.git("\{t2}/myrepo--c3", ["branch", "-q", "--set-upstream-to=origin/c1"]),
  )
  ignore(s.ws(["worktree", "lint", "--task", "t2", "--fix"]))
  s.note(
    "c3 upstream rc=\{s.exec("git", ["-C", "\{t2}/myrepo--c3", "rev-parse", "@{u}"]).code}",
  )

  s.setup_ws(["new", "t3"])
  add("t3", ["--repo", "myrepo", "--readonly", "--branch", "b1"])
  let readonly_worktree = "\{s.tasks}/t3/_readonly/myrepo--b1"
  ignore(s.git(readonly_worktree, ["switch", "-q", "-c", "tmpb"]))
  write("\{readonly_worktree}/f", "changed\n")
  ignore(s.git(readonly_worktree, ["commit", "-qam", "c"]))
  write("\{readonly_worktree}/f", "dirty\n")
  ignore(s.ws(["worktree", "lint", "--task", "t3"]))

  s.setup_ws(["new", "t4"])
  add("t4", ["--repo", "myrepo", "--branch", "d1"])
  add("t4", ["--repo", "other", "--branch", "d2"])
  let t4 = "\{s.tasks}/t4"
  @fs.rmdir("\{t4}/myrepo--d1", recursive=true)
  @fs.mkdir("\{t4}/myrepo--ghost")
  @fs.mkdir("\{t4}/random-dir")
  @fs.mkdir("\{t4}/_readonly/zzz", recursive=true)
  @fs.mkdir("\{s.tasks}/Bad_Task")
  @fs.rename("\{t4}/other--d2", "\{t4}/myrepo--d9")
  ignore(s.ws(["worktree", "lint", "--task", "t4", "--fix"]))
  ignore(s.ws(["worktree", "lint", "--task", "Bad_Task"]))
  @fs.rmdir(t4, recursive=true)
  @fs.rmdir("\{s.tasks}/Bad_Task", recursive=true)
  ignore(s.ws(["lint"]))
  inspect(
    s.transcript.to_string(),
    content=(
      #|[t2] myrepo--c1
      #|  種類: dir-branch-mismatch
      #|  内容: 現在のブランチ 'other' の slug が名前と異なります
      #|  fix: 手動対応: ブランチを戻すか worktree を作り直す
      #|  結果: 手動対応が必要です
      #|[t2] myrepo--c2
      #|  種類: dir-branch-mismatch
      #|  内容: detached HEAD です(作業用 worktree はブランチ固定)
      #|  fix: 手動対応: ブランチを checkout する
      #|  結果: 手動対応が必要です
      #|[t2] myrepo--c3
      #|  種類: upstream-mismatch
      #|  内容: upstream が別名ブランチ 'origin/c1' を指しています
      #|  fix: git branch --unset-upstream を実行
      #|  結果: 修正しました
      #|
      #|3 件の問題(未解決 2 件)
      #|rc=1
      #|c3 upstream rc=128
      #|[t3] _readonly/myrepo--b1
      #|  種類: readonly
      #|  内容: ブランチ 'tmpb' を checkout しています
      #|  fix: 手動対応: detached に戻す(git switch --detach)
      #|[t3] _readonly/myrepo--b1
      #|  種類: readonly
      #|  内容: origin に無いコミットが 1 件あります
      #|  fix: 手動対応: 内容を確認する
      #|[t3] _readonly/myrepo--b1
      #|  種類: readonly
      #|  内容: 未コミット変更があります
      #|  fix: 手動対応: 内容を確認する
      #|
      #|3 件の問題
      #|rc=1
      #|[t4] _readonly/zzz
      #|  種類: naming
      #|  内容: `<repo>--<slug>` 形式のディレクトリ名ではありません
      #|  fix: 手動対応: 名前を確認し、不要なら削除
      #|  結果: 手動対応が必要です
      #|[t4] myrepo--d9
      #|  種類: naming
      #|  内容: repo 部分 'myrepo' と実際の bare repo($S/root/repos/other.git)が一致しません
      #|  fix: 手動対応: ディレクトリ名を実際の repo に合わせる
      #|  結果: 手動対応が必要です
      #|[t4] myrepo--d9
      #|  種類: dir-branch-mismatch
      #|  内容: 現在のブランチ 'd2' の slug が名前と異なります
      #|  fix: 手動対応: ブランチを戻すか worktree を作り直す
      #|  結果: 手動対応が必要です
      #|[t4] myrepo--ghost
      #|  種類: broken-worktree
      #|  内容: git が worktree として認識しません
      #|  fix: 手動対応: 不要なら削除し、必要なら ws worktree add で作り直す
      #|  結果: 手動対応が必要です
      #|[t4] random-dir
      #|  種類: naming
      #|  内容: `<repo>--<slug>` 形式のディレクトリ名ではありません
      #|  fix: 手動対応: 名前を確認し、不要なら削除
      #|  結果: 手動対応が必要です
      #|[t4] myrepo--d1
      #|  種類: broken-worktree
      #|  内容: git に登録されていますがディレクトリがありません: $S/root/tasks/t4/myrepo--d1
      #|  fix: git worktree prune を実行
      #|  結果: 修正しました
      #|[t4] other--d2
      #|  種類: broken-worktree
      #|  内容: git に登録されていますがディレクトリがありません: $S/root/tasks/t4/other--d2
      #|  fix: git worktree prune を実行
      #|  結果: 修正しました
      #|
      #|7 件の問題(未解決 5 件)
      #|rc=1
      #|! ws: task 名は [a-z0-9-] のみ(先頭末尾・連続ハイフン不可): 'Bad_Task'
      #|rc=1
      #|[t2] myrepo--c1
      #|  種類: dir-branch-mismatch
      #|  内容: 現在のブランチ 'other' の slug が名前と異なります
      #|  fix: 手動対応: ブランチを戻すか worktree を作り直す
      #|[t2] myrepo--c2
      #|  種類: dir-branch-mismatch
      #|  内容: detached HEAD です(作業用 worktree はブランチ固定)
      #|  fix: 手動対応: ブランチを checkout する
      #|[t3] _readonly/myrepo--b1
      #|  種類: readonly
      #|  内容: ブランチ 'tmpb' を checkout しています
      #|  fix: 手動対応: detached に戻す(git switch --detach)
      #|[t3] _readonly/myrepo--b1
      #|  種類: readonly
      #|  内容: origin に無いコミットが 1 件あります
      #|  fix: 手動対応: 内容を確認する
      #|[t3] _readonly/myrepo--b1
      #|  種類: readonly
      #|  内容: 未コミット変更があります
      #|  fix: 手動対応: 内容を確認する
      #|
      #|5 件の問題
      #|rc=1
      #|
    ),
  )
}

///|
async test "lint scopes" {
  let s = Sandbox::new()
  defer s.cleanup()
  s.make_remote("myrepo", ["a1", "b1", "c1"])
  for task in ["ta", "tb"] {
    s.setup_ws(["new", task])
  }
  s.setup_ws([
    "worktree", "add", "--task", "ta", "--repo", "myrepo", "--branch", "a1",
  ])
  s.setup_ws([
    "worktree", "add", "--task", "tb", "--repo", "myrepo", "--branch", "b1",
  ])
  let bare = "\{s.ws_root}/repos/myrepo.git"
  let stray = "\{s.root}/outside/p/stray"
  ignore(s.git(bare, ["worktree", "add", "--detach", stray, "origin/c1"]))
  @fs.rmdir("\{s.tasks}/ta/myrepo--a1", recursive=true)
  @fs.rmdir("\{s.tasks}/tb/myrepo--b1", recursive=true)
  @fs.rmdir(stray, recursive=true)
  ignore(s.ws(["worktree", "lint", "--task", "ta"]))
  ignore(s.ws(["worktree", "lint", "--task", "tb"]))
  ignore(s.ws(["lint"]))
  ignore(s.ws(["lint", "--fix"]))
  ignore(s.ws(["lint"]))
  inspect(
    s.transcript.to_string(),
    content=(
      #|[ta] myrepo--a1
      #|  種類: broken-worktree
      #|  内容: git に登録されていますがディレクトリがありません: $S/root/tasks/ta/myrepo--a1
      #|  fix: git worktree prune を実行
      #|
      #|1 件の問題
      #|rc=1
      #|[tb] myrepo--b1
      #|  種類: broken-worktree
      #|  内容: git に登録されていますがディレクトリがありません: $S/root/tasks/tb/myrepo--b1
      #|  fix: git worktree prune を実行
      #|
      #|1 件の問題
      #|rc=1
      #|[-] stray
      #|  種類: broken-worktree
      #|  内容: git に登録されていますがディレクトリがありません: $S/outside/p/stray
      #|  fix: git worktree prune を実行
      #|[ta] myrepo--a1
      #|  種類: broken-worktree
      #|  内容: git に登録されていますがディレクトリがありません: $S/root/tasks/ta/myrepo--a1
      #|  fix: git worktree prune を実行
      #|[tb] myrepo--b1
      #|  種類: broken-worktree
      #|  内容: git に登録されていますがディレクトリがありません: $S/root/tasks/tb/myrepo--b1
      #|  fix: git worktree prune を実行
      #|
      #|3 件の問題
      #|rc=1
      #|[-] stray
      #|  種類: broken-worktree
      #|  内容: git に登録されていますがディレクトリがありません: $S/outside/p/stray
      #|  fix: git worktree prune を実行
      #|  結果: 修正しました
      #|[ta] myrepo--a1
      #|  種類: broken-worktree
      #|  内容: git に登録されていますがディレクトリがありません: $S/root/tasks/ta/myrepo--a1
      #|  fix: git worktree prune を実行
      #|  結果: 修正しました
      #|[tb] myrepo--b1
      #|  種類: broken-worktree
      #|  内容: git に登録されていますがディレクトリがありません: $S/root/tasks/tb/myrepo--b1
      #|  fix: git worktree prune を実行
      #|  結果: 修正しました
      #|
      #|3 件の問題(未解決 0 件)
      #|rc=0
      #|問題はありません
      #|rc=0
      #|
    ),
  )
}
