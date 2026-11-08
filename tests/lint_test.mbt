///|
async test "lint node_modules states and unreferenced caches" {
  let s = Sandbox::new()
  defer s.cleanup()
  s.setup_npm()
  let t1 = "\{s.tasks}/t1"
  for branch in ["b1", "b2", "b3", "b4"] {
    s.setup_ws(["add", "--task", "t1", "--repo", "npmrepo", "--branch", branch])
  }
  let lint = (args : Array[String]) => s.ws(["lint", ..args])
  ignore(lint(["--task", "t1"]))
  ignore(s.must("npm", ["ci"], cwd="\{t1}/npmrepo--b3"))
  ignore(lint(["--task", "t1", "--fix"]))
  s.note("shared: \{s.marker("t1/npmrepo--b3") == s.marker("t1/npmrepo--b1")}")
  let lockfile = "\{t1}/npmrepo--b2/package-lock.json"
  let original = @fs.read_text(lockfile)
  write(lockfile, original.replace_all(old="1.0.0", new="9.9.9"))
  ignore(lint(["--task", "t1"]))
  write(lockfile, original)
  @fs.rmdir("\{t1}/npmrepo--b1/bin/node_modules", recursive=true)
  ignore(lint(["--task", "t1"]))
  ignore(s.exec(ws_binary.wait(), ["npm", "ci"], cwd="\{t1}/npmrepo--b1/bin"))
  ignore(
    s.git("\{s.ws_root}/repos/npmrepo.git", [
      "worktree",
      "remove",
      "--force",
      "\{t1}/npmrepo--b4",
    ]),
  )
  ignore(lint(["--task", "t1"]))
  ignore(lint(["--all", "--fix"]))
  s.note("cache entries: \{s.cache_entries()}")
  ignore(lint(["--all"]))
  inspect(
    s.transcript.to_string(),
    content=(
      #|問題はありません
      #|rc=0
      #|注意: --fix の node_modules 置換は、実行中のプロセス(dev server 等)があると壊れます
      #|
      #|[t1] npmrepo--b3
      #|  種類: node_modules
      #|  内容: .: 置き換え可能(共有キャッシュ未使用)
      #|  fix: キャッシュにあればクローンに置換、無ければ登録して目印を置く(実行中のプロセスがあると壊れる)
      #|  結果: 修正しました
      #|
      #|1 件の問題(未解決 0 件)
      #|rc=0
      #|shared: true
      #|[t1] npmrepo--b2
      #|  種類: node_modules
      #|  内容: .: node_modules が package-lock.json と一致しません
      #|  fix: 手動対応: cd $S/root/tasks/t1/npmrepo--b2 && ws npm ci
      #|
      #|1 件の問題
      #|rc=1
      #|[t1] npmrepo--b1
      #|  種類: node_modules
      #|  内容: bin: node_modules がありません
      #|  fix: 手動対応: cd $S/root/tasks/t1/npmrepo--b1/bin && ws npm ci
      #|
      #|1 件の問題
      #|rc=1
      #|問題はありません
      #|rc=0
      #|注意: --fix の node_modules 置換は、実行中のプロセス(dev server 等)があると壊れます
      #|
      #|[-] <sha256>-abi<node>
      #|  種類: unreferenced-cache
      #|  内容: どの worktree の目印からも参照されていないキャッシュです
      #|  fix: キャッシュを削除
      #|  結果: 修正しました
      #|
      #|1 件の問題(未解決 0 件)
      #|rc=0
      #|cache entries: 2
      #|問題はありません
      #|rc=0
      #|
    ),
  )
}

