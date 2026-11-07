///|
async fn read_packages(file : String) -> Map[String, Json] {
  let root = @json.parse(@sys.read_text(file))
  match root {
    Null => fail("lockfile root is null")
    Object(fields) =>
      match fields.get("packages") {
        Some(Object(packages)) => packages
        _ => Map([])
      }
    _ => Map([])
  }
}

///|
fn field(entry : Json, name : String) -> Json {
  match entry {
    Object(fields) => fields.get(name).unwrap_or(Json::null())
    _ => Json::null()
  }
}

///|
fn package_identity(entry : Json) -> String {
  // file: の resolved は記録場所(lockfile と node_modules/.package-lock.json)からの相対パスなので、同じ依存でも値が異なる
  let resolved = match field(entry, "resolved") {
    String(value) if value.has_prefix("file:") => Json::null()
    value => value
  }
  let linked = field(entry, "link") is True
  Json::array([
    field(entry, "version"),
    resolved,
    field(entry, "integrity"),
    Json::boolean(linked),
  ]).stringify()
}

///|
// node_modules/.package-lock.json は npm 自身が書く「実際にインストールした内容」の記録。
// dev/peer などの付随フィールドは npm が lockfile と異なる形で書くことがあるため、同一性を表すフィールドだけを比べる
async fn node_modules_matches_lockfile(package_dir : String) -> Bool {
  let hidden_path = @sys.join([
    package_dir, "node_modules", ".package-lock.json",
  ])
  if !@sys.exists(hidden_path) {
    return false
  }
  let (lock_packages, installed_packages) = (
    read_packages(@sys.join([package_dir, "package-lock.json"])),
    read_packages(hidden_path),
  ) catch {
    _ => return false
  }
  for name, entry in installed_packages {
    if name == "" {
      continue
    }
    match lock_packages.get(name) {
      Some(locked) if package_identity(locked) == package_identity(entry) => ()
      _ => return false
    }
  }
  for name, entry in lock_packages {
    // node_modules 外の link 先や、現在のプラットフォームでは入らない optional は実際には配置されない
    if name == "" ||
      !name.contains("node_modules/") ||
      field(entry, "optional") is True {
      continue
    }
    if !installed_packages.contains(name) {
      return false
    }
  }
  true
}
