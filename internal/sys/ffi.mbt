///|
#borrow(source, destination)
extern "C" fn c_clonefile(source : Bytes, destination : Bytes) -> Int32 = "ws_clonefile"

///|
extern "C" fn c_getpid() -> Int32 = "ws_getpid"

///|
#borrow(argv_blob)
extern "C" fn c_spawn_detached(argv_blob : Bytes, argc : Int32) -> Int32 = "ws_spawn_detached"

///|
fn to_c_string(text : String) -> Bytes {
  let encoded = @utf8.encode(text)
  Bytes::makei(encoded.length() + 1, index => {
    if index < encoded.length() {
      encoded[index]
    } else {
      0
    }
  })
}

///|
/// `source`(ファイルまたはディレクトリツリー)を `clonefile(2)`(copy-on-write)で `destination` に複製する。
/// `destination` は存在してはならない。成功なら 0、失敗なら errno を返す。
pub fn clonefile(source : String, destination : String) -> Int32 {
  c_clonefile(to_c_string(source), to_c_string(destination))
}

///|
pub fn process_id() -> Int32 {
  c_getpid()
}

///|
/// `command`(PATH から探す)を新しいセッションで stdio を /dev/null にして起動し、終了を待たない。
/// 起動できれば 0、できなければ errno を返す。
pub fn spawn_detached(command : String, args : Array[String]) -> Int32 {
  let blob = Buffer()
  for part in [command, ..args] {
    blob.write_bytes(to_c_string(part))
  }
  c_spawn_detached(blob.to_bytes(), args.length() + 1)
}
