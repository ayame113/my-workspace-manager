///|
#borrow(source, destination)
extern "C" fn c_clonefile(source : Bytes, destination : Bytes) -> Int32 = "ws_clonefile"

///|
fn c_string(text : String) -> Bytes {
  @utf8.encode(text + "\u{0}")
}

///|
/// `source` を copy-on-write の `clonefile(2)` で `destination`(存在しないこと)に複製する。
pub fn clonefile(source : String, destination : String) -> Bool {
  c_clonefile(c_string(source), c_string(destination)) == 0
}

///|
// println はバッファされ、stdio を継承した子プロセスの出力と順序が入れ替わるので fd に直接書く。
// 読み手が先に終了しても(`ws ls | head -1`)処理を止めないよう、閉じたパイプへの出力は捨てる
async fn write(output : @stdio.Output, text : String) -> Unit {
  output.write(@utf8.encode(text)) catch {
    @os_error.OsError(_, raw_code=32, ..) => ()
    error => raise error
  }
}

///|
pub async fn print(text : String) -> Unit {
  write(@stdio.stdout, text)
}

///|
pub async fn eprint(text : String) -> Unit {
  write(@stdio.stderr, text)
}

///|
pub async fn log(text : String) -> Unit {
  print(text + "\n")
}

///|
pub async fn log_error(text : String) -> Unit {
  eprint(text + "\n")
}

///|
pub struct Captured {
  exit_code : Int32
  stdout : String
  stderr : String
}

///|
/// stdin を /dev/null にして実行し、stdout(trim 済み)と stderr を集める。起動できなければ例外。
pub async fn run_captured(
  command : String,
  args : Array[String],
  cwd? : String,
) -> Captured {
  let result = @process.run(
    command,
    args,
    stdin=Empty,
    stdout=Collect,
    stderr=Collect,
    cwd?=cwd.map(dir => dir[:]),
  )
  {
    exit_code: result.exit_code,
    stdout: @utf8.decode_lossy(result.stdout_binary()).trim().to_owned(),
    stderr: @utf8.decode_lossy(result.stderr_binary()),
  }
}

///|
/// stdio を継承して実行し、終了コードを返す(シグナル終了は負)。起動できなければ例外。
pub async fn run_inheriting(
  command : String,
  args : Array[String],
  cwd? : String,
) -> Int32 {
  @process.run(command, args, cwd?=cwd.map(dir => dir[:])).exit_code
}

///|
/// 出力を捨てて実行し、成功したかを返す。起動できない場合も失敗。
pub async fn run_silently(command : String, args : Array[String]) -> Bool {
  let result = @process.run(
    command,
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
/// 終了を待たずに実行し、ws の終了後も動かし続ける。
pub async fn spawn_in_background(
  command : String,
  args : Array[String],
) -> Unit {
  @process.spawn_orphan(
    command,
    args,
    stdin=Empty,
    stdout=Discard,
    stderr=Discard,
  ).close()
}

///|
/// symlink は辿る。調べられない場合も false。
pub async fn exists(path : String) -> Bool {
  @fs.exists(path) catch {
    _ => false
  }
}

///|
pub async fn is_directory(path : String) -> Bool {
  @fs.kind(path) == Directory
}

///|
pub fn sorted(items : Array[String]) -> Array[String] {
  let copy = items.copy()
  copy.sort_by((left, right) => left.lexical_compare(right))
  copy
}

///|
/// `directory` 直下のディレクトリ(symlink は除く)の名前をソートして返す。
pub async fn subdirectory_names(directory : String) -> Array[String] {
  let handle = @fs.opendir(directory)
  defer handle.close()
  let names = []
  while handle.next(include_hidden=true) is Some(entry) {
    if entry.is_dir {
      names.push(entry.name)
    }
  }
  sorted(names)
}

///|
/// 無ければ何もしない。
pub async fn remove_tree(path : String) -> Unit {
  if !exists(path) {
    return
  }
  if @fs.kind(path, follow_symlink=false) == Directory {
    @fs.rmdir(path, recursive=true)
  } else {
    @fs.remove(path)
  }
}

///|
pub async fn sha256_hex(path : String) -> String {
  let hex = StringBuilder()
  for byte in @crypto.sha256(@fs.read_binary(path)) {
    hex.write_string(byte.to_int().to_string(radix=16).pad_start(2, '0'))
  }
  hex.to_string()
}
