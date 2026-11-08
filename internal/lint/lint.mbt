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
/// 1 つの worktree についての問題を `issues` に記録する関数を返す。
fn reporter(
  issues : Array[Issue],
  task : String,
  worktree : String,
) -> (String, String, String, (async () -> Unit)?) -> Unit {
  (kind, message, fix, action) => {
    issues.push({ task, worktree, kind, message, fix, action, })
  }
}

///|
/// git が worktree として認識すれば、その gitdir(`<bare>/worktrees/<name>`)を返す。
async fn read_gitdir(worktree : String) -> String? {
  // .git が無いディレクトリでは、親ディレクトリの repo を拾わないようにする
  guard @sys.exists("\{worktree}/.git") else { return None }
  let result = @git.run(["-C", worktree, "rev-parse", "--absolute-git-dir"])
  if result.exit_code == 0 {
    Some(result.stdout)
  } else {
    None
  }
}

///|
async fn realpath_or_none(path : String) -> String? {
  Some(@fs.realpath(path)) catch {
    _ => None
  }
}

///|
async fn lint_worktree(
  issues : Array[Issue],
  task : String,
  directory : String,
  name : String,
  read_only~ : Bool,
) -> Unit {
  let report = reporter(
    issues,
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
    return
  }
  guard read_gitdir(directory) is Some(gitdir) else {
    manual(
      "broken-worktree", "git が worktree として認識しません", "不要なら削除し、必要なら ws add で作り直す",
    )
    return
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
}

///|
async fn lint_task(issues : Array[Issue], task : String) -> Unit {
  let directory = @layout.task_dir(task)
  if !@layout.is_valid_task_name(task) {
    reporter(issues, task, "-")(
      "naming",
      "task 名が規則([a-z0-9-]、連続・先頭末尾ハイフン不可)に違反しています",
      "手動対応: task 名の変更はしない方針なので内容を確認する",
      None,
    )
  }
  for name in @sys.subdirectory_names(directory) {
    if name != "_readonly" {
      lint_worktree(issues, task, "\{directory}/\{name}", name, read_only=false)
      continue
    }
    for readonly_name in @sys.subdirectory_names("\{directory}/_readonly") {
      lint_worktree(
        issues,
        task,
        "\{directory}/_readonly/\{readonly_name}",
        readonly_name,
        read_only=true,
      )
    }
  }
}

///|
async fn lint_dangling_registrations(issues : Array[Issue]) -> Unit {
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
      reporter(issues, task, @path.Path(worktree).basename().to_owned())(
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
/// 検査して結果を表示し、`fix` なら自動修正する。`task` が `None` なら全 task と未参照キャッシュを検査する。
/// 未解決の問題が無ければ 0、あれば 1 を返す。
pub async fn run(task : String?, fix : Bool) -> Int32 {
  let issues : Array[Issue] = []
  match task {
    Some(task) => lint_task(issues, task)
    None =>
      if @sys.exists(@layout.tasks_root()) {
        for task in @sys.subdirectory_names(@layout.tasks_root()) {
          lint_task(issues, task)
        }
      }
  }
  lint_dangling_registrations(issues)
  if issues.is_empty() {
    @sys.log("問題はありません")
    return 0
  }
  let mut unresolved = 0
  for issue in issues {
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
  @sys.log("\n\{issues.length()} 件の問題\{summary}")
  if unresolved == 0 {
    0
  } else {
    1
  }
}