///|
async test "lint worktree rules" {
  let s = Sandbox::new()
  defer s.cleanup()
  s.setup_npm()
  let add = async fn(task : String, args : Array[String]) {
    s.setup_ws(["add", "--task", task, "--no-install", ..args])
  }
  s.setup_ws(["new", "t2"])
  for branch in ["c1", "c2", "c3"] {
    add("t2", ["--repo", "npmrepo", "--branch", branch])
  }
  let t2 = "\{s.tasks}/t2"
  ignore(s.git("\{t2}/npmrepo--c1", ["switch", "-q", "-c", "other"]))
  ignore(s.git("\{t2}/npmrepo--c2", ["switch", "-q", "--detach"]))
  ignore(
    s.git("\{t2}/npmrepo--c3", ["branch", "-q", "--set-upstream-to=origin/c1"]),
  )
  ignore(s.ws(["lint", "--task", "t2", "--fix"]))
  s.note(
    "c3 upstream rc=\{s.exec("git", ["-C", "\{t2}/npmrepo--c3", "rev-parse", "@{u}"]).code}",
  )

  s.setup_ws(["new", "t3"])
  add("t3", ["--repo", "npmrepo", "--readonly", "--branch", "b1"])
  let readonly_worktree = "\{s.tasks}/t3/_readonly/npmrepo--b1"
  ignore(s.git(readonly_worktree, ["switch", "-q", "-c", "tmpb"]))
  write("\{readonly_worktree}/.gitignore", "node_modules\nchanged\n")
  ignore(s.git(readonly_worktree, ["commit", "-qam", "c"]))
  write("\{readonly_worktree}/.gitignore", "node_modules\ndirty\n")
  ignore(s.ws(["lint", "--task", "t3"]))

  s.make_npm_remote("nvrepo")
  s.setup_ws(["new", "t4"])
  add("t4", ["--repo", "npmrepo", "--branch", "d1"])
  add("t4", ["--repo", "nvrepo", "--branch", "d2"])
  let t4 = "\{s.tasks}/t4"
  @fs.rmdir("\{t4}/npmrepo--d1", recursive=true)
  @fs.mkdir("\{t4}/npmrepo--ghost")
  @fs.mkdir("\{t4}/random-dir")
  @fs.mkdir("\{t4}/_readonly/zzz", recursive=true)
  @fs.mkdir("\{s.tasks}/Bad_Task")
  @fs.rename("\{t4}/nvrepo--d2", "\{t4}/npmrepo--d9")
  let stale = "\{s.ws_root}/.cache/node_modules/deadbeef-abi1"
  @fs.mkdir(stale, recursive=true)
  write("\{stale}/meta.json", "{}")
  ignore(s.ws(["lint", "--task", "t4", "--fix"]))
  ignore(s.ws(["lint", "--task", "Bad_Task"]))
  s.note(
    "unreferenced reported while unscannable: \{s.exec(ws_binary.wait(), ["lint", "--all"]).stdout.contains("unreferenced-cache")}",
  )
  @fs.rmdir(t4, recursive=true)
  s.note(
    "unreferenced reported: \{s.exec(ws_binary.wait(), ["lint", "--all"]).stdout.contains("deadbeef-abi1")}",
  )
  inspect(
    s.transcript.to_string(),
    content=(
      #|注意: --fix の node_modules 置換は、実行中のプロセス(dev server 等)があると壊れます
      #|
      #|[t2] npmrepo--c1
      #|  種類: dir-branch-mismatch
      #|  内容: 現在のブランチ 'other' の slug が名前と異なります
      #|  fix: 手動対応: ブランチを戻すか worktree を作り直す
      #|  結果: 手動対応が必要です
      #|[t2] npmrepo--c1
      #|  種類: node_modules
      #|  内容: .: node_modules がありません
      #|  fix: 手動対応: cd $S/root/tasks/t2/npmrepo--c1 && ws npm ci
      #|  結果: 手動対応が必要です
      #|[t2] npmrepo--c1
      #|  種類: node_modules
      #|  内容: bin: node_modules がありません
      #|  fix: 手動対応: cd $S/root/tasks/t2/npmrepo--c1/bin && ws npm ci
      #|  結果: 手動対応が必要です
      #|[t2] npmrepo--c2
      #|  種類: dir-branch-mismatch
      #|  内容: detached HEAD です(作業用 worktree はブランチ固定)
      #|  fix: 手動対応: ブランチを checkout する
      #|  結果: 手動対応が必要です
      #|[t2] npmrepo--c2
      #|  種類: node_modules
      #|  内容: .: node_modules がありません
      #|  fix: 手動対応: cd $S/root/tasks/t2/npmrepo--c2 && ws npm ci
      #|  結果: 手動対応が必要です
      #|[t2] npmrepo--c2
      #|  種類: node_modules
      #|  内容: bin: node_modules がありません
      #|  fix: 手動対応: cd $S/root/tasks/t2/npmrepo--c2/bin && ws npm ci
      #|  結果: 手動対応が必要です
      #|[t2] npmrepo--c3
      #|  種類: upstream-mismatch
      #|  内容: upstream が別名ブランチ 'origin/c1' を指しています
      #|  fix: git branch --unset-upstream を実行
      #|  結果: 修正しました
      #|[t2] npmrepo--c3
      #|  種類: node_modules
      #|  内容: .: node_modules がありません
      #|  fix: 手動対応: cd $S/root/tasks/t2/npmrepo--c3 && ws npm ci
      #|  結果: 手動対応が必要です
      #|[t2] npmrepo--c3
      #|  種類: node_modules
      #|  内容: bin: node_modules がありません
      #|  fix: 手動対応: cd $S/root/tasks/t2/npmrepo--c3/bin && ws npm ci
      #|  結果: 手動対応が必要です
      #|
      #|9 件の問題(未解決 8 件)
      #|rc=1
      #|c3 upstream rc=128
      #|[t3] _readonly/npmrepo--b1
      #|  種類: readonly
      #|  内容: ブランチ 'tmpb' を checkout しています
      #|  fix: 手動対応: detached に戻す(git switch --detach)
      #|[t3] _readonly/npmrepo--b1
      #|  種類: readonly
      #|  内容: origin に無いコミットが 1 件あります
      #|  fix: 手動対応: 内容を確認する
      #|[t3] _readonly/npmrepo--b1
      #|  種類: readonly
      #|  内容: 未コミット変更があります
      #|  fix: 手動対応: 内容を確認する
      #|[t3] _readonly/npmrepo--b1
      #|  種類: node_modules
      #|  内容: .: node_modules がありません
      #|  fix: 手動対応: cd $S/root/tasks/t3/_readonly/npmrepo--b1 && ws npm ci
      #|[t3] _readonly/npmrepo--b1
      #|  種類: node_modules
      #|  内容: bin: node_modules がありません
      #|  fix: 手動対応: cd $S/root/tasks/t3/_readonly/npmrepo--b1/bin && ws npm ci
      #|
      #|5 件の問題
      #|rc=1
      #|注意: --fix の node_modules 置換は、実行中のプロセス(dev server 等)があると壊れます
      #|
      #|[t4] _readonly/zzz
      #|  種類: naming
      #|  内容: `<repo>--<slug>` 形式のディレクトリ名ではありません
      #|  fix: 手動対応: 名前を確認し、不要なら削除
      #|  結果: 手動対応が必要です
      #|[t4] npmrepo--d9
      #|  種類: naming
      #|  内容: repo 部分 'npmrepo' と実際の bare repo($S/root/repos/nvrepo.git)が一致しません
      #|  fix: 手動対応: ディレクトリ名を実際の repo に合わせる
      #|  結果: 手動対応が必要です
      #|[t4] npmrepo--d9
      #|  種類: dir-branch-mismatch
      #|  内容: 現在のブランチ 'd2' の slug が名前と異なります
      #|  fix: 手動対応: ブランチを戻すか worktree を作り直す
      #|  結果: 手動対応が必要です
      #|[t4] npmrepo--d9
      #|  種類: node_modules
      #|  内容: .: node_modules がありません
      #|  fix: 手動対応: cd $S/root/tasks/t4/npmrepo--d9 && ws npm ci
      #|  結果: 手動対応が必要です
      #|[t4] npmrepo--d9
      #|  種類: node_modules
      #|  内容: bin: node_modules がありません
      #|  fix: 手動対応: cd $S/root/tasks/t4/npmrepo--d9/bin && ws npm ci
      #|  結果: 手動対応が必要です
      #|[t4] npmrepo--ghost
      #|  種類: broken-worktree
      #|  内容: git が worktree として認識しません
      #|  fix: 手動対応: 不要なら削除し、必要なら ws add で作り直す
      #|  結果: 手動対応が必要です
      #|[t4] random-dir
      #|  種類: naming
      #|  内容: `<repo>--<slug>` 形式のディレクトリ名ではありません
      #|  fix: 手動対応: 名前を確認し、不要なら削除
      #|  結果: 手動対応が必要です
      #|[t4] npmrepo--d1
      #|  種類: broken-worktree
      #|  内容: git に登録されていますがディレクトリがありません: $S/root/tasks/t4/npmrepo--d1
      #|  fix: git worktree prune を実行
      #|  結果: 修正しました
      #|[t4] nvrepo--d2
      #|  種類: broken-worktree
      #|  内容: git に登録されていますがディレクトリがありません: $S/root/tasks/t4/nvrepo--d2
      #|  fix: git worktree prune を実行
      #|  結果: 修正しました
      #|
      #|9 件の問題(未解決 7 件)
      #|rc=1
      #|! ws: task 名は [a-z0-9-] のみ(先頭末尾・連続ハイフン不可): 'Bad_Task'
      #|rc=1
      #|unreferenced reported while unscannable: false
      #|unreferenced reported: true
      #|
    ),
  )
}
