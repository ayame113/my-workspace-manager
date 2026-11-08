///|
fn cache_root() -> String {
  "\{@layout.root()}/.cache/node_modules"
}

///|
/// `key` のキャッシュエントリ。`node_modules/` と `meta.json` を持つ。
pub fn cache_entry_path(key : String) -> String {
  "\{cache_root()}/\{key}"
}

///|
/// 作成途中の `.` 始まりのものも含む。
pub async fn cache_entry_names() -> Array[String] {
  if @sys.exists(cache_root()) {
    @sys.subdirectory_names(cache_root())
  } else {
    []
  }
}

///|
pub async fn cache_has(key : String) -> Bool {
  @sys.exists("\{cache_entry_path(key)}/meta.json")
}

///|
/// `<package-lock.json の sha256>-abi<ABI>-<platform>-<arch>`。
/// node を実行できなければ `WsError`。
pub async fn compute_cache_key(package_dir : String) -> String {
  // nodenv がそのディレクトリの .node-version で node を選ぶため、ABI は ws 自身ではなく対象ディレクトリの node から取る
  let probe = @sys.run_captured(
    "node",
    [
      "-p", "[process.versions.modules, process.platform, process.arch].join('-')",
    ],
    cwd=package_dir,
  ) catch {
    error =>
      raise @errors.WsError(
        "\{package_dir} で node を実行できません: \{error}",
      )
  }
  if probe.exit_code != 0 {
    raise @errors.WsError(
      "\{package_dir} で node を実行できません: \{probe.stderr.trim()}",
    )
  }
  "\{@sys.sha256_hex("\{package_dir}/package-lock.json")}-abi\{probe.stdout}"
}

///|
fn clone_directory(
  source : String,
  destination : String,
) -> Unit raise @errors.WsError {
  if !@sys.clonefile(source, destination) {
    raise @errors.WsError(
      "\{source} を \{destination} に複製できません",
    )
  }
}

///|
/// 失敗やキャンセル(Ctrl-C)の後始末で、作りかけのディレクトリを消す。
// キャンセル中に呼ばれても削除しきれるよう中断不可にする
async fn remove_partial(path : String) -> Unit nocancel {
  @async.protect_from_cancel(() => @sys.remove_tree(path))
}

///|
/// `node_modules/.ws-cache-key` に記録されたキー。読めなければ `None`。
pub async fn read_marker(package_dir : String) -> String? {
  Some(
    @fs.read_text("\{package_dir}/node_modules/.ws-cache-key").trim().to_owned(),
  ) catch {
    _ => None
  }
}

///|
async fn write_marker(package_dir : String, key : String) -> Unit {
  if read_marker(package_dir) != Some(key) {
    @fs.write_text(
      "\{package_dir}/node_modules/.ws-cache-key",
      key,
      create_mode=CreateOrTruncate,
    )
  }
}

///|
/// `<package_dir>/node_modules` をキャッシュに登録する。登録済みなら何もしない。
pub async fn register_in_cache(package_dir : String, key : String) -> Unit {
  if cache_has(key) {
    return
  }
  // 登録元にも目印を置く。クローンでブロックが共有されるのでディスク消費は増えない
  write_marker(package_dir, key)
  // 完成したものだけがキャッシュとして見えるよう、staging で作ってから rename する
  let staging = "\{cache_root()}/.tmp-\{key}"
  @sys.remove_tree(staging)
  @fs.mkdir(staging, recursive=true)
  errdefer remove_partial(staging)
  clone_directory("\{package_dir}/node_modules", "\{staging}/node_modules")
  let meta : Json = {
    "key": Json::string(key),
    "createdAt": Json::number(@env.now().to_double()),
  }
  @fs.write_text(
    "\{staging}/meta.json",
    meta.stringify(),
    create_mode=CreateNew,
  )
  @fs.rename(staging, cache_entry_path(key))
}

