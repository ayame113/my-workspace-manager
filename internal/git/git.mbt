///|
/// git を実行する。起動できない場合だけ `WsError`。
pub async fn run(args : Array[String]) -> @sys.Captured {
  @sys.run_captured("git", args) catch {
    error => raise @errors.WsError("git を実行できません: \{error}")
  }
}

///|
/// git を実行して stdout を返す。非 0 終了なら git の stderr を持つ `GitError`。
pub async fn output(args : Array[String]) -> String {
  let result = run(args)
  guard result.exit_code == 0 else { raise @errors.GitError(result.stderr) }
  result.stdout
}

///|
pub async fn succeeds(args : Array[String]) -> Bool {
  run(args).exit_code == 0
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
/// detached HEAD なら `None`。
pub async fn current_branch(worktree : String) -> String? {
  let result = run(["-C", worktree, "symbolic-ref", "-q", "--short", "HEAD"])
  if result.exit_code == 0 {
    Some(result.stdout)
  } else {
    None
  }
}

///|
pub async fn short_head(worktree : String) -> String {
  output(["-C", worktree, "rev-parse", "--short", "HEAD"])
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
