///|
async test "lint node_modules states and unreferenced caches" {
  let log = Transcript::new()
  let fixture = NpmFixture::new()
  let sandbox = fixture.sandbox
  defer sandbox.cleanup()
  let tasks = fixture.tasks
  for branch in ["b1", "b2", "b3", "b4"] {
    ignore(
      fixture.ws([
        "add", "--task", "t1", "--repo", "npmrepo", "--branch", branch,
      ]),
    )
  }
  log.record(fixture.ws(["lint", "--task", "t1"]))

  ignore(sandbox.must("npm", ["ci"], cwd="\{tasks}/t1/npmrepo--b3"))
  log.record(fixture.ws(["lint", "--task", "t1"]))
  log.record(fixture.ws(["lint", "--task", "t1", "--fix"]))
  log.record(
    fixture.marker("t1/npmrepo--b3") == fixture.marker("t1/npmrepo--b1"),
  )
  log.record(fixture.ws(["lint", "--task", "t1"]))

  let lockfile = "\{tasks}/t1/npmrepo--b2/package-lock.json"
  let original = @fs.read_text(lockfile)
  sandbox.write(
    lockfile,
    original
    .replace_all(old="\"version\": \"1.0.0\",", new="\"version\": \"1.0.1\",")
    .replace_all(old="dep-a-1.0.0", new="dep-a-9.9.9"),
  )
  log.record(fixture.ws(["lint", "--task", "t1"]))
  sandbox.write(lockfile, original)
  log.record(fixture.ws(["lint", "--task", "t1"]))

  @fs.rmdir("\{tasks}/t1/npmrepo--b1/bin/node_modules", recursive=true)
  log.record(fixture.ws(["lint", "--task", "t1"]))
  ignore(fixture.ws(["npm", "ci"], cwd="\{tasks}/t1/npmrepo--b1/bin"))

  ignore(
    sandbox.git("\{sandbox.ws_root}/repos/npmrepo.git", [
      "worktree",
      "remove",
      "--force",
      "\{tasks}/t1/npmrepo--b4",
    ]),
  )
  log.record(fixture.ws(["lint", "--task", "t1"]))
  log.record(fixture.ws(["lint", "--all"]))
  log.record(fixture.ws(["lint", "--all", "--fix"]))
  log.record(wait_until_trash_is_empty(sandbox))
  log.record(fixture.cache_entries())
  log.record(fixture.ws(["lint", "--all"]))
  inspect(
    log.text(),
    content=(
      #|問題はありません
      #|rc=0
      #|[t1] npmrepo--b3
      #|  種類: node_modules
      #|  内容: .: 置き換え可能(共有キャッシュ未使用)
      #|  fix: キャッシュにあればクローンに置換、無ければ登録して目印を置く(実行中のプロセスがあると壊れる)
      #|
      #|1 件の問題
      #|rc=1
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
      #|true
      #|問題はありません
      #|rc=0
      #|[t1] npmrepo--b2
      #|  種類: node_modules
      #|  内容: .: node_modules が package-lock.json と一致しません
      #|  fix: 手動対応: cd $S/root/tasks/t1/npmrepo--b2 && ws npm ci
      #|
      #|1 件の問題
      #|rc=1
      #|問題はありません
      #|rc=0
      #|[t1] npmrepo--b1
      #|  種類: node_modules
      #|  内容: bin: node_modules がありません
      #|  fix: 手動対応: cd $S/root/tasks/t1/npmrepo--b1/bin && ws npm ci
      #|
      #|1 件の問題
      #|rc=1
      #|問題はありません
      #|rc=0
      #|[-] <sha256>-abi<node>
      #|  種類: unreferenced-cache
      #|  内容: どの worktree の目印からも参照されていないキャッシュです
      #|  fix: キャッシュを削除(trash 経由)
      #|
      #|1 件の問題
      #|rc=1
      #|注意: --fix の node_modules 置換は、実行中のプロセス(dev server 等)があると壊れます
      #|
      #|[-] <sha256>-abi<node>
      #|  種類: unreferenced-cache
      #|  内容: どの worktree の目印からも参照されていないキャッシュです
      #|  fix: キャッシュを削除(trash 経由)
      #|  結果: 修正しました
      #|
      #|1 件の問題(未解決 0 件)
      #|rc=0
      #|0
      #|<sha256>-abi<node> <sha256>-abi<node>
      #|問題はありません
      #|rc=0
      #|
    ),
  )
}

