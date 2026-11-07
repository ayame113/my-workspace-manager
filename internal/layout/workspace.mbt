///|
let ws_root : String = match @sys.environment_variable("WS_ROOT") {
  Some(root) if root != "" => root
  _ => @sys.join([@sys.environment_variable("HOME").unwrap_or(""), "ws"])
}

///|
/// `$WS_ROOT`。未設定なら `~/ws`。
pub fn root() -> String {
  ws_root
}

///|
pub fn tasks_root() -> String {
  @sys.join([ws_root, "tasks"])
}

///|
pub fn repos_root() -> String {
  @sys.join([ws_root, "repos"])
}

///|
pub fn task_dir(task : String) -> String {
  @sys.join([ws_root, "tasks", task])
}

///|
pub fn bare_path(repo : String) -> String {
  @sys.join([ws_root, "repos", "\{repo}.git"])
}

///|
async fn task_from_current_directory() -> String {
  let tasks_root = tasks_root()
  let outside = @errors.WsError(
    "カレントディレクトリが \{tasks_root}/<task>/ 配下ではありません",
  )
  if !@sys.exists(tasks_root) {
    raise outside
  }
  let cwd = @sys.current_directory()
  let relative = @sys.relative(
    @sys.realpath(tasks_root),
    @sys.realpath(cwd),
    cwd~,
  )
  if relative == "" || relative.has_prefix("..") || relative.has_prefix("/") {
    raise outside
  }
  @sys.split_to_array(relative, "/")[0]
}

///|
/// `--task` を解決する。省略または `.` ならカレントディレクトリが属する task を使い、省略時に task 外なら
/// エラーに `hint_when_outside` を添える。名前が不正か task ディレクトリが無ければ `WsError`。
pub async fn resolve_existing_task(
  task_option : String?,
  hint_when_outside : String,
) -> String {
  let task = match task_option {
    None | Some(".") =>
      task_from_current_directory() catch {
        @errors.WsError(message) if task_option is None =>
          raise @errors.WsError("\{message}。\{hint_when_outside}")
        error => raise error
      }
    Some(task) => task
  }
  validate_task_name(task)
  if !@sys.exists(task_dir(task)) {
    raise @errors.WsError(
      "task '\{task}' がありません。先に 'ws new \{task}' を実行してください",
    )
  }
  task
}

///|
/// bare repo のパスを返す(repo の設定も適用する)。存在しなければ `WsError`。
pub async fn require_bare(repo : String) -> String {
  validate_repo_name(repo)
  let bare = bare_path(repo)
  if !@sys.exists(bare) {
    raise @errors.WsError(
      "bare repo '\{repo}' がありません。別ターミナルで次を実行してください:\n  ws clone <url> \{repo}",
    )
  }
  @git.apply_repo_config(bare)
  bare
}

///|
pub fn refuse_inside_agent(what : String) -> Unit raise @errors.WsError {
  // ssh 鍵のパスフレーズ入力待ちでエージェントがハングするのを防ぐ
  if @sys.environment_variable("CLAUDECODE") is Some(value) && value != "" {
    raise @errors.WsError(
      "\{what} は ssh のパスフレーズ入力が必要なため、エージェントからは実行できません。" +
      "ユーザーに別のターミナルで実行してもらってください",
    )
  }
}

///|
pub async fn is_worktree(dir : String) -> Bool {
  @sys.exists(@sys.join([dir, ".git"]))
}

///|
/// `dir` 直下の worktree ディレクトリ名をソートして返す。`dir` が無ければ空。
pub async fn list_worktree_names(dir : String) -> Array[String] {
  if !@sys.exists(dir) {
    return []
  }
  let names : Array[String] = []
  for name in @sys.subdirectory_names(dir) {
    if is_worktree(@sys.join([dir, name])) {
      names.push(name)
    }
  }
  names
}
