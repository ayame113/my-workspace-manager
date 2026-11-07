///|
priv struct Issue {
  task : String
  worktree : String
  kind : String
  message : String
  fix : String
  action : (async () -> Unit)?
}

///|
priv struct Context {
  issues : Array[Issue]
  referenced_keys : Set[String]
  mut unscannable : Int32
}

///|
/// 1 つの worktree についての問題を記録する関数を返す。
fn Context::reporter(
  self : Context,
  task : String,
  worktree : String,
) -> (String, String, String, (async () -> Unit)?) -> Unit {
  (kind, message, fix, action) => {
    self.issues.push({ task, worktree, kind, message, fix, action, })
  }
}

///|
async fn lint_node_modules(
  context : Context,
  report : (String, String, String, (async () -> Unit)?) -> Unit,
  worktree : String,
) -> Unit {
  for package_dir in @cache.lockfile_directories(worktree) {
    let location = match package_dir.strip_prefix(worktree + "/") {
      Some(relative) => relative.to_owned()
      None => "."
    }
    let marker = @cache.read_marker(package_dir)
    if marker is Some(key) {
      context.referenced_keys.add(key)
    }
    let rerun = "手動対応: cd \{package_dir} && ws npm ci"
    if !@sys.exists("\{package_dir}/node_modules") {
      report(
        "node_modules",
        "\{location}: node_modules がありません",
        rerun,
        None,
      )
      continue
    }
    if !node_modules_matches_lockfile(package_dir) {
      report(
        "node_modules",
        "\{location}: node_modules が package-lock.json と一致しません",
        rerun,
        None,
      )
      continue
    }
    let key = @cache.compute_cache_key(package_dir) catch {
      @errors.WsError(message) => {
        report(
          "node_modules",
          "\{location}: \{message}",
          "手動対応: node のバージョン設定(nodenv)を確認する",
          None,
        )
        continue
      }
      error => raise error
    }
    if marker == Some(key) {
      continue
    }
    let reason = if marker is None {
      "共有キャッシュ未使用"
    } else {
      "目印のキーが古い"
    }
    report(
      "node_modules",
      "\{location}: 置き換え可能(\{reason})",
      "キャッシュにあればクローンに置換、無ければ登録して目印を置く(実行中のプロセスがあると壊れる)",
      Some(async fn() {
        let key = @cache.compute_cache_key(package_dir)
        if @cache.cache_has(key) {
          ignore(@cache.place_from_cache(package_dir, key))
        } else {
          @cache.register_in_cache(package_dir, key)
        }
      }),
    )
  }
}

///|
/// worktree として扱えない理由。問題が無ければ gitdir を返す。
async fn read_gitdir(worktree : String) -> Result[String, String] {
  let git_path = "\{worktree}/.git"
  if !@sys.exists(git_path) {
    return Err(".git がありません")
  }
  if @sys.is_directory(git_path) {
    return Err(
      ".git がディレクトリです(worktree ではなく独立した repo)",
    )
  }
  let gitdir = @fs.read_text(git_path)
    .split("\n")
    .find_first(line => line.has_prefix("gitdir:"))
  guard gitdir is Some(line) else {
    return Err(".git ファイルの内容が不正です")
  }
  let gitdir = line["gitdir:".length():].trim().to_owned()
  if !@sys.exists(gitdir) {
    return Err("gitdir が存在しません: \{gitdir}")
  }
  if !@git.succeeds(["-C", worktree, "rev-parse", "--git-dir"]) {
    return Err("git が worktree として認識しません")
  }
  Ok(gitdir)
}

///|
async fn realpath_or_none(path : String) -> String? {
  Some(@fs.realpath(path)) catch {
    _ => None
  }
}