///|
async test "lint worktree rules" {
  let log = Transcript::new()
  let fixture = NpmFixture::new()
  let sandbox = fixture.sandbox
  defer sandbox.cleanup()
  let tasks = fixture.tasks
  let add = async fn(task : String, args : Array[String]) {
    let outcome = sandbox.ws(
      ["add", "--task", task, "--repo", "npmrepo", "--no-install", ..args],
    )
    if outcome.code != 0 {
      fail(outcome.render())
    }
  }

  ignore(sandbox.ws(["new", "t2"]))
  for branch in ["c1", "c2", "c3"] {
    add("t2", ["--branch", branch])
  }
  ignore(
    sandbox.git("\{tasks}/t2/npmrepo--c1", ["switch", "-q", "-c", "other"]),
  )
  ignore(sandbox.git("\{tasks}/t2/npmrepo--c2", ["switch", "-q", "--detach"]))
  ignore(
    sandbox.git("\{tasks}/t2/npmrepo--c3", [
      "branch", "-q", "--set-upstream-to=origin/c1",
    ]),
  )
  log.record(fixture.ws(["lint", "--task", "t2"]))
  ignore(sandbox.git("\{tasks}/t2/npmrepo--c1", ["switch", "-q", "c1"]))
  ignore(
    sandbox.git("\{tasks}/t2/npmrepo--c1", ["branch", "-q", "-D", "other"]),
  )
  ignore(sandbox.git("\{tasks}/t2/npmrepo--c2", ["switch", "-q", "c2"]))
  let fixed = sandbox.ws(["lint", "--task", "t2", "--fix"])
  log.record(
    fixed.stdout
    .split("\n")
    .filter(line => line.contains("結果"))
    .map(line => line.to_owned())
    .collect()
    .join("\n"),
  )
  log.record(
    sandbox.exec("git", [
      "-C",
      "\{tasks}/t2/npmrepo--c3",
      "rev-parse",
      "--abbrev-ref",
      "@{u}",
    ]).code,
  )

  ignore(sandbox.ws(["new", "t3"]))
  add("t3", ["--readonly", "--branch", "b1"])
  let readonly_worktree = "\{tasks}/t3/_readonly/npmrepo--b1"
  ignore(sandbox.git(readonly_worktree, ["switch", "-q", "-c", "tmpb"]))
  sandbox.write(
    "\{readonly_worktree}/package.json",
    @fs.read_text("\{readonly_worktree}/package.json") + "c\n",
  )
  ignore(sandbox.git(readonly_worktree, ["commit", "-qam", "c"]))
  sandbox.write("\{readonly_worktree}/.gitignore", "node_modules\nd\n")
  log.record(fixture.ws(["lint", "--task", "t3"]))

  make_npm_repo(sandbox, "nvrepo")
  ignore(sandbox.ws(["clone", "file://\{sandbox.root}/remotes/nvrepo.git"]))
  ignore(sandbox.ws(["new", "t4"]))
  add("t4", ["--branch", "d1"])
  let d2 = sandbox.ws([
    "add", "--task", "t4", "--repo", "nvrepo", "--no-install", "--branch", "d2",
  ])
  log.record(d2.code)
  @fs.rmdir("\{tasks}/t4/npmrepo--d1", recursive=true)
  @fs.mkdir("\{tasks}/t4/npmrepo--ghost")
  sandbox.write("\{tasks}/t4/npmrepo--ghost/file", "x\n")
  @fs.mkdir("\{tasks}/t4/random-dir")
  @fs.mkdir("\{tasks}/t4/_readonly/zzz", recursive=true)
  sandbox.write("\{tasks}/t4/afile", "f\n")
  @fs.mkdir("\{tasks}/Bad_Task")
  @fs.rename("\{tasks}/t4/nvrepo--d2", "\{tasks}/t4/npmrepo--d9")
  log.record(fixture.ws(["lint", "--task", "t4"]))
  log.record(fixture.ws(["lint", "--task", "Bad_Task"]))
  let fixed = sandbox.ws(["lint", "--task", "t4", "--fix"])
  log.record(
    fixed.stdout
    .split("\n")
    .filter(line => line.contains("修正しました"))
    .count(),
  )
  log.record(
    sandbox.ws(["lint", "--task", "t4"]).stdout
    .split("\n")
    .filter(line => line.contains("種類: broken-worktree"))
    .count(),
  )

  let stale = "\{sandbox.ws_root}/.cache/node_modules/deadbeef-abi1"
  @fs.mkdir(stale, recursive=true)
  sandbox.write("\{stale}/meta.json", "{}")
  log.record(
    sandbox.ws(["lint", "--all"]).stdout.contains("unreferenced-cache"),
  )
  @fs.rmdir("\{tasks}/t4", recursive=true)
  @fs.rmdir("\{tasks}/Bad_Task", recursive=true)
  log.record(fixture.ws(["lint", "--all"]))
  inspect(
    log.text(),
    content=(
      #|[t2] npmrepo--c1
      #|  種類: dir-branch-mismatch
      #|  内容: 現在のブランチ 'other' の slug が名前と異なります
      #|  fix: 手動対応: ブランチを戻すか worktree を作り直す
      #|[t2] npmrepo--c1
      #|  種類: node_modules
      #|  内容: .: node_modules がありません
      #|  fix: 手動対応: cd $S/root/tasks/t2/npmrepo--c1 && ws npm ci
      #|[t2] npmrepo--c1
      #|  種類: node_modules
      #|  内容: bin: node_modules がありません
      #|  fix: 手動対応: cd $S/root/tasks/t2/npmrepo--c1/bin && ws npm ci
      #|[t2] npmrepo--c2
      #|  種類: dir-branch-mismatch
      #|  内容: detached HEAD です(作業用 worktree はブランチ固定)
      #|  fix: 手動対応: ブランチを checkout する
      #|[t2] npmrepo--c2
      #|  種類: node_modules
      #|  内容: .: node_modules がありません
      #|  fix: 手動対応: cd $S/root/tasks/t2/npmrepo--c2 && ws npm ci
      #|[t2] npmrepo--c2
      #|  種類: node_modules
      #|  内容: bin: node_modules がありません
      #|  fix: 手動対応: cd $S/root/tasks/t2/npmrepo--c2/bin && ws npm ci
      #|[t2] npmrepo--c3
      #|  種類: upstream-mismatch
      #|  内容: upstream が別名ブランチ 'origin/c1' を指しています
      #|  fix: git branch --unset-upstream を実行
      #|[t2] npmrepo--c3
      #|  種類: node_modules
      #|  内容: .: node_modules がありません
      #|  fix: 手動対応: cd $S/root/tasks/t2/npmrepo--c3 && ws npm ci
      #|[t2] npmrepo--c3
      #|  種類: node_modules
      #|  内容: bin: node_modules がありません
      #|  fix: 手動対応: cd $S/root/tasks/t2/npmrepo--c3/bin && ws npm ci
      #|
      #|9 件の問題
      #|rc=1
      #|  結果: 手動対応が必要です
      #|  結果: 手動対応が必要です
      #|  結果: 手動対応が必要です
      #|  結果: 手動対応が必要です
      #|  結果: 修正しました
      #|  結果: 手動対応が必要です
      #|  結果: 手動対応が必要です
      #|128
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
      #|0
      #|[t4] _readonly/zzz
      #|  種類: naming
      #|  内容: `<repo>--<slug>` 形式のディレクトリ名ではありません
      #|  fix: 手動対応: 名前を確認し、不要なら削除
      #|[t4] npmrepo--d9
      #|  種類: naming
      #|  内容: repo 部分 'npmrepo' と実際の bare repo($S/root/repos/nvrepo.git)が一致しません
      #|  fix: 手動対応: ディレクトリ名を実際の repo に合わせる
      #|[t4] npmrepo--d9
      #|  種類: dir-branch-mismatch
      #|  内容: 現在のブランチ 'd2' の slug が名前と異なります
      #|  fix: 手動対応: ブランチを戻すか worktree を作り直す
      #|[t4] npmrepo--d9
      #|  種類: node_modules
      #|  内容: .: node_modules がありません
      #|  fix: 手動対応: cd $S/root/tasks/t4/npmrepo--d9 && ws npm ci
      #|[t4] npmrepo--d9
      #|  種類: node_modules
      #|  内容: bin: node_modules がありません
      #|  fix: 手動対応: cd $S/root/tasks/t4/npmrepo--d9/bin && ws npm ci
      #|[t4] npmrepo--ghost
      #|  種類: broken-worktree
      #|  内容: .git がありません
      #|  fix: 手動対応: 不要なら削除し、必要なら ws add で作り直す
      #|[t4] random-dir
      #|  種類: naming
      #|  内容: `<repo>--<slug>` 形式のディレクトリ名ではありません
      #|  fix: 手動対応: 名前を確認し、不要なら削除
      #|[t4] npmrepo--d1
      #|  種類: broken-worktree
      #|  内容: git に登録されていますがディレクトリがありません: $S/root/tasks/t4/npmrepo--d1
      #|  fix: git worktree prune を実行
      #|[t4] nvrepo--d2
      #|  種類: broken-worktree
      #|  内容: git に登録されていますがディレクトリがありません: $S/root/tasks/t4/nvrepo--d2
      #|  fix: git worktree prune を実行
      #|
      #|9 件の問題
      #|rc=1
      #|! ws: task 名は [a-z0-9-] のみ(先頭末尾・連続ハイフン不可): 'Bad_Task'
      #|rc=1
      #|2
      #|2
      #|false
      #|[t2] npmrepo--c1
      #|  種類: node_modules
      #|  内容: .: node_modules がありません
      #|  fix: 手動対応: cd $S/root/tasks/t2/npmrepo--c1 && ws npm ci
      #|[t2] npmrepo--c1
      #|  種類: node_modules
      #|  内容: bin: node_modules がありません
      #|  fix: 手動対応: cd $S/root/tasks/t2/npmrepo--c1/bin && ws npm ci
      #|[t2] npmrepo--c2
      #|  種類: node_modules
      #|  内容: .: node_modules がありません
      #|  fix: 手動対応: cd $S/root/tasks/t2/npmrepo--c2 && ws npm ci
      #|[t2] npmrepo--c2
      #|  種類: node_modules
      #|  内容: bin: node_modules がありません
      #|  fix: 手動対応: cd $S/root/tasks/t2/npmrepo--c2/bin && ws npm ci
      #|[t2] npmrepo--c3
      #|  種類: node_modules
      #|  内容: .: node_modules がありません
      #|  fix: 手動対応: cd $S/root/tasks/t2/npmrepo--c3 && ws npm ci
      #|[t2] npmrepo--c3
      #|  種類: node_modules
      #|  内容: bin: node_modules がありません
      #|  fix: 手動対応: cd $S/root/tasks/t2/npmrepo--c3/bin && ws npm ci
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
      #|[-] deadbeef-abi1
      #|  種類: unreferenced-cache
      #|  内容: どの worktree の目印からも参照されていないキャッシュです
      #|  fix: キャッシュを削除(trash 経由)
      #|
      #|12 件の問題
      #|rc=1
      #|
    ),
  )
}
