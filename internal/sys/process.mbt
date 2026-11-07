///|
/// 子プロセスを起動できなかった。メッセージは Node.js と同じ形式(`spawnSync git ENOENT` 等)。
pub(all) suberror SpawnError {
  SpawnError(String)
}

///|
pub struct Captured {
  exit_code : Int32
  stdout : String
  stderr : String
}

///|
pub fn environment() -> Map[String, String] {
  @env.get_env_vars()
}

///|
pub fn environment_variable(name : String) -> String? {
  @env.get_env_var(name)
}

///|
pub fn current_directory() -> String {
  @env.current_dir().unwrap_or(".")
}

///|
async fn is_executable_file(path : String) -> Bool {
  (@fs.kind(path, follow_symlink=true) == Regular && @fs.can_execute(path)) catch {
    _ => false
  }
}

///|
// Node.js(libuv)と同じく子に渡す環境の PATH で探す。posix_spawnp に任せると ws 自身の PATH で探してしまう
async fn resolve_command(
  command : String,
  env : Map[String, String]?,
) -> String {
  if command.contains("/") {
    return command
  }
  let search_path = match env {
    Some(variables) => variables.get("PATH")
    None => environment_variable("PATH")
  }
  for directory in search_path.unwrap_or("/usr/bin:/bin").split(":") {
    let directory = if directory == "" { "." } else { directory.to_owned() }
    let candidate = directory + "/" + command
    if is_executable_file(candidate) {
      return candidate
    }
  }
  raise SpawnError("spawnSync \{command} ENOENT")
}

///|
fn spawn_error(command : String, error : Error) -> SpawnError {
  let code = match error {
    @os_error.OsError(NotFound, ..) => "ENOENT"
    @os_error.OsError(AccessDenied, ..) => "EACCES"
    other => other.to_string()
  }
  SpawnError("spawnSync \{command} \{code}")
}

///|
/// stdin を /dev/null にして実行し、stdout と stderr を UTF-8(不正バイトは置換)で集める。
/// `env` を渡すと環境変数をすべて置き換える。シグナルで終了した場合の終了コードは負。
pub async fn run_captured(
  command : String,
  args : Array[String],
  cwd? : String,
  env? : Map[String, String],
) -> Captured {
  let executable = resolve_command(command, env)
  let result = @process.run(
    executable,
    args,
    inherit_env=env is None,
    extra_env=env.unwrap_or({}),
    stdin=Empty,
    stdout=Collect,
    stderr=Collect,
    cwd?=cwd.map(directory => directory[:]),
  ) catch {
    error => raise spawn_error(command, error)
  }
  {
    exit_code: result.exit_code,
    stdout: @utf8.decode_lossy(result.stdout_binary()),
    stderr: @utf8.decode_lossy(result.stderr_binary()),
  }
}

///|
/// stdio を継承して実行し、終了コードを返す。シグナルで終了した場合は `None`。
pub async fn run_inheriting(
  command : String,
  args : Array[String],
  cwd? : String,
  env? : Map[String, String],
) -> Int32? {
  let executable = resolve_command(command, env)
  let result = @process.run(
    executable,
    args,
    inherit_env=env is None,
    extra_env=env.unwrap_or({}),
    cwd?=cwd.map(directory => directory[:]),
  ) catch {
    error => raise spawn_error(command, error)
  }
  if result.exit_code < 0 {
    None
  } else {
    Some(result.exit_code)
  }
}

///|
/// stdio をすべて /dev/null にして実行し、終了コード 0 で終わったかを返す。起動できなければ false。
pub async fn run_silently(command : String, args : Array[String]) -> Bool {
  let executable = resolve_command(command, None) catch { _ => return false }
  let result = @process.run(
    executable,
    args,
    stdin=Empty,
    stdout=Discard,
    stderr=Discard,
  ) catch {
    _ => return false
  }
  result.exit_code == 0
}

///|
pub fn exit(code : Int32) -> Unit {
  @env.exit(code)
}
