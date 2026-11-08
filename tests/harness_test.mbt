// 統合テスト: 実際の ws をビルドし、一時ディレクトリのローカル file:// リモートに対して実行する。
// ネットワークには触れない

///|
let ws_binary : @async.Lazy[String] = @async.Lazy(async fn() {
  if @env.get_env_var("WS_TEST_BIN") is Some(binary) && binary != "" {
    return binary
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
/// テスト用の一時ディレクトリ。`ws` の実行結果を記録し、最後に 1 つのスナップショットで検証する。
struct Sandbox {
  root : String
  ws_root : String
  tasks : String
  env : Map[String, String]
  transcript : StringBuilder
}

///|
async fn Sandbox::new() -> Sandbox {
  let root = @fs.realpath(@fs.tmpdir(prefix="ws-it-"))
  let env = @env.get_env_vars()
  env.remove("CLAUDECODE")
  let date = "2026-01-01T00:00:00Z"
  for
    name, value in {
      "WS_ROOT": "\{root}/root",
      "GIT_CONFIG_NOSYSTEM": "1",
      "GIT_CONFIG_GLOBAL": "\{root}/gitconfig",
      "GIT_AUTHOR_NAME": "t",
      "GIT_AUTHOR_EMAIL": "a@b",
      "GIT_COMMITTER_NAME": "t",
      "GIT_COMMITTER_EMAIL": "a@b",
      "GIT_AUTHOR_DATE": date,
      "GIT_COMMITTER_DATE": date,
    } {
    env[name] = value
  }
  @fs.write_text("\{root}/gitconfig", "[init]\n\tdefaultBranch = review\n")
  for dir in ["remotes", "outside/p"] {
    @fs.mkdir("\{root}/\{dir}", recursive=true)
  }
  {
    root,
    ws_root: "\{root}/root",
    tasks: "\{root}/root/tasks",
    env,
    transcript: StringBuilder(),
  }
}

///|
async fn Sandbox::cleanup(self : Sandbox) -> Unit nocancel {
  @async.protect_from_cancel(() => @fs.rmdir(self.root, recursive=true))
}

///|
fn Sandbox::normalize(self : Sandbox, text : String) -> String {
  let patterns = [("空き容量: [^\n]*", "空き容量: <df>")]
  patterns.fold(init=text.replace_all(old=self.root, new="$S"), (text, pattern) => {
    let regex = @string.Regex(pattern.0) catch { _ => panic() }
    regex.replace_by(text, _ => pattern.1).to_owned()
  })
}

///|
fn[T : Show] Sandbox::note(self : Sandbox, value : T) -> Unit {
  self.transcript.write_string("\{value}\n")
}

///|
async fn Sandbox::exec(
  self : Sandbox,
  command : String,
  args : Array[String],
  cwd? : String,
) -> Outcome {
  let result = @process.run(
    command,
    args,
    inherit_env=false,
    extra_env=self.env,
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
/// ws を実行して、stdout・stderr の最初の行・終了コードを記録する。
async fn Sandbox::ws(
  self : Sandbox,
  args : Array[String],
  cwd? : String,
) -> Outcome {
  let outcome = self.exec(ws_binary.wait(), args, cwd?)
  self.transcript.write_string(outcome.stdout)
  if outcome.stderr.split("\n").find_first(line => line != "") is Some(line) {
    self.note("! \{line}")
  }
  self.note("rc=\{outcome.code}")
  outcome
}

///|
/// 記録せずに ws を実行し、失敗したらテストを失敗させる。
async fn Sandbox::setup_ws(self : Sandbox, args : Array[String]) -> Unit {
  let outcome = self.exec(ws_binary.wait(), args)
  if outcome.code != 0 {
    fail("ws \{args.join(" ")} failed: \{outcome.stdout}\{outcome.stderr}")
  }
}

///|
/// 準備用のコマンド。失敗したらテストを失敗させ、trim した stdout を返す。
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
async fn write(path : String, content : String) -> Unit {
  @fs.write_text(path, content, create_mode=CreateOrTruncate)
}

///|
/// ブランチ群を持つ seed repo から bare remote `remotes/<name>.git` を作る。
async fn Sandbox::make_remote(
  self : Sandbox,
  name : String,
  branches : Array[String],
) -> Unit {
  let seed = "\{self.root}/seed-\{name}"
  if !@fs.exists(seed) {
    @fs.mkdir(seed)
    ignore(self.git(seed, ["init", "-q", "."]))
    write("\{seed}/f", "1\n")
    ignore(self.git(seed, ["add", "-A"]))
    ignore(self.git(seed, ["commit", "-qm", "init"]))
  }
  for branch in branches {
    ignore(self.git(seed, ["branch", branch]))
  }
  ignore(
    self.git(seed, [
      "clone",
      "-q",
      "--bare",
      ".",
      "\{self.root}/remotes/\{name}.git",
    ]),
  )
  self.setup_ws(["clone", "file://\{self.root}/remotes/\{name}.git"])
}

///|
/// seed repo の `branch` にコミットして remote に push する。
async fn Sandbox::push_change(
  self : Sandbox,
  name : String,
  branch : String,
) -> Unit {
  let seed = "\{self.root}/seed-\{name}"
  ignore(self.git(seed, ["checkout", "-q", branch]))
  write("\{seed}/f", @fs.read_text("\{seed}/f") + branch + "\n")
  ignore(self.git(seed, ["commit", "-qam", branch]))
  ignore(self.git(seed, ["push", "-q", "../remotes/\{name}.git", branch]))
}
