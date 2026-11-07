///|
pub struct EnsureResult {
  ok : Bool
  message : String
  exit_code : Int32
}

///|
/// `start_directory` から上へ辿って最初に package.json があるディレクトリ。無ければ `WsError`。
pub async fn find_package_dir(start_directory : String) -> String {
  let mut directory = start_directory
  while !@sys.exists(@sys.join([directory, "package.json"])) {
    let parent = @sys.dirname(directory)
    if parent == directory {
      raise @errors.WsError("package.json が見つかりません")
    }
    directory = parent
  }
  directory
}

///|
/// `<package_dir>/node_modules` をキャッシュから、無ければ `npm ci` で用意してキャッシュに登録する。
/// `npm ci` の失敗は戻り値で、それ以外の失敗は例外で返す。
pub async fn ensure_node_modules(
  package_dir : String,
  npm_args : Array[String],
) -> EnsureResult {
  let lockfile = @sys.join([package_dir, "package-lock.json"])
  if !@sys.exists(lockfile) {
    raise @errors.WsError(
      "\{package_dir} に package-lock.json がありません",
    )
  }
  // 追加引数(--omit=dev 等)は node_modules の中身を変えうるので、キャッシュの利用も登録もしない
  let use_cache = npm_args.length() == 0
  let key = if use_cache { Some(compute_cache_key(package_dir)) } else { None }
  if key is Some(key) && cache_has(key) {
    let cloning = place_from_cache(package_dir, key)
    return {
      ok: true,
      message: "キャッシュから復元しました (\{cloning}): \{package_dir}",
      exit_code: 0,
    }
  }
  let lock_hash_before = @sys.sha256_hex_of_file(lockfile)
  let status = @sys.run_inheriting(
    "npm",
    ["ci", ..npm_args],
    cwd=package_dir,
    env=environment_for(package_dir),
  ) catch {
    @sys.SpawnError(_) => Some(1)
    error => raise error
  }
  if status != Some(0) {
    return {
      ok: false,
      message: "npm ci に失敗しました: \{package_dir}",
      exit_code: status.unwrap_or(1),
    }
  }
  let lock_unchanged = @sys.sha256_hex_of_file(lockfile) == lock_hash_before
  if key is Some(key) &&
    lock_unchanged &&
    @sys.exists(@sys.join([package_dir, "node_modules"])) {
    ignore(register_in_cache(package_dir, key))
    return {
      ok: true,
      message: "npm ci を実行しキャッシュに登録しました: \{package_dir}",
      exit_code: 0,
    }
  }
  {
    ok: true,
    message: "npm ci を実行しました: \{package_dir}",
    exit_code: 0,
  }
}

///|
/// `worktree` で git 管理されている package-lock.json のディレクトリ(node_modules 内を除く)をソートして返す。
pub async fn lockfile_directories(worktree : String) -> Array[String] {
  let directories : Array[String] = []
  for
    file in @sys.split_to_array(
      @git.output(["-C", worktree, "ls-files", "-z"]),
      "\u{0}",
    ) {
    if @sys.basename(file) == "package-lock.json" &&
      !@sys.split_to_array(file, "/").contains("node_modules") {
      directories.push(@sys.join([worktree, @sys.dirname(file)]))
    }
  }
  @sys.sorted(directories)
}

///|
/// `worktree` 内の全 lockfile について node_modules を用意し、結果を表示する。失敗した数を返す。
pub async fn install_node_modules_in_worktree(worktree : String) -> Int32 {
  let mut failed = 0
  for package_dir in lockfile_directories(worktree) {
    let message = try {
      let result = ensure_node_modules(package_dir, [])
      if result.ok {
        @sys.log(result.message)
        continue
      }
      result.message
    } catch {
      error =>
        match @errors.expected_message(error) {
          Some(message) => @sys.trim(message)
          None => raise error
        }
    }
    failed = failed + 1
    @sys.log_error(
      "warn: \{message}\n  再実行: cd \{package_dir} && ws npm ci",
    )
  }
  failed
}