///|
async fn lint_worktree(
  context : Context,
  task : String,
  directory : String,
  name : String,
  read_only~ : Bool,
) -> Unit {
  let report = context.reporter(
    task,
    if read_only {
      "_readonly/\{name}"
    } else {
      name
    },
  )
  let manual = (kind : String, message : String, fix : String) => {
    report(kind, message, "手動対応: \{fix}", None)
  }
  guard @layout.parse_worktree_name(name) is Some(parsed) else {
    manual(
      "naming", "`<repo>--<slug>` 形式のディレクトリ名ではありません",
      "名前を確認し、不要なら削除",
    )
    context.unscannable += 1
    return
  }
  let gitdir = match read_gitdir(directory) {
    Ok(gitdir) => gitdir
    Err(problem) => {
      manual(
        "broken-worktree", problem, "不要なら削除し、必要なら ws add で作り直す",
      )
      context.unscannable += 1
      return
    }
  }
  let actual_bare = realpath_or_none(
    @path.Path(gitdir).dirname().dirname().to_string(),
  )
  if actual_bare is None ||
    actual_bare != realpath_or_none(@layout.bare_path(parsed.repo)) {
    manual(
      "naming",
      "repo 部分 '\{parsed.repo}' と実際の bare repo(\{actual_bare.unwrap_or("不明")})が一致しません",
      "ディレクトリ名を実際の repo に合わせる",
    )
  }
  let branch = @git.current_branch(directory)
  if read_only {
    if branch is Some(branch) {
      manual(
        "readonly",
        "ブランチ '\{branch}' を checkout しています",
        "detached に戻す(git switch --detach)",
      )
    }
    let unpushed = @string.parse_int(
      @git.output([
        "-C", directory, "rev-list", "--count", "HEAD", "--not", "--remotes=origin",
      ]),
    ) catch {
      _ => 0
    }
    if unpushed > 0 {
      manual(
        "readonly",
        "origin に無いコミットが \{unpushed} 件あります",
        "内容を確認する",
      )
    }
    if @git.is_dirty(directory) {
      manual(
        "readonly", "未コミット変更があります", "内容を確認する",
      )
    }
  } else if branch is Some(branch) {
    if @layout.branch_slug(branch) != parsed.slug {
      manual(
        "dir-branch-mismatch",
        "現在のブランチ '\{branch}' の slug が名前と異なります",
        "ブランチを戻すか worktree を作り直す",
      )
    }
    let upstream = @git.run([
      "-C", directory, "rev-parse", "--abbrev-ref", "@{upstream}",
    ])
    if upstream.exit_code == 0 && upstream.stdout != "origin/\{branch}" {
      report(
        "upstream-mismatch",
        "upstream が別名ブランチ '\{upstream.stdout}' を指しています",
        "git branch --unset-upstream を実行",
        Some(async fn() {
          ignore(@git.output(["-C", directory, "branch", "--unset-upstream"]))
        }),
      )
    }
  } else {
    manual(
      "dir-branch-mismatch", "detached HEAD です(作業用 worktree はブランチ固定)",
      "ブランチを checkout する",
    )
  }
  lint_node_modules(context, report, directory)
}

///|
async fn lint_task(context : Context, task : String) -> Unit {
  let directory = @layout.task_dir(task)
  if !@layout.is_valid_task_name(task) {
    context.reporter(task, "-")(
      "naming",
      "task 名が規則([a-z0-9-]、連続・先頭末尾ハイフン不可)に違反しています",
      "手動対応: task 名の変更はしない方針なので内容を確認する",
      None,
    )
  }
  for name in @sys.subdirectory_names(directory) {
    if name != "_readonly" {
      lint_worktree(
        context,
        task,
        "\{directory}/\{name}",
        name,
        read_only=false,
      )
      continue
    }
    for readonly_name in @sys.subdirectory_names("\{directory}/_readonly") {
      lint_worktree(
        context,
        task,
        "\{directory}/_readonly/\{readonly_name}",
        readonly_name,
        read_only=true,
      )
    }
  }
}