///|
/// `<package_dir>/node_modules` をキャッシュのクローンで置き換える。
pub async fn place_from_cache(package_dir : String, key : String) -> Unit {
  let target = "\{package_dir}/node_modules"
  @sys.remove_tree(target)
  // 目印まで書き終える前に失敗・キャンセルしたら、作りかけの node_modules を残さない
  errdefer remove_partial(target)
  clone_directory("\{cache_entry_path(key)}/node_modules", target)
  write_marker(package_dir, key)
}

///|
/// 上へ辿って最初に package.json があるディレクトリ。
pub async fn find_package_dir(start : String) -> String {
  let mut directory = @path.Path(start)
  while !@sys.exists("\{directory}/package.json") {
    let parent = directory.dirname()
    if parent == directory {
      raise @errors.WsError("package.json が見つかりません")
    }
    directory = parent
  }
  directory.to_string()
}

///|
/// node_modules をキャッシュから、無ければ `npm ci` で用意して登録する。成功時はメッセージ、
/// `npm ci` の失敗は `Err(終了コード)` で返す。
pub async fn ensure_node_modules(
  package_dir : String,
  npm_args : Array[String],
) -> Result[String, Int32] {
  let lockfile = "\{package_dir}/package-lock.json"
  if !@sys.exists(lockfile) {
    raise @errors.WsError(
      "\{package_dir} に package-lock.json がありません",
    )
  }
  // 追加引数(--omit=dev 等)は node_modules の中身を変えうるので、キャッシュの利用も登録もしない
  let key = if npm_args.is_empty() {
    Some(compute_cache_key(package_dir))
  } else {
    None
  }
  if key is Some(key) && read_marker(package_dir) == Some(key) {
    return Ok("node_modules は既に最新です: \{package_dir}")
  }
  if key is Some(key) && cache_has(key) {
    place_from_cache(package_dir, key)
    return Ok("キャッシュから復元しました: \{package_dir}")
  }
  let lock_hash = @sys.sha256_hex(lockfile)
  let status = {
    // npm ci は node_modules を作り直すので、キャンセル(Ctrl-C)で中断された作りかけは消してよい
    errdefer remove_partial("\{package_dir}/node_modules")
    @sys.run_inheriting("npm", ["ci", ..npm_args], cwd=package_dir) catch {
      _ => 1
    }
  }
  if status != 0 {
    return Err(if status > 0 { status } else { 1 })
  }
  if key is Some(key) &&
    @sys.sha256_hex(lockfile) == lock_hash &&
    @sys.exists("\{package_dir}/node_modules") {
    register_in_cache(package_dir, key)
    return Ok(
      "npm ci を実行しキャッシュに登録しました: \{package_dir}",
    )
  }
  Ok("npm ci を実行しました: \{package_dir}")
}

///|
/// git 管理下の package-lock.json(node_modules 内を除く)のディレクトリ。
pub async fn lockfile_directories(worktree : String) -> Array[String] {
  let directories = []
  for file in @git.output(["-C", worktree, "ls-files", "-z"]).split("\u{0}") {
    let file = @path.Path(file.to_owned())
    if file.basename() == "package-lock.json" &&
      !file.to_string().split("/").any(part => part == "node_modules") {
      directories.push(
        @path.Path(worktree).join(file.dirname()).normalize().to_string(),
      )
    }
  }
  @sys.sorted(directories)
}

///|
/// 全 lockfile について node_modules を用意し、失敗した数を返す。
pub async fn install_node_modules_in_worktree(worktree : String) -> Int32 {
  let mut failed = 0
  for package_dir in lockfile_directories(worktree) {
    let failure = try ensure_node_modules(package_dir, []) catch {
      error =>
        match @errors.expected_message(error) {
          Some(message) => message.trim().to_owned()
          None => raise error
        }
    } noraise {
      Ok(message) => {
        @sys.log(message)
        continue
      }
      Err(_) => "npm ci に失敗しました: \{package_dir}"
    }
    failed += 1
    @sys.log_error(
      "warn: \{failure}\n  再実行: cd \{package_dir} && ws npm ci",
    )
  }
  failed
}
