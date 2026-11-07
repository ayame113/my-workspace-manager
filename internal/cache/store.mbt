///|
let cache_key_marker = ".ws-cache-key"

///|
fn node_modules_cache_root() -> String {
  @sys.join([@layout.root(), ".cache", "node_modules"])
}

///|
fn trash_root() -> String {
  @sys.join([@layout.root(), ".cache", "trash"])
}

///|
/// `key` のキャッシュエントリのディレクトリ。`node_modules/` と `meta.json` を持つ。
pub fn cache_entry_path(key : String) -> String {
  @sys.join([node_modules_cache_root(), key])
}

///|
/// 全キャッシュエントリの名前。作成途中の `.` 始まりのものも含む。
pub async fn cache_entry_names() -> Array[String] {
  let root = node_modules_cache_root()
  if !@sys.exists(root) {
    return []
  }
  @sys.subdirectory_names(root)
}

///|
pub async fn cache_has(key : String) -> Bool {
  @sys.exists(@sys.join([cache_entry_path(key), "meta.json"]))
}

///|
let cache_key_by_package_dir : Map[String, String] = Map([])

///|
/// `<package-lock.json の sha256>-abi<ABI>-<platform>-<arch>`。ABI 等は `package_dir` で選ばれる node のもの。
/// そこで node を実行できなければ `WsError`。
pub async fn compute_cache_key(package_dir : String) -> String {
  if cache_key_by_package_dir.get(package_dir) is Some(key) {
    return key
  }
  // nodenv がそのディレクトリの .node-version で node を選ぶため、ABI は ws 自身ではなく対象ディレクトリの node から取る
  let probe = @sys.run_captured(
    "node",
    [
      "-p", "[process.versions.modules, process.platform, process.arch].join('-')",
    ],
    cwd=package_dir,
    env=environment_for(package_dir),
  ) catch {
    @sys.SpawnError(message) =>
      raise @errors.WsError(
        "\{package_dir} で node を実行できません: \{message}",
      )
    error => raise error
  }
  if probe.exit_code != 0 {
    raise @errors.WsError(
      "\{package_dir} で node を実行できません: \{@sys.trim(probe.stderr)}",
    )
  }
  let lock_hash = @sys.sha256_hex_of_file(
    @sys.join([package_dir, "package-lock.json"]),
  )
  let key = "\{lock_hash}-abi\{@sys.trim(probe.stdout)}"
  cache_key_by_package_dir.set(package_dir, key)
  key
}

///|
/// 使った方法(`clonefile` か `cp -cR`)を返す。
async fn clone_directory(source : String, destination : String) -> String {
  if @sys.clonefile(source, destination) == 0 {
    return "clonefile"
  }
  @sys.remove_recursively(destination)
  if @sys.run_silently("cp", ["-cR", source, destination]) {
    return "cp -cR"
  }
  raise @errors.WsError("\{source} を \{destination} に複製できません")
}

///|
/// `directory` を trash に移し、切り離したバックグラウンドプロセスで削除する。大きな node_modules の削除を待たないため。
pub async fn discard_directory(directory : String) -> Unit {
  let trash = trash_root()
  @sys.make_directory(trash, recursive=true)
  let trashed = @sys.join([
    trash,
    "\{@sys.basename(directory)}-\{@sys.unique_suffix()}",
  ])
  @sys.rename(directory, trashed)
  ignore(@sys.spawn_detached("rm", ["-rf", trashed]))
}

///|
/// `<package_dir>/node_modules/.ws-cache-key` に記録されたキー。読めなければ `None`。
pub async fn read_marker(package_dir : String) -> String? {
  let text = @sys.read_text(
    @sys.join([package_dir, "node_modules", cache_key_marker]),
  ) catch {
    _ => return None
  }
  Some(@sys.trim(text))
}

///|
async fn write_marker(package_dir : String, key : String) -> Unit {
  if read_marker(package_dir) != Some(key) {
    @sys.write_text(
      @sys.join([package_dir, "node_modules", cache_key_marker]),
      key,
    )
  }
}

///|
/// `<package_dir>/node_modules` を `key` でキャッシュに登録する。既に登録済みなら false。
pub async fn register_in_cache(package_dir : String, key : String) -> Bool {
  if cache_has(key) {
    return false
  }
  let root = node_modules_cache_root()
  @sys.make_directory(root, recursive=true)
  let staging = @sys.join([root, ".tmp-\{@sys.unique_suffix()}"])
  @sys.make_directory(staging)
  // 登録元にも目印を置く。クローンでブロックが共有されるのでディスク消費は増えない
  write_marker(package_dir, key)
  stage_cache_entry(package_dir, staging, key) catch {
    error => {
      if cache_has(key) {
        return false
      }
      raise error
    }
  }
  true
}

///|
async fn stage_cache_entry(
  package_dir : String,
  staging : String,
  key : String,
) -> Unit {
  try {
    ignore(
      clone_directory(
        @sys.join([package_dir, "node_modules"]),
        @sys.join([staging, "node_modules"]),
      ),
    )
    let meta : Json = {
      "key": Json::string(key),
      "createdAt": Json::string(@sys.iso_timestamp()),
    }
    @sys.write_text(@sys.join([staging, "meta.json"]), meta.stringify())
    @sys.rename(staging, cache_entry_path(key))
  } catch {
    error => {
      discard_directory(staging)
      raise error
    }
  }
}

///|
/// `<package_dir>/node_modules` をキャッシュのクローンで置き換え、使った複製方法を返す。
pub async fn place_from_cache(package_dir : String, key : String) -> String {
  let target = @sys.join([package_dir, "node_modules"])
  if @sys.exists(target) {
    discard_directory(target)
  }
  let cloning = clone_directory(
    @sys.join([cache_entry_path(key), "node_modules"]),
    target,
  ) catch {
    error => {
      if @sys.exists(target) {
        discard_directory(target)
      }
      raise error
    }
  }
  write_marker(package_dir, key)
  cloning
}
