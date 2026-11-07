///|
/// JavaScript の `trim` と正規表現 `\s` が空白とみなすコード単位か。
pub fn is_js_whitespace(code_unit : UInt16) -> Bool {
  match code_unit.to_int() {
    0x09 | 0x0A | 0x0B | 0x0C | 0x0D | 0x20 | 0xA0 | 0x1680 => true
    0x2028 | 0x2029 | 0x202F | 0x205F | 0x3000 | 0xFEFF => true
    code => code >= 0x2000 && code <= 0x200A
  }
}

///|
/// JavaScript の `String.prototype.trim` 相当。
pub fn trim(text : String) -> String {
  let mut start = 0
  let mut end = text.length()
  while start < end && is_js_whitespace(text.code_unit_at(start)) {
    start = start + 1
  }
  while end > start && is_js_whitespace(text.code_unit_at(end - 1)) {
    end = end - 1
  }
  text.exact_view(start~, end~).to_owned()
}

///|
/// JavaScript の `text.slice(start)` 相当。範囲外なら ""。
pub fn slice_from(text : String, start : Int32) -> String {
  if start >= text.length() {
    ""
  } else {
    text.exact_view(start~).to_owned()
  }
}

///|
pub fn split_to_array(text : String, separator : String) -> Array[String] {
  text.split(separator).map(part => part.to_owned()).collect()
}

///|
/// JavaScript の比較関数なし `toSorted()` と同じく UTF-16 コード単位順に並べる。
pub fn sorted(items : Array[String]) -> Array[String] {
  let copy = items.copy()
  copy.sort_by((left, right) => left.lexical_compare(right))
  copy
}
