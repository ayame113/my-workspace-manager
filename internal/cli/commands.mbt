///|
async fn cmd_new(args : Array[String]) -> Int32 {
  let task = parse_command_args(args, [], 1, 1).positionals[0]
  @layout.validate_task_name(task)
  let dir = @layout.task_dir(task)
  if @sys.exists(dir) {
    @sys.log("task '\{task}' は既に存在します: \{dir}")
    return 0
  }
  @sys.make_directory(dir, recursive=true)
  @sys.log("作成しました: \{dir}")
  0
}

///|
fn strip_trailing_slashes(url : String) -> String {
  let mut end = url.length()
  while end > 0 && url.code_unit_at(end - 1) == '/' {
    end = end - 1
  }
  url.exact_view(end~).to_owned()
}

///|
fn repo_name_from_url(url : String) -> String {
  let name = @sys.basename(strip_trailing_slashes(url))
  if name.has_suffix(".git") {
    name.exact_view(end=name.length() - 4).to_owned()
  } else {
    name
  }
}

///|
async fn cmd_clone(args : Array[String]) -> Int32 {
  @layout.refuse_inside_agent("ws clone")
  let positionals = parse_command_args(args, [], 1, 2).positionals
  let url = positionals[0]
  let repo = if positionals.length() > 1 {
    positionals[1]
  } else {
    repo_name_from_url(url)
  }
  if repo == "" {
    raise @errors.WsError("URL から repo 名を導出できません")
  }
  @layout.validate_repo_name(repo)
  let bare = @layout.bare_path(repo)
  if @sys.exists(bare) {
    raise @errors.WsError("\{bare} は既に存在します")
  }
  @sys.make_directory(@layout.repos_root(), recursive=true)
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
fn missing_remote_branch_error(branch : String) -> @errors.WsError {
  @errors.WsError(
    "ローカルに '\{branch}' も 'origin/\{branch}' もありません。" +
    "リモートにあるはずなら、別ターミナルで ws pull --task <task> か git fetch が必要です。" +
    "新規ブランチなら --branch <branch> --create --base <start-point> を使ってください",
  )
}

///|
async fn cmd_add(args : Array[String]) -> Int32 {
  let parsed = parse_command_args(
    args,
    [
      text_option("task"),
      text_option("repo"),
      text_option("branch"),
      flag_option("create", short='c'),
      text_option("base"),
      flag_option("readonly"),
      flag_option("no-install"),
    ],
    0,
    0,
  )
  let creating = parsed.flag("create")
  let base = parsed.text("base")
  let read_only = parsed.flag("readonly")
  guard parsed.text("repo") is Some(repo) &&
    parsed.text("branch") is Some(branch) else {
    raise @errors.UsageError
  }
  if creating != (base is Some(_)) {
    raise @errors.UsageError
  }
  if read_only && creating {
    raise @errors.UsageError
  }

  let task = @layout.resolve_existing_task(
    parsed.text("task"),
    "--task <task> を指定してください",
  )
  let bare = @layout.require_bare(repo)
  let slug = @layout.slug_or_raise(branch)
  let dir = if read_only {
    @sys.join([@layout.task_dir(task), "_readonly"])
  } else {
    @layout.task_dir(task)
  }
  let worktree = @sys.join([dir, "\{repo}--\{slug}"])

  if read_only {
    if !@git.ref_exists(bare, "refs/remotes/origin/\{branch}") {
      raise missing_remote_branch_error(branch)
    }
    @sys.make_directory(dir, recursive=true)
    ignore(
      @git.output([
        "-C",
        bare,
        "worktree",
        "add",
        "--detach",
        worktree,
        "origin/\{branch}",
      ]),
    )
  } else if base is Some(base) {
    if base.has_prefix("-") {
      raise @errors.WsError(
        "--base が '-' で始まっています: '\{base}'",
      )
    }
    if @git.ref_exists(bare, "refs/heads/\{branch}") {
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
    ignore(
      @git.output([
        "-C", bare, "worktree", "add", "--no-track", "-b", branch, worktree, base,
      ]),
    )
  } else if @git.ref_exists(bare, "refs/heads/\{branch}") {
    ignore(@git.output(["-C", bare, "worktree", "add", worktree, branch]))
  } else if @git.ref_exists(bare, "refs/remotes/origin/\{branch}") {
    ignore(
      @git.output([
        "-C",
        bare,
        "worktree",
        "add",
        "--track",
        "-b",
        branch,
        worktree,
        "origin/\{branch}",
      ]),
    )
  } else {
    raise missing_remote_branch_error(branch)
  }
  @sys.log("作成しました: \{worktree}")
  if parsed.flag("no-install") {
    return 0
  }
  if @cache.install_node_modules_in_worktree(worktree) == 0 {
    0
  } else {
    1
  }
}

///|
async fn cmd_npm(args : Array[String]) -> Int32 {
  if args.length() == 0 || args[0] != "ci" {
    raise @errors.UsageError
  }
  let rest = args[1:].to_owned()
  let (own, npm_args) = match rest.search("--") {
    Some(separator) =>
      (rest[:separator].to_owned(), rest[separator + 1:].to_owned())
    None => (rest, [])
  }
  ignore(parse_command_args(own, [], 0, 0))
  let package_dir = @cache.find_package_dir(
    @sys.realpath(@sys.current_directory()),
  )
  let result = @cache.ensure_node_modules(package_dir, npm_args)
  if result.ok {
    @sys.log(result.message)
    return 0
  }
  @sys.log_error(result.message)
  result.exit_code
}

///|
async fn cmd_lint(args : Array[String]) -> Int32 {
  let parsed = parse_command_args(
    args,
    [text_option("task"), flag_option("all"), flag_option("fix")],
    0,
    0,
  )
  let all = parsed.flag("all")
  if all && parsed.text("task") is Some(_) {
    raise @errors.UsageError
  }
  let scope : @lint.Scope = if all {
    AllTasks
  } else {
    Task(
      @layout.resolve_existing_task(
        parsed.text("task"),
        "--task <task> か --all を指定してください",
      ),
    )
  }
  @lint.run(scope, parsed.flag("fix"))
}

///|
async fn print_worktrees(dir : String, kind : String) -> Unit {
  for name in @layout.list_worktree_names(dir) {
    let worktree = @sys.join([dir, name])
    let state = if @git.is_dirty(worktree) { "dirty" } else { "clean" }
    @sys.log(
      "\{kind.pad_end(6, ' ')} \{name.pad_end(55, ' ')} \{@git.describe_head(worktree).pad_end(45, ' ')} \{state}",
    )
  }
}

///|
async fn print_task_worktrees(task : String) -> Unit {
  let dir = @layout.task_dir(task)
  print_worktrees(dir, "work")
  print_worktrees(@sys.join([dir, "_readonly"]), "ro")
}

///|
async fn cmd_ls(args : Array[String]) -> Int32 {
  let parsed = parse_command_args(
    args,
    [text_option("task"), flag_option("all")],
    0,
    0,
  )
  let all = parsed.flag("all")
  if all && parsed.text("task") is Some(_) {
    raise @errors.UsageError
  }
  if !all {
    print_task_worktrees(
      @layout.resolve_existing_task(
        parsed.text("task"),
        "--task <task> か --all を指定してください",
      ),
    )
    return 0
  }
  let tasks_dir = @layout.tasks_root()
  if !@sys.exists(tasks_dir) {
    return 0
  }
  for task in @sys.subdirectory_names(tasks_dir) {
    @sys.log("[\{task}]")
    print_task_worktrees(task)
  }
  0
}
