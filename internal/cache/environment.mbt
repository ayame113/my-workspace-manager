///|
// ws が nodenv 経由で起動されたシェルやツール(nodenv exec が export する)から呼ばれると、そのとき選ばれた node の
// バージョン別 bin が PATH の先頭に、起動時の cwd が NODENV_DIR に、解決済みバージョンが NODENV_VERSION に入った状態で継承される。
// そのままでは子プロセスの node/npm が対象ディレクトリの .node-version ではなく、その版になる。PWD も spawn の cwd には追従しない

///|
/// `directory` で node/npm を実行するための環境変数。nodenv がそのディレクトリの `.node-version` で版を選ぶようにする。
pub fn environment_for(directory : String) -> Map[String, String] {
  let environment = @sys.environment()
  environment.set("PWD", directory)
  environment.remove("NODENV_DIR")
  environment.remove("NODENV_VERSION")
  if environment.get("NODENV_ROOT") is Some(nodenv_root) &&
    nodenv_root != "" &&
    environment.get("PATH") is Some(path) &&
    path != "" {
    let versions_root = @sys.join([nodenv_root, "versions"]) + "/"
    let kept = @sys.split_to_array(path, ":").filter(entry => {
      !entry.has_prefix(versions_root)
    })
    environment.set("PATH", kept.join(":"))
  }
  environment
}
