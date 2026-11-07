///|
async fn read_packages(file : String) -> Map[String, Json] {
  match @json.parse(@fs.read_text(file)) {
    Object(root) =>
      match root.get("packages") {
        Some(Object(packages)) => packages
        _ => Map([])
      }
    _ => fail("\{file} is not a JSON object")
  }
}

///|
fn field(entry : Json, name : String) -> Json {
  if entry is Object(fields) && fields.get(name) is Some(value) {
    value
  } else {
    Json::null()
  }
}

///|
fn package_identity(entry : Json) -> Json {
  // file: の resolved は記録場所(lockfile と node_modules/.package-lock.json)からの相対パスなので、同じ依存でも値が異なる
  let resolved = match field(entry, "resolved") {
    String(value) if value.has_prefix("file:") => Json::null()
    value => value
  }
  [
    field(entry, "version"),
    resolved,
    field(entry, "integrity"),
    Json::boolean(field(entry, "link") is True),
  ]
}

///|
// node_modules/.package-lock.json は npm 自身が書く「実際にインストールした内容」の記録。
// dev/peer などの付随フィールドは npm が lockfile と異なる形で書くことがあるため、同一性を表すフィールドだけを比べる
async fn node_modules_matches_lockfile(package_dir : String) -> Bool {
  let (locked, installed) = (
    read_packages("\{package_dir}/package-lock.json"),
    read_packages("\{package_dir}/node_modules/.package-lock.json"),
  ) catch {
    _ => return false
  }
  for name, entry in installed {
    if name != "" &&
      locked.get(name).map(package_identity) != Some(package_identity(entry)) {
      return false
    }
  }
  // node_modules 外の link 先や、現在のプラットフォームでは入らない optional は実際には配置されない
  locked
  .iter()
  .all(item => {
    let (name, entry) = item
    !name.contains("node_modules/") ||
    field(entry, "optional") is True ||
    installed.contains(name)
  })
}
