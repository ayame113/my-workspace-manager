///|
/// `files` は (worktree 内の相対パス, 内容, mode)。
async fn make_files(
  worktree : String,
  files : Array[(String, String, Int32)],
) -> Unit {
  for file in files {
    let (path, content, mode) = file
    let full = "\{worktree}/\{path}"
    @fs.mkdir(
      @path.Path(full).dirname().to_string(),
      recursive=true,
      allow_exist=true,
    )
    write(full, content)
    @fs.chmod(full, mode)
  }
}

///|
/// 各ファイルの inode・mode・内容を並べる。置き換えで inode だけが変わり、内容と mode は変わらないことを確かめる。
async fn describe(paths : Array[String]) -> Array[(Int64, Int32, String)] {
  let result = []
  for path in paths {
    let stat = @sys.lstat(path).unwrap()
    result.push((stat.inode, stat.mode, @fs.read_text(path)))
  }
  result
}

///|
async test "compact shares identical node_modules files" {
  let s = Sandbox::new()
  defer s.cleanup()
  s.make_remote("myrepo", ["b1", "b2", "b3"])
  s.setup_ws(["new", "t1"])
  for branch in ["b1", "b2"] {
    s.setup_ws([
      "worktree", "add", "--task", "t1", "--repo", "myrepo", "--branch", branch,
    ])
  }
  let a = "\{s.tasks}/t1/myrepo--b1"
  let b = "\{s.tasks}/t1/myrepo--b2"
  make_files(a, [
    ("node_modules/x/index.js", "same\n", 0o644),
    ("node_modules/x/node_modules/y/index.js", "same\n", 0o644),
    ("node_modules/x/diff.js", "a1\n", 0o644),
    ("node_modules/x/bin.sh", "same\n", 0o755),
    ("node_modules/x/empty", "", 0o644),
    ("lib/outside.js", "same\n", 0o644),
  ])
  make_files(b, [
    ("node_modules/x/index.js", "same\n", 0o644),
    ("node_modules/x/diff.js", "b2\n", 0o644),
    ("node_modules/x/bin.sh", "same\n", 0o644),
    ("node_modules/x/empty", "", 0o644),
  ])
  @fs.symlink(target="index.js", "\{b}/node_modules/x/link.js")
  let watched = [
    "\{a}/node_modules/x/index.js",
    "\{a}/node_modules/x/node_modules/y/index.js",
    "\{a}/node_modules/x/diff.js",
    "\{a}/node_modules/x/bin.sh",
    "\{a}/node_modules/x/empty",
    "\{a}/lib/outside.js",
    "\{b}/node_modules/x/index.js",
    "\{b}/node_modules/x/diff.js",
    "\{b}/node_modules/x/bin.sh",
    "\{b}/node_modules/x/empty",
  ]
  let before = describe(watched)

  ignore(s.ws(["compact", "--dry-run"]))
  s.note(
    "dry-run changed nothing: \{describe(watched) == before}, index: \{@fs.exists("\{s.ws_root}/.compact-index")}",
  )

  ignore(s.ws(["compact"]))
  let after = describe(watched)
  let replaced = []
  for i, path in watched {
    if before[i].0 != after[i].0 {
      replaced.push(path.replace(old=s.root, new="$S"))
    }
  }
  s.note("replaced inodes: \{replaced.join(" ")}")
  s.note(
    "contents and modes kept: \{before.map(f => (f.1, f.2)) == after.map(f => (f.1, f.2))}",
  )
  s.note(
    "symlink kept: \{@fs.kind("\{b}/node_modules/x/link.js", follow_symlink=false) == SymLink}",
  )

  // 2 回目: 変更が無ければハッシュ計算も置き換えも 0 件
  ignore(s.ws(["compact"]))
  // 書き換えたファイル(inode・ctime が変わる)だけが処理し直される
  make_files(b, [("node_modules/x/diff.js", "a1\n", 0o644)])
  ignore(s.ws(["compact"]))

  s.setup_ws([
    "worktree", "add", "--task", "t1", "--repo", "myrepo", "--branch", "b3",
  ])
  make_files("\{s.tasks}/t1/myrepo--b3", [
    ("node_modules/x/index.js", "same\n", 0o644),
    ("node_modules/x/unique.js", "only here\n", 0o644),
  ])
  ignore(s.ws(["compact"]))

  @fs.remove("\{s.ws_root}/.compact-index")
  ignore(s.ws(["compact"]))
  inspect(
    s.transcript.to_string(),
    content=(
      #|走査しました: 7 ファイル
      #|ハッシュ計算: 7 ファイル
      #|置き換え対象: 3 ファイル(削減見込み 0.0 MiB)。--dry-run のため変更していません
      #|rc=0
      #|dry-run changed nothing: true, index: false
      #|走査しました: 7 ファイル
      #|ハッシュ計算: 7 ファイル
      #|置き換え: 3 ファイル(削減見込み 0.0 MiB)
      #|空き容量: <df>
      #|rc=0
      #|replaced inodes: $S/root/tasks/t1/myrepo--b1/node_modules/x/index.js $S/root/tasks/t1/myrepo--b2/node_modules/x/index.js $S/root/tasks/t1/myrepo--b2/node_modules/x/bin.sh
      #|contents and modes kept: true
      #|symlink kept: true
      #|走査しました: 7 ファイル
      #|ハッシュ計算: 0 ファイル
      #|置き換え: 0 ファイル(削減見込み 0.0 MiB)
      #|空き容量: <df>
      #|rc=0
      #|走査しました: 7 ファイル
      #|ハッシュ計算: 1 ファイル
      #|置き換え: 1 ファイル(削減見込み 0.0 MiB)
      #|空き容量: <df>
      #|rc=0
      #|走査しました: 9 ファイル
      #|ハッシュ計算: 1 ファイル
      #|置き換え: 1 ファイル(削減見込み 0.0 MiB)
      #|空き容量: <df>
      #|rc=0
      #|走査しました: 9 ファイル
      #|ハッシュ計算: 8 ファイル
      #|置き換え: 5 ファイル(削減見込み 0.0 MiB)
      #|空き容量: <df>
      #|rc=0
      #|
    ),
  )
}
