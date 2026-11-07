// 表示されるパスを旧実装と同一に保つため、Node.js の `path.posix` と同じ結果を返す

///|
fn normalize_segments(path : String, allow_above_root : Bool) -> Array[String] {
  let segments : Array[String] = []
  for segment in path.split("/") {
    match segment {
      "" | "." => ()
      ".." =>
        if segments.length() > 0 && segments[segments.length() - 1] != ".." {
          ignore(segments.pop())
        } else if allow_above_root {
          segments.push("..")
        }
      _ => segments.push(segment.to_owned())
    }
  }
  segments
}

///|
/// Node.js の `path.normalize` 相当。
pub fn normalize(path : String) -> String {
  if path == "" {
    return "."
  }
  let is_absolute = path.has_prefix("/")
  let has_trailing_separator = path.has_suffix("/")
  let body = normalize_segments(path, !is_absolute).join("/")
  if body == "" {
    return if is_absolute {
      "/"
    } else if has_trailing_separator {
      "./"
    } else {
      "."
    }
  }
  let body = if has_trailing_separator { body + "/" } else { body }
  if is_absolute {
    "/" + body
  } else {
    body
  }
}

///|
/// Node.js の `path.join` 相当。
pub fn join(parts : Array[String]) -> String {
  let non_empty = parts.filter(part => part != "")
  if non_empty.length() == 0 {
    return "."
  }
  normalize(non_empty.join("/"))
}

///|
/// Node.js の `path.dirname` 相当。
pub fn dirname(path : String) -> String {
  if path == "" {
    return "."
  }
  let has_root = path.has_prefix("/")
  let mut end = -1
  let mut matched_slash = true
  for index = path.length() - 1; index >= 1; index = index - 1 {
    if path.code_unit_at(index) == '/' {
      if !matched_slash {
        end = index
        break
      }
    } else {
      matched_slash = false
    }
  }
  if end == -1 {
    return if has_root { "/" } else { "." }
  }
  if has_root && end == 1 {
    return "//"
  }
  path.exact_view(end~).to_owned()
}

///|
/// Node.js の `path.basename`(拡張子引数なし)相当。
pub fn basename(path : String) -> String {
  let mut start = 0
  let mut end = -1
  let mut matched_slash = true
  for index = path.length() - 1; index >= 0; index = index - 1 {
    if path.code_unit_at(index) == '/' {
      if !matched_slash {
        start = index + 1
        break
      }
    } else if end == -1 {
      matched_slash = false
      end = index + 1
    }
  }
  if end == -1 {
    return ""
  }
  path.exact_view(start~, end~).to_owned()
}

///|
/// Node.js の `path.resolve(path)` 相当。相対パスは `cwd` 基準。
pub fn resolve(path : String, cwd~ : String) -> String {
  let absolute = if path.has_prefix("/") { path } else { cwd + "/" + path }
  "/" + normalize_segments(absolute, false).join("/")
}

///|
/// Node.js の `path.relative(from, to)` 相当。
pub fn relative(from : String, to : String, cwd~ : String) -> String {
  let from_segments = normalize_segments(resolve(from, cwd~), false)
  let to_segments = normalize_segments(resolve(to, cwd~), false)
  let mut common = 0
  while common < from_segments.length() &&
        common < to_segments.length() &&
        from_segments[common] == to_segments[common] {
    common = common + 1
  }
  let parts : Array[String] = []
  for _ in common..<from_segments.length() {
    parts.push("..")
  }
  for segment in to_segments[common:] {
    parts.push(segment)
  }
  parts.join("/")
}
