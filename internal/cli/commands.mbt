///|
async fn cmd_new(task : String) -> Int32 {
  @layout.validate_task_name(task)
  let dir = @layout.task_dir(task)
  if @sys.exists(dir) {
    @sys.log("task '\{task}' は既に存在します: \{dir}")
  } else {
    @fs.mkdir(dir, recursive=true)
    @sys.log("作成しました: \{dir}")
  }
  0
}

///|
async fn cmd_clone(url : String, repo_option : String?) -> Int32 {
  @layout.refuse_inside_agent("ws clone")
  let repo = repo_option.unwrap_or_else(() => {
    let name = url
      .trim_end(chars="/")
      .to_owned()
      .split("/")
      .last()
      .unwrap_or("")
    name.strip_suffix(".git").unwrap_or(name).to_owned()
  })
  if repo == "" {
    raise @errors.WsError("URL から repo 名を導出できません")
  }
  @layout.validate_repo_name(repo)
  let bare = @layout.bare_path(repo)
  if @sys.exists(bare) {
    raise @errors.WsError("\{bare} は既に存在します")
  }
  @fs.mkdir(@layout.repos_root(), recursive=true, allow_exist=true)
  // git clone --bare はリモートの全ブランチをローカルブランチ化し、add の「ローカルブランチ存在判定」を狂わせる
  ignore(@git.output(["init", "--quiet", "--bare", bare]))
  @git.apply_repo_config(bare)
  ignore(@git.output(["-C", bare, "remote", "add", "origin", url]))
  ignore(
    @git.output([
      "-C", bare, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*",
    ]),
  )
  ignore(@git.output(["-C", bare, "fetch", "--quiet", "origin"]))
  @sys.log("作成しました: \{bare}")
  0
}

///|
fn missing_branch_error(branch : String) -> @errors.WsError {
  @errors.WsError(
    "ローカルに '\{branch}' も 'origin/\{branch}' もありません。" +
    "リモートにあるはずなら、別ターミナルで ws worktree pull --task <task> か git fetch が必要です。" +
    "新規ブランチなら --branch <branch> --create --base <start-point> を使ってください",
  )
}

///|
async fn cmd_add(
  task : String,
  repo : String,
  branch : String,
  base~ : String?,
  read_only~ : Bool,
) -> Int32 {
  let bare = @layout.require_bare(repo)
  let slug = @layout.slug_or_raise(branch)
  let dir = if read_only {
    "\{@layout.task_dir(task)}/_readonly"
  } else {
    @layout.task_dir(task)
  }
  let worktree = "\{dir}/\{repo}--\{slug}"
  let add = async fn(args : Array[String]) {
    ignore(@git.output(["-C", bare, "worktree", "add", ..args]))
  }
  let remote_exists = @git.ref_exists(bare, "refs/remotes/origin/\{branch}")
  let local_exists = @git.ref_exists(bare, "refs/heads/\{branch}")
  if read_only {
    guard remote_exists else { raise missing_branch_error(branch) }
    @fs.mkdir(dir, recursive=true, allow_exist=true)
    add(["--detach", worktree, "origin/\{branch}"])
  } else if base is Some(base) {
    if local_exists {
      raise @errors.WsError(
        "ローカルブランチ '\{branch}' は既に存在します",
      )
    }
    // 解決できない名前を渡すと git worktree add は -b を無視し、origin の同名ブランチをローカルに作ってしまう
    if !@git.succeeds([
        "-C",
        bare,
        "rev-parse",
        "--verify",
        "--quiet",
        "\{base}^{commit}",
      ]) {
      raise @errors.WsError(
        "--base '\{base}' を解決できません(origin/main のように明示してください)",
      )
    }
    add(["--no-track", "-b", branch, worktree, base])
  } else if local_exists {
    add([worktree, branch])
  } else if remote_exists {
    add(["--track", "-b", branch, worktree, "origin/\{branch}"])
  } else {
    raise missing_branch_error(branch)
  }
  @sys.log("作成しました: \{worktree}")
  0
}

///|
async fn print_worktrees(dir : String, kind : String) -> Unit {
  for name in @layout.list_worktree_names(dir) {
    let worktree = "\{dir}/\{name}"
    let head = match @git.current_branch(worktree) {
      Some(branch) => branch
      None => "detached@\{@git.short_head(worktree)}"
    }
    let state = if @git.is_dirty(worktree) { "dirty" } else { "clean" }
    @sys.log(
      "\{kind.pad_end(6, ' ')} \{name.pad_end(55, ' ')} \{head.pad_end(45, ' ')} \{state}",
    )
  }
}

///|
async fn cmd_worktree_ls(task : String) -> Int32 {
  print_worktrees(@layout.task_dir(task), "work")
  print_worktrees("\{@layout.task_dir(task)}/_readonly", "ro")
  0
}

///|
async fn count_dirty(dir : String) -> Int32 {
  let mut dirty = 0
  for name in @layout.list_worktree_names(dir) {
    if @git.is_dirty("\{dir}/\{name}") {
      dirty += 1
    }
  }
  dirty
}

///|
async fn cmd_ls() -> Int32 {
  if !@sys.exists(@layout.tasks_root()) {
    return 0
  }
  let rows = []
  for task in @sys.subdirectory_names(@layout.tasks_root()) {
    let dir = @layout.task_dir(task)
    let work = @layout.list_worktree_names(dir).length()
    let read_only_count = @layout.list_worktree_names("\{dir}/_readonly").length()
    let dirty = count_dirty(dir) + count_dirty("\{dir}/_readonly")
    let readonly_note = if read_only_count == 0 {
      ""
    } else {
      " (\{read_only_count} readonly)"
    }
    let dirty_note = if dirty == 0 { "clean" } else { "\{dirty} dirty" }
    rows.push(
      (task, "\{work + read_only_count} worktrees\{readonly_note}", dirty_note),
    )
  }
  let task_width = rows
    .iter()
    .map(row => row.0.length())
    .fold(init=0, Int32::max)
  let count_width = rows
    .iter()
    .map(row => row.1.length())
    .fold(init=0, Int32::max)
  for row in rows {
    @sys.log(
      "\{row.0.pad_end(task_width + 3, ' ')}\{row.1.pad_end(count_width + 3, ' ')}\{row.2}",
    )
  }
  0
}
