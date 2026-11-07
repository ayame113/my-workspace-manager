// 統合テスト: 実際の ws バイナリをビルドし、一時ディレクトリのローカル file:// リモートに対して実行する。
// ネットワークには触れない(npm は file: 依存 + offline)

///|
let ws_binary : @async.Lazy[String] = @async.Lazy(async fn() {
  match @env.get_env_var("WS_TEST_BIN") {
    Some(binary) if binary != "" => return binary
    _ => ()
  }
  // 実行中の moon test とビルドディレクトリを取り合わないよう、別の target-dir にビルドする
  let target_dir = "_build/integration"
  let result = @process.run(
    "moon",
    ["build", "--target", "native", "--target-dir", target_dir, "cmd/ws"],
    stdout=Collect,
    stderr=DuplicateStdout,
  )
  if result.exit_code != 0 {
    fail("moon build failed:\n\{result.stdout_text()}")
  }
  @fs.realpath("\{target_dir}/native/debug/build/cmd/ws/ws.exe")
})

///|
struct Outcome {
  code : Int32
  stdout : String
  stderr : String
}

///|
/// stdout、`! ` を前置した stderr の各行、終了コードの順に並べる。
fn Outcome::render(self : Outcome) -> String {
  let parts = StringBuilder()
  parts.write_string(self.stdout)
  for line in self.stderr.split("\n") {
    if line != "" {
      parts.write_string("! \{line}\n")
    }
  }
  parts.write_string("rc=\{self.code}")
  parts.to_string()
}

///|
struct Sandbox {
  root : String
  ws_root : String
  env : Map[String, String]
}

///|
async fn Sandbox::new() -> Sandbox {
  let root = @fs.realpath(@fs.tmpdir(prefix="ws-it-"))
  let env = @env.get_env_vars()
  env.remove("CLAUDECODE")
  env.remove("GIT_DIR")
  env.remove("GIT_WORK_TREE")
  let ws_root = "\{root}/root"
  let fixed_date = "2026-01-01T00:00:00Z"
  for
    name, value in {
      "WS_ROOT": ws_root,
      "GIT_CONFIG_NOSYSTEM": "1",
      "GIT_CONFIG_GLOBAL": "\{root}/gitconfig",
      "GIT_AUTHOR_NAME": "t",
      "GIT_AUTHOR_EMAIL": "a@b",
      "GIT_COMMITTER_NAME": "t",
      "GIT_COMMITTER_EMAIL": "a@b",
      "GIT_AUTHOR_DATE": fixed_date,
      "GIT_COMMITTER_DATE": fixed_date,
      "npm_config_audit": "false",
      "npm_config_fund": "false",
      "npm_config_offline": "true",
      "npm_config_update_notifier": "false",
    } {
    env.set(name, value)
  }
  @fs.write_text("\{root}/gitconfig", "[init]\n\tdefaultBranch = review\n")
  { root, ws_root, env, }
}

///|
async fn Sandbox::cleanup(self : Sandbox) -> Unit {
  @fs.rmdir(self.root, recursive=true)
}

///|
fn Sandbox::normalize(self : Sandbox, text : String) -> String {
  let replaced = text.replace_all(old=self.root, new="$S")
  let sha = @string.Regex("[0-9a-f]{64}") catch { _ => panic() }
  let abi = @string.Regex("-abi[0-9]+-[a-z0-9]+-[a-z0-9]+") catch {
    _ => panic()
  }
  let hashed = sha.replace_by(replaced, _ => "<sha256>").to_owned()
  abi.replace_by(hashed, _ => "-abi<node>").to_owned()
}

///|
async fn Sandbox::exec(
  self : Sandbox,
  command : String,
  args : Array[String],
  cwd? : String,
  env? : Map[String, String],
) -> Outcome {
  let result = @process.run(
    command,
    args,
    inherit_env=false,
    extra_env=env.unwrap_or(self.env),
    stdin=Empty,
    stdout=Collect,
    stderr=Collect,
    cwd=cwd.unwrap_or(self.root),
  )
  {
    code: result.exit_code,
    stdout: self.normalize(@utf8.decode_lossy(result.stdout_binary())),
    stderr: self.normalize(@utf8.decode_lossy(result.stderr_binary())),
  }
}

///|
/// ws を実行する。`env_changes` は変数名から新しい値(`None` なら削除)への対応。
async fn Sandbox::ws(
  self : Sandbox,
  args : Array[String],
  cwd? : String,
  env_changes~ : Map[String, String?] = Map([]),
) -> Outcome {
  let env = self.env.copy()
  for name, value in env_changes {
    match value {
      Some(value) => env.set(name, value)
      None => env.remove(name)
    }
  }
  self.exec(ws_binary.wait(), args, cwd?, env~)
}

///|
/// 準備用のコマンドを実行し、失敗したらテストを失敗させる。trim した stdout を返す。
async fn Sandbox::must(
  self : Sandbox,
  command : String,
  args : Array[String],
  cwd? : String,
) -> String {
  let outcome = self.exec(command, args, cwd?)
  if outcome.code != 0 {
    fail("\{command} \{args.join(" ")} failed: \{outcome.stderr}")
  }
  outcome.stdout.trim().to_owned()
}

///|
async fn Sandbox::git(
  self : Sandbox,
  cwd : String,
  args : Array[String],
) -> String {
  self.must("git", ["-C", cwd, ..args])
}

///|
async fn Sandbox::write(
  self : Sandbox,
  path : String,
  content : String,
) -> Unit {
  ignore(self)
  @fs.write_text(path, content, create_mode=CreateOrTruncate)
}

///|
async fn Sandbox::exists(self : Sandbox, path : String) -> Bool {
  ignore(self)
  @fs.exists(path)
}

///|
/// テストごとに出力を 1 つのスナップショットにまとめる(`moon test --update` が 1 回で済むように)。
struct Transcript {
  buffer : StringBuilder
}

///|
fn Transcript::new() -> Transcript {
  { buffer: StringBuilder(), }
}

///|
fn[T : Show] Transcript::record(self : Transcript, value : T) -> Unit {
  self.buffer.write_string("\{value}\n")
}

///|
fn Transcript::text(self : Transcript) -> String {
  self.buffer.to_string()
}
