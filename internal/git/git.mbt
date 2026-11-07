///|
pub struct GitResult {
  ok : Bool
  stdout : String
  stderr : String
}

///|
/// git を実行する。`stdout` は trim 済み。git を起動できない場合だけ `WsError`。
pub async fn run(args : Array[String]) -> GitResult {
  let result = @sys.run_captured("git", args) catch {
    @sys.SpawnError(message) =>
      raise @errors.WsError("git を実行できません: \{message}")
    error => raise error
  }
  {
    ok: result.exit_code == 0,
    stdout: @sys.trim(result.stdout),
    stderr: result.stderr,
  }
}

///|
/// git を実行して trim 済みの stdout を返す。非 0 終了なら git の stderr を持つ `GitError`。
pub async fn output(args : Array[String]) -> String {
  let result = run(args)
  guard result.ok else { raise @errors.GitError(result.stderr) }
  result.stdout
}

///|
pub async fn succeeds(args : Array[String]) -> Bool {
  run(args).ok
}

///|
pub async fn ref_exists(bare : String, refname : String) -> Bool {
  succeeds(["-C", bare, "show-ref", "--verify", "--quiet", refname])
}

///|
pub async fn is_dirty(worktree : String) -> Bool {
  output(["-C", worktree, "status", "--porcelain"]) != ""
}

///|
/// checkout 中のブランチ。detached HEAD なら `None`。
pub async fn current_branch(worktree : String) -> String? {
  let result = run(["-C", worktree, "symbolic-ref", "-q", "--short", "HEAD"])
  if result.ok {
    Some(result.stdout)
  } else {
    None
  }
}

///|
pub async fn describe_head(worktree : String) -> String {
  match current_branch(worktree) {
    Some(branch) => branch
    None =>
      "detached@\{output(["-C", worktree, "rev-parse", "--short", "HEAD"])}"
  }
}

///|
pub async fn is_ancestor(
  worktree : String,
  ancestor : String,
  descendant : String,
) -> Bool {
  succeeds(["-C", worktree, "merge-base", "--is-ancestor", ancestor, descendant])
}

///|
pub async fn apply_repo_config(bare : String) -> Unit {
  // 手で切ったブランチの upstream が base(origin/main 等)になると、エディタの push/sync で base に push してしまう
  ignore(output(["-C", bare, "config", "branch.autoSetupMerge", "simple"]))
}
