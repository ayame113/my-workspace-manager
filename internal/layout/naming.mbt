///|
fn is_lower_alphanumeric(code_unit : UInt16) -> Bool {
  (code_unit >= 'a' && code_unit <= 'z') ||
  (code_unit >= '0' && code_unit <= '9')
}

///|
fn is_alphanumeric(code_unit : UInt16) -> Bool {
  is_lower_alphanumeric(code_unit) || (code_unit >= 'A' && code_unit <= 'Z')
}

///|
/// `^[a-z0-9]+(-[a-z0-9]+)*$` に一致するか(task 名と branch slug の規則)。
pub fn is_valid_task_name(name : String) -> Bool {
  name != "" &&
  !name.has_prefix("-") &&
  !name.has_suffix("-") &&
  !name.contains("--") &&
  name
  .code_units()
  .iter()
  .all(code_unit => is_lower_alphanumeric(code_unit) || code_unit == '-')
}

///|
/// `^[A-Za-z0-9][A-Za-z0-9._-]*$` に一致するか。
pub fn is_repo_name_shape(name : String) -> Bool {
  name != "" &&
  is_alphanumeric(name.code_unit_at(0)) &&
  name
  .code_units()
  .iter()
  .all(code_unit => {
    is_alphanumeric(code_unit) ||
    code_unit == '.' ||
    code_unit == '_' ||
    code_unit == '-'
  })
}

///|
// 旧実装の JavaScript `toLowerCase()` はこの 2 文字を ASCII を含む文字列に変換するので、slug を一致させるために合わせる
fn lower_case_like_javascript(text : String) -> String {
  text
  .to_lower()
  .replace_all(old="\u{130}", new="i\u{307}")
  .replace_all(old="\u{212A}", new="k")
}

///|
/// 小文字化し、`[a-z0-9-]` 以外の UTF-16 コード単位を `-` に置換し、連続する `-` を 1 つにして先頭末尾の `-` を除く。
pub fn branch_slug(branch : String) -> String {
  let slug = StringBuilder()
  let mut previous_was_hyphen = false
  for code_unit in lower_case_like_javascript(branch).code_units() {
    let is_hyphen = !is_lower_alphanumeric(code_unit)
    if is_hyphen && (previous_was_hyphen || slug.is_empty()) {
      continue
    }
    slug.write_char(
      if is_hyphen {
        '-'
      } else {
        code_unit.to_int().unsafe_to_char()
      },
    )
    previous_was_hyphen = is_hyphen
  }
  let slug = slug.to_string()
  if slug.has_suffix("-") {
    slug.exact_view(end=slug.length() - 1).to_owned()
  } else {
    slug
  }
}

///|
pub fn slug_or_raise(branch : String) -> String raise @errors.WsError {
  if branch.has_prefix("-") {
    raise @errors.WsError(
      "ブランチ名が '-' で始まっています: '\{branch}'",
    )
  }
  let slug = branch_slug(branch)
  if slug == "" {
    raise @errors.WsError(
      "ブランチ名 '\{branch}' から slug を作れません",
    )
  }
  slug
}

///|
pub fn validate_task_name(task : String) -> Unit raise @errors.WsError {
  if !is_valid_task_name(task) {
    raise @errors.WsError(
      "task 名は [a-z0-9-] のみ(先頭末尾・連続ハイフン不可): '\{task}'",
    )
  }
}

///|
pub fn validate_repo_name(repo : String) -> Unit raise @errors.WsError {
  if !is_repo_name_shape(repo) || repo.contains("--") {
    raise @errors.WsError(
      "repo 名が不正です('--' を含まず英数字 . _ - のみ): '\{repo}'",
    )
  }
}

///|
/// worktree のディレクトリ名 `<repo>--<slug>` の repo 部分。`--` が無ければ名前全体。
pub fn repo_of(name : String) -> String {
  match name.find("--") {
    Some(separator) => name.exact_view(end=separator).to_owned()
    None => name
  }
}

///|
pub struct WorktreeName {
  repo : String
  slug : String
}

///|
/// `<repo>--<slug>` を分解する。どちらかが命名規則に合わなければ `None`。
pub fn parse_worktree_name(name : String) -> WorktreeName? {
  guard name.find("--") is Some(separator) && separator >= 1 else {
    return None
  }
  let repo = name.exact_view(end=separator).to_owned()
  let slug = name.exact_view(start=separator + 2).to_owned()
  if !is_repo_name_shape(repo) || !is_valid_task_name(slug) {
    return None
  }
  Some({ repo, slug, })
}
