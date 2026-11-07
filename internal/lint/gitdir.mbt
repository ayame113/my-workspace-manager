///|
fn is_line_terminator(code_unit : UInt16) -> Bool {
  code_unit == '\n' ||
  code_unit == '\r' ||
  code_unit == 0x2028 ||
  code_unit == 0x2029
}

///|
fn end_of_line(text : String, start : Int32) -> Int32 {
  let mut end = start
  while end < text.length() && !is_line_terminator(text.code_unit_at(end)) {
    end = end + 1
  }
  end
}

///|
/// JavaScript の正規表現 `/^gitdir:\s*(.+)$/m` を `text` に適用したときのキャプチャ(バックトラックの挙動も含めて同じ)。
fn match_gitdir_line(text : String) -> String? {
  let prefix = "gitdir:"
  for line_start in 0..<text.length() {
    if line_start > 0 && !is_line_terminator(text.code_unit_at(line_start - 1)) {
      continue
    }
    if !text.exact_view(start=line_start).has_prefix(prefix) {
      continue
    }
    let after_prefix = line_start + prefix.length()
    let mut after_spaces = after_prefix
    while after_spaces < text.length() &&
          @sys.is_js_whitespace(text.code_unit_at(after_spaces)) {
      after_spaces = after_spaces + 1
    }
    for capture_start = after_spaces
        capture_start >= after_prefix
        capture_start = capture_start - 1 {
      if capture_start < text.length() &&
        !is_line_terminator(text.code_unit_at(capture_start)) {
        return Some(
          text
          .exact_view(start=capture_start, end=end_of_line(text, capture_start))
          .to_owned(),
        )
      }
    }
  }
  None
}

///|
priv enum GitdirCheck {
  Gitdir(String)
  Problem(String)
}

///|
async fn read_gitdir(worktree : String) -> GitdirCheck {
  let git_path = @sys.join([worktree, ".git"])
  if !@sys.exists(git_path) {
    return Problem(".git がありません")
  }
  if @sys.is_directory(git_path) {
    return Problem(
      ".git がディレクトリです(worktree ではなく独立した repo)",
    )
  }
  guard match_gitdir_line(@sys.read_text(git_path)) is Some(captured) else {
    return Problem(".git ファイルの内容が不正です")
  }
  let gitdir = @sys.trim(captured)
  if !@sys.exists(gitdir) {
    return Problem("gitdir が存在しません: \{gitdir}")
  }
  if !@git.succeeds(["-C", worktree, "rev-parse", "--git-dir"]) {
    return Problem("git が worktree として認識しません")
  }
  Gitdir(gitdir)
}