///|
async fn lint_dangling_registrations(context : Context) -> Unit {
  let repos = @layout.repos_root()
  if !@sys.exists(repos) {
    return
  }
  for entry in @sys.subdirectory_names(repos) {
    let bare = "\{repos}/\{entry}"
    let listing = @git.run(["-C", bare, "worktree", "list", "--porcelain"])
    if listing.exit_code != 0 {
      continue
    }
    // 先頭は bare repo 自身
    for
      worktree in listing.stdout
      .split("\n")
      .filter_map(line => line.strip_prefix("worktree "))
      .drop(1) {
      let worktree = worktree.to_owned()
      if @sys.exists(worktree) {
        continue
      }
      let task = match worktree.strip_prefix(@layout.tasks_root() + "/") {
        Some(relative) => relative.split("/").next().unwrap().to_owned()
        None => "-"
      }
      context.reporter(task, @path.Path(worktree).basename().to_owned())(
        "broken-worktree",
        "git に登録されていますがディレクトリがありません: \{worktree}",
        "git worktree prune を実行",
        Some(async fn() {
          ignore(@git.output(["-C", bare, "worktree", "prune"]))
        }),
      )
    }
  }
}

///|
async fn lint_trash(context : Context) -> Unit {
  for entry in @cache.stale_trash_entries() {
    context.reporter("-", entry)(
      "trash",
      "バックグラウンド削除が中断された trash の残骸です",
      "削除する",
      Some(async fn() { @sys.remove_tree(entry) }),
    )
  }
}

///|
async fn lint_unreferenced_caches(context : Context) -> Unit {
  if context.unscannable > 0 {
    return
  }
  for key in @cache.cache_entry_names() {
    if !key.has_prefix(".") && !context.referenced_keys.contains(key) {
      context.reporter("-", key)(
        "unreferenced-cache",
        "どの worktree の目印からも参照されていないキャッシュです",
        "キャッシュを削除(trash 経由)",
        Some(async fn() {
          @cache.discard_directory(@cache.cache_entry_path(key))
        }),
      )
    }
  }
}

///|
/// 検査して結果を表示し、`fix` なら自動修正する。`task` が `None` なら全 task と未参照キャッシュを検査する。
/// 未解決の問題が無ければ 0、あれば 1 を返す。
pub async fn run(task : String?, fix : Bool) -> Int32 {
  let context : Context = {
    issues: [],
    referenced_keys: Set([]),
    unscannable: 0,
  }
  match task {
    Some(task) => lint_task(context, task)
    None => {
      if @sys.exists(@layout.tasks_root()) {
        for task in @sys.subdirectory_names(@layout.tasks_root()) {
          lint_task(context, task)
        }
      }
      lint_unreferenced_caches(context)
    }
  }
  lint_dangling_registrations(context)
  // trash は全 task 共通で task の走査結果にも依存しないので、--task 指定時も検査する
  lint_trash(context)
  if context.issues.is_empty() {
    @sys.log("問題はありません")
    return 0
  }
  if fix {
    @sys.log(
      "注意: --fix の node_modules 置換は、実行中のプロセス(dev server 等)があると壊れます\n",
    )
  }
  let mut unresolved = 0
  for issue in context.issues {
    @sys.log(
      "[\{issue.task}] \{issue.worktree}\n  種類: \{issue.kind}\n  内容: \{issue.message}\n  fix: \{issue.fix}",
    )
    if !fix {
      unresolved += 1
      continue
    }
    let result = match issue.action {
      None => Err("手動対応が必要です")
      Some(action) =>
        try action() catch {
          error =>
            match @errors.expected_message(error) {
              Some(message) =>
                Err("修正に失敗しました: \{message.trim()}")
              None => raise error
            }
        } noraise {
          _ => Ok(())
        }
    }
    match result {
      Ok(_) => @sys.log("  結果: 修正しました")
      Err(message) => {
        @sys.log("  結果: \{message}")
        unresolved += 1
      }
    }
  }
  let summary = if fix { "(未解決 \{unresolved} 件)" } else { "" }
  @sys.log("\n\{context.issues.length()} 件の問題\{summary}")
  if unresolved == 0 {
    0
  } else {
    1
  }
}
