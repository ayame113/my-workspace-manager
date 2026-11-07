///|
/// Node.js の `fs.existsSync` 相当。symlink は辿り、エラーはすべて「存在しない」扱い。
pub async fn exists(path : String) -> Bool {
  @fs.exists(path) catch {
    _ => false
  }
}

///|
/// symlink を辿った先がディレクトリか。調べられなければ例外。
pub async fn is_directory(path : String) -> Bool {
  @fs.kind(path, follow_symlink=true) == Directory
}

///|
/// `directory` 直下のディレクトリ(symlink は除く)の名前を UTF-16 順にソートして返す。読めなければ例外。
pub async fn subdirectory_names(directory : String) -> Array[String] {
  let handle = @fs.opendir(directory)
  defer handle.close()
  let names : Array[String] = []
  while handle.next(include_hidden=true) is Some(entry) {
    if entry.is_dir {
      names.push(entry.name)
    }
  }
  sorted(names)
}

///|
pub async fn make_directory(path : String, recursive~ : Bool = false) -> Unit {
  @fs.mkdir(path, recursive~, allow_exist=recursive)
}

///|
pub async fn rename(from : String, to : String) -> Unit {
  @fs.rename(from, to)
}

///|
pub async fn realpath(path : String) -> String {
  @fs.realpath(path)
}

///|
pub async fn read_text(path : String) -> String {
  @utf8.decode_lossy(@fs.read_binary(path))
}

///|
pub async fn write_text(path : String, text : String) -> Unit {
  @fs.write_text(path, text, create_mode=CreateOrTruncate)
}

///|
/// Node.js の `fs.rmSync(path, { recursive: true, force: true })` 相当。symlink は辿らずに消す。
pub async fn remove_recursively(path : String) -> Unit {
  let kind = @fs.kind(path, follow_symlink=false) catch {
    @os_error.OsError(NotFound, ..) => return
    error => raise error
  }
  if kind == Directory {
    @fs.rmdir(path, recursive=true, allow_missing=true)
  } else {
    @fs.remove(path, allow_missing=true)
  }
}

///|
pub async fn sha256_hex_of_file(path : String) -> String {
  let digest = @crypto.sha256(@fs.read_binary(path))
  let hex = StringBuilder()
  for byte in digest {
    hex.write_string(byte.to_int().to_string(radix=16).pad_start(2, '0'))
  }
  hex.to_string()
}
