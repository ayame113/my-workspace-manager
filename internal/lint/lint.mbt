///|
priv struct Issue {
  task : String
  worktree : String
  kind : String
  message : String
  fix_description : String
  action : (async () -> Unit)?
}

///|
priv struct Context {
  issues : Array[Issue]
  referenced_keys : Set[String]
  mut unscannable : Int32
}

///|
fn Context::report(
  self : Context,
  task : String,
  worktree : String,
  kind : String,
  message : String,
  fix_description : String,
  action? : async () -> Unit,
) -> Unit {
  self.issues.push({ task, worktree, kind, message, fix_description, action, })
}

///|
async fn lint_node_modules(
  context : Context,
  task : String,
  worktree_label : String,
  worktree : String,
) -> Unit {
  for package_dir in @cache.lockfile_directories(worktree) {
    let relative = @sys.relative(
      worktree,
      package_dir,
      cwd=@sys.current_directory(),
    )
    let location = if relative == "" { "." } else { relative }
    let marker = @cache.read_marker(package_dir)
    if marker is Some(key) {
      context.referenced_keys.add(key)
    }
    let rerun = "手動対応: cd \{package_dir} && ws npm ci"
    if !@sys.exists(@sys.join([package_dir, "node_modules"])) {
      context.report(
        task,
        worktree_label,
        "node_modules",
        "\{location}: node_modules がありません",
        rerun,
      )
      continue
    }
    if !node_modules_matches_lockfile(package_dir) {
      context.report(
        task,
        worktree_label,
        "node_modules",
        "\{location}: node_modules が package-lock.json と一致しません",
        rerun,
      )
      continue
    }
    let key = @cache.compute_cache_key(package_dir) catch {
      @errors.WsError(message) => {
        context.report(
          task,
          worktree_label,
          "node_modules",
          "\{location}: \{@sys.trim(message)}",
          "手動対応: node のバージョン設定(nodenv)を確認する",
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
    context.report(
      task,
      worktree_label,
      "node_modules",
      "\{location}: 置き換え可能(\{reason})",
      "キャッシュにあればクローンに置換、無ければ登録して目印を置く(実行中のプロセスがあると壊れる)",
      action=async fn() {
        let current_key = @cache.compute_cache_key(package_dir)
        if @cache.cache_has(current_key) {
          ignore(@cache.place_from_cache(package_dir, current_key))
        } else {
          ignore(@cache.register_in_cache(package_dir, current_key))
        }
      },
    )
  }
}

///|
priv enum WorktreeKind {
  Work
  Readonly
}

///|
async fn lint_worktree(
  context : Context,
  task : String,
  directory : String,
  name : String,
  kind : WorktreeKind,
) -> Unit {
  let label = match kind {
    Readonly => "_readonly/\{name}"
    Work => name
  }
  guard @layout.parse_worktree_name(name) is Some(parsed) else {
    context.report(
      task, label, "naming", "`<repo>--<slug>` 形式のディレクトリ名ではありません",
      "手動対応: 名前を確認し、不要なら削除",
    )
    context.unscannable = context.unscannable + 1
    return
  }
  let gitdir = match read_gitdir(directory) {
    Gitdir(gitdir) => gitdir
    Problem(problem) => {
      context.report(
        task, label, "broken-worktree", problem, "手動対応: 不要なら削除し、必要なら ws add で作り直す",
      )
      context.unscannable = context.unscannable + 1
      return
    }
  }
  let actual_bare = realpath_or_none(@sys.dirname(@sys.dirname(gitdir)))
  let expected_bare = realpath_or_none(@layout.bare_path(parsed.repo))
  if actual_bare is None || actual_bare != expected_bare {
    context.report(
      task,
      label,
      "naming",
      "repo 部分 '\{parsed.repo}' と実際の bare repo(\{actual_bare.unwrap_or("不明")})が一致しません",
      "手動対応: ディレクトリ名を実際の repo に合わせる",
    )
  }
  let branch = @git.current_branch(directory)
  match kind {
    Work =>
      match branch {
        None =>
          context.report(
            task, label, "dir-branch-mismatch", "detached HEAD です(作業用 worktree はブランチ固定)",
            "手動対応: ブランチを checkout する",
          )
        Some(branch) => {
          if @layout.branch_slug(branch) != parsed.slug {
            context.report(
              task,
              label,
              "dir-branch-mismatch",
              "現在のブランチ '\{branch}' の slug が名前と異なります",
              "手動対応: ブランチを戻すか worktree を作り直す",
            )
          }
          let upstream = @git.run([
            "-C", directory, "rev-parse", "--abbrev-ref", "@{upstream}",
          ])
          if upstream.ok && upstream.stdout != "origin/\{branch}" {
            context.report(
              task,
              label,
              "upstream-mismatch",
              "upstream が別名ブランチ '\{upstream.stdout}' を指しています",
              "git branch --unset-upstream を実行",
              action=async fn() {
                ignore(
                  @git.output(["-C", directory, "branch", "--unset-upstream"]),
                )
              },
            )
          }
        }
      }
    Readonly => {
      if branch is Some(branch) {
        context.report(
          task,
          label,
          "readonly",
          "ブランチ '\{branch}' を checkout しています",
          "手動対応: detached に戻す(git switch --detach)",
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
        context.report(
          task,
          label,
          "readonly",
          "origin に無いコミットが \{unpushed} 件あります",
          "手動対応: 内容を確認する",
        )
      }
      if @git.is_dirty(directory) {
        context.report(
          task, label, "readonly", "未コミット変更があります", "手動対応: 内容を確認する",
        )
      }
    }
  }
  lint_node_modules(context, task, label, directory)
}

///|
async fn realpath_or_none(target : String) -> String? {
  Some(@sys.realpath(target)) catch {
    _ => None
  }
}

///|
async fn lint_task(context : Context, task : String) -> Unit {
  let directory = @layout.task_dir(task)
  if !@layout.is_valid_task_name(task) {
    context.report(
      task, "-", "naming", "task 名が規則([a-z0-9-]、連続・先頭末尾ハイフン不可)に違反しています",
      "手動対応: task 名の変更はしない方針なので内容を確認する",
    )
  }
  for name in @sys.subdirectory_names(directory) {
    if name != "_readonly" {
      lint_worktree(context, task, @sys.join([directory, name]), name, Work)
      continue
    }
    for readonly_name in @sys.subdirectory_names(@sys.join([directory, name])) {
      lint_worktree(
        context,
        task,
        @sys.join([directory, name, readonly_name]),
        readonly_name,
        Readonly,
      )
    }
  }
}

///|
async fn lint_dangling_registrations(context : Context) -> Unit {
  let repos_dir = @layout.repos_root()
  if !@sys.exists(repos_dir) {
    return
  }
  let tasks_root = @layout.tasks_root()
  for entry in @sys.subdirectory_names(repos_dir) {
    let bare = @sys.join([repos_dir, entry])
    let listing = @git.run(["-C", bare, "worktree", "list", "--porcelain"])
    if !listing.ok {
      continue
    }
    let registered = @sys.split_to_array(listing.stdout, "\n")
      .filter(line => line.has_prefix("worktree "))
      .map(line => @sys.slice_from(line, "worktree ".length()))
    for index, worktree in registered {
      // 先頭は bare repo 自身
      if index == 0 || @sys.exists(worktree) {
        continue
      }
      let relative = @sys.relative(
        tasks_root,
        worktree,
        cwd=@sys.current_directory(),
      )
      let task = if relative.has_prefix("..") {
        "-"
      } else {
        @sys.split_to_array(relative, "/")[0]
      }
      context.report(
        task,
        @sys.basename(worktree),
        "broken-worktree",
        "git に登録されていますがディレクトリがありません: \{worktree}",
        "git worktree prune を実行",
        action=async fn() {
          ignore(@git.output(["-C", bare, "worktree", "prune"]))
        },
      )
    }
  }
}

///|
async fn lint_unreferenced_caches(context : Context) -> Unit {
  if context.unscannable > 0 {
    return
  }
  for key in @cache.cache_entry_names() {
    if key.has_prefix(".") || context.referenced_keys.contains(key) {
      continue
    }
    context.report(
      "-",
      key,
      "unreferenced-cache",
      "どの worktree の目印からも参照されていないキャッシュです",
      "キャッシュを削除(trash 経由)",
      action=async fn() {
        @cache.discard_directory(@cache.cache_entry_path(key))
      },
    )
  }
}

///|
/// `ws lint` の検査範囲。全 task(未参照キャッシュの検査を含む)か、1 つの task。
pub(all) enum Scope {
  AllTasks
  Task(String)
}

///|
/// 検査結果を表示し、`fix` なら自動修正も行う。未解決の問題が無ければ 0、あれば 1 を返す(終了コード)。
pub async fn run(scope : Scope, fix : Bool) -> Int32 {
  let context : Context = {
    issues: [],
    referenced_keys: Set([]),
    unscannable: 0,
  }
  match scope {
    AllTasks => {
      let tasks_root = @layout.tasks_root()
      if @sys.exists(tasks_root) {
        for task in @sys.subdirectory_names(tasks_root) {
          lint_task(context, task)
        }
      }
      lint_unreferenced_caches(context)
    }
    Task(task) => lint_task(context, task)
  }
  lint_dangling_registrations(context)

  if context.issues.length() == 0 {
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
    @sys.log("[\{issue.task}] \{issue.worktree}")
    @sys.log("  種類: \{issue.kind}")
    @sys.log("  内容: \{issue.message}")
    @sys.log("  fix: \{issue.fix_description}")
    if !fix {
      unresolved = unresolved + 1
      continue
    }
    guard issue.action is Some(action) else {
      @sys.log("  結果: 手動対応が必要です")
      unresolved = unresolved + 1
      continue
    }
    action() catch {
      error =>
        match @errors.expected_message(error) {
          Some(message) => {
            @sys.log(
              "  結果: 修正に失敗しました: \{@sys.trim(message)}",
            )
            unresolved = unresolved + 1
            continue
          }
          None => raise error
        }
    }
    @sys.log("  結果: 修正しました")
  }
  let summary = if fix { "(未解決 \{unresolved} 件)" } else { "" }
  @sys.log("\n\{context.issues.length()} 件の問題\{summary}")
  if unresolved == 0 {
    0
  } else {
    1
  }
}
