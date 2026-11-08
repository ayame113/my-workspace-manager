///|
/// 前回処理したファイルの状態。inode・ctime・size が今と一致すれば内容は変わっていないとみなし、記録したハッシュを使う。
// npm はパッケージ展開時に mtime を固定値(1985-10-26)にするので、変更の検出には mtime ではなく ctime を使う
priv struct IndexEntry {
  inode : Int64
  ctime_ns : Int64
  size : Int64
  /// サイズが一意だったファイルは計算していないので `None`
  hash : String?
  /// 同じキーの代表のクローンに置き換え済み
  shared : Bool
}

///|
let index_header = "ws-compact-index-v2"

///|
fn index_path() -> String {
  "\{@layout.root()}/.compact-index"
}

///|
fn IndexEntry::matches(self : IndexEntry, stat : @sys.Stat) -> Bool {
  self.inode == stat.inode &&
  self.ctime_ns == stat.ctime_ns &&
  self.size == stat.size
}

///|
/// 無い・壊れている場合は空(全ファイルを処理し直す)。
async fn load_index() -> Map[String, IndexEntry] {
  let entries = Map([])
  let text = @fs.read_text(index_path()) catch { _ => return entries }
  let lines = text.split("\n")
  guard lines.next() is Some(header) && header == index_header else {
    return Map([])
  }
  for line in lines {
    match line.split("\t").collect() {
      [inode, ctime_ns, size, hash, shared, path] => {
        let parse = (text : StringView) => @string.parse_int64(text)
        let (inode, ctime_ns, size) = (
          parse(inode),
          parse(ctime_ns),
          parse(size),
        ) catch {
          _ => return Map([])
        }
        let hash = if hash == "-" { None } else { Some(hash.to_owned()) }
        entries[path.to_owned()] = {
          inode,
          ctime_ns,
          size,
          hash,
          shared: shared == "1",
        }
      }
      [""] => ()
      _ => return Map([])
    }
  }
  entries
}

///|
/// 途中で中断しても壊れたインデックスが残らないよう、一時ファイルに書いてから rename する。
async fn save_index(entries : Map[String, IndexEntry]) -> Unit {
  let text = StringBuilder()
  text.write_string("\{index_header}\n")
  for path, entry in entries {
    let shared = if entry.shared { "1" } else { "0" }
    text.write_string(
      "\{entry.inode}\t\{entry.ctime_ns}\t\{entry.size}\t\{entry.hash.unwrap_or("-")}\t\{shared}\t\{path}\n",
    )
  }
  let temporary = index_path() + ".tmp"
  @fs.write_text(temporary, text.to_string(), create_mode=CreateOrTruncate)
  @fs.rename(temporary, index_path())
}
