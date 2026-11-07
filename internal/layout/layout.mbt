///|
let ws_root : String = match @env.get_env_var("WS_ROOT") {
  Some(root) if root != "" => root
  _ => "\{@env.get_env_var("HOME").unwrap_or("")}/ws"
}

///|
/// `$WS_ROOT`。未設定なら `~/ws`。
pub fn root() -> String {
  ws_root
}

///|
pub fn tasks_root() -> String {
  "\{ws_root}/tasks"
}

///|
pub fn repos_root() -> String {
  "\{ws_root}/repos"
}

///|
pub fn task_dir(task : String) -> String {
  "\{ws_root}/tasks/\{task}"
}

///|
pub fn bare_path(repo : String) -> String {
  "\{ws_root}/repos/\{repo}.git"
}

///|
fn is_slug_char(c : Char) -> Bool {
  c is ('a'..<='z' | '0'..<='9')
}

///|
/// `^[a-z0-9]+(-[a-z0-9]+)*$`。task 名と branch slug の規則。
pub fn is_valid_task_name(name : String) -> Bool {
  name != "" &&
  !name.has_prefix("-") &&
  !name.has_suffix("-") &&
  !name.contains("--") &&
  name.iter().all(c => is_slug_char(c) || c == '-')
}

///|
/// `^[A-Za-z0-9][A-Za-z0-9._-]*$`。
pub fn is_repo_name_shape(name : String) -> Bool {
  let alphanumeric = (c : Char) => c is ('a'..<='z' | 'A'..<='Z' | '0'..<='9')
  name.get_char(0) is Some(first) &&
  alphanumeric(first) &&
  name.iter().all(c => alphanumeric(c) || c is ('.' | '_' | '-'))
}

///|
/// 小文字化し、`[a-z0-9]` 以外の連続を `-` 1 つにして、先頭末尾の `-` を除く。
pub fn branch_slug(branch : String) -> String {
  String::from_iter(
    branch.to_lower().iter().map(c => if is_slug_char(c) { c } else { '-' }),
  )
  .split("-")
  .filter(part => part != "")
  .map(part => part.to_owned())
  .collect()
  .join("-")
}

///|
pub fn slug_or_raise(branch : String) -> String raise @errors.WsError {
  match branch_slug(branch) {
    "" =>
      raise @errors.WsError(
        "ブランチ名 '\{branch}' から slug を作れません",
      )
    slug => slug
  }
}

///|
pub fn validate_task_name(task : String) -> Unit raise @errors.WsError {
  if !is_valid_task_name(task) {
    raise @errors.WsError(
      "task 名は [a-z0-9-] のみ(先頭末尾・連続ハイフン不可): '\{task}'",
    )
  }
}

///|
pub fn validate_repo_name(repo : String) -> Unit raise @errors.WsError {
  if !is_repo_name_shape(repo) || repo.contains("--") {
    raise @errors.WsError(
      "repo 名が不正です('--' を含まず英数字 . _ - のみ): '\{repo}'",
    )
  }
}

///|
pub struct WorktreeName {
  repo : String
  slug : String
}

///|
/// `<repo>--<slug>` を分解する。`--` が無ければ slug は空。
pub fn split_worktree_name(name : String) -> WorktreeName {
  match name.split_once("--") {
    Some((repo, slug)) => { repo: repo.to_owned(), slug: slug.to_owned(), }
    None => { repo: name, slug: "", }
  }
}

///|
/// 両方が命名規則に合うときだけ分解する。
pub fn parse_worktree_name(name : String) -> WorktreeName? {
  let parsed = split_worktree_name(name)
  guard is_repo_name_shape(parsed.repo) && is_valid_task_name(parsed.slug) else {
    return None
  }
  Some(parsed)
}

///|
async fn task_from_current_directory() -> String {
  let tasks_root = tasks_root()
  let outside = @errors.WsError(
    "カレントディレクトリが \{tasks_root}/<task>/ 配下ではありません",
  )
  guard @sys.exists(tasks_root) &&
    @fs.realpath(@env.current_dir().unwrap_or(".")).strip_prefix(
      @fs.realpath(tasks_root) + "/",
    )
    is Some(relative) else {
    raise outside
  }
  relative.split("/").next().unwrap().to_owned()
}

///|
/// `--task` を解決する。省略または `.` ならカレントディレクトリが属する task を使い、
/// 省略時に task 外ならエラーに `hint` を添える。名前が不正か task が無ければ `WsError`。
pub async fn resolve_existing_task(
  task_option : String?,
  hint : String,
) -> String {
  let task = match task_option {
    None | Some(".") =>
      task_from_current_directory() catch {
        @errors.WsError(message) if task_option is None =>
          raise @errors.WsError("\{message}。\{hint}")
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
/// bare repo のパスを返し、repo の設定を適用する。無ければ `WsError`。
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
  if @env.get_env_var("CLAUDECODE") is Some(value) && value != "" {
    raise @errors.WsError(
      "\{what} は ssh のパスフレーズ入力が必要なため、エージェントからは実行できません。ユーザーに別のターミナルで実行してもらってください",
    )
  }
}

///|
/// `dir` 直下の worktree ディレクトリ名。`dir` が無ければ空。
pub async fn list_worktree_names(dir : String) -> Array[String] {
  if !@sys.exists(dir) {
    return []
  }
  let names = []
  for name in @sys.subdirectory_names(dir) {
    if @sys.exists("\{dir}/\{name}/.git") {
      names.push(name)
    }
  }
  names
}
