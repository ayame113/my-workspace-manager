///|
priv struct Candidate {
  path : String
  stat : @sys.Stat
  /// 前回から変更されていなければ、そのときの記録
  recorded : IndexEntry?
  mut hash : String?
}

///|
fn Candidate::entry(self : Candidate, shared~ : Bool) -> IndexEntry {
  {
    inode: self.stat.inode,
    ctime_ns: self.stat.ctime_ns,
    size: self.stat.size,
    hash: self.hash,
    shared,
  }
}

///|
/// worktree 内の node_modules(入れ子を含む)配下の空でない通常ファイル。symlink は辿らず、ハードリンクは 1 つにまとめる。
async fn scan_node_modules(
  directory : String,
  inside_node_modules : Bool,
  seen : Set[(Int64, Int64)],
  files : Array[(String, @sys.Stat)],
) -> Unit {
  let handle = @fs.opendir(directory)
  defer handle.close()
  while handle.next(include_hidden=true) is Some(entry) {
    let path = "\{directory}/\{entry.name}"
    if entry.is_dir {
      if entry.name != ".git" {
        scan_node_modules(
          path,
          inside_node_modules || entry.name == "node_modules",
          seen,
          files,
        )
      }
    } else if inside_node_modules &&
      @sys.lstat(path) is Some(stat) &&
      stat.is_regular &&
      stat.size > 0L &&
      !seen.contains((stat.device, stat.inode)) {
      seen.add((stat.device, stat.inode))
      files.push((path, stat))
      if files.length() % 10000 == 0 {
        @sys.log("走査中: \{files.length()} ファイル")
      }
    }
  }
}

///|
async fn scan_tasks() -> Array[(String, @sys.Stat)] {
  let files = []
  let seen = Set([])
  let tasks_root = @layout.tasks_root()
  if !@sys.exists(tasks_root) {
    return files
  }
  for task in @sys.subdirectory_names(tasks_root) {
    let task_dir = "\{tasks_root}/\{task}"
    for dir in [task_dir, "\{task_dir}/_readonly"] {
      for worktree in @layout.list_worktree_names(dir) {
        scan_node_modules("\{dir}/\{worktree}", false, seen, files)
      }
    }
  }
  files
}

///|
/// 走査時から変更されていないか(ctime は内容・mode のどちらが変わっても更新される)。
fn unchanged(path : String, scanned : @sys.Stat) -> Bool {
  @sys.lstat(path) is Some(now) &&
  now.inode == scanned.inode &&
  now.size == scanned.size &&
  now.ctime_ns == scanned.ctime_ns
}

///|
/// `target` を `representative` の clonefile で置き換える。複製できなければ false。
/// 一時ファイルを残さないよう、複製から rename までは中断させない。
async fn replace_with_clone(
  target : String,
  representative : String,
) -> Bool nocancel {
  @async.protect_from_cancel(() => {
    let temporary = "\{@path.Path(target).dirname()}/.ws-compact-\{@path.Path(target).basename()}"
    @fs.remove(temporary, allow_missing=true)
    if !@sys.clonefile(representative, temporary) {
      return false
    }
    errdefer @fs.remove(temporary, allow_missing=true)
    // 同じディレクトリ内の rename なので、対象は旧内容か新内容のどちらかとして常に存在する
    @fs.rename(temporary, target)
    true
  })
}

///|
fn format_bytes(bytes : Int64) -> String {
  let tenths = bytes * 10L / 1048576L
  "\{tenths / 10L}.\{tenths % 10L} MiB"
}

///|
/// 全 task の worktree の node_modules にある同一ファイル(サイズ・SHA-256・mode が一致)を、代表ファイルの clonefile に
/// 置き換えてディスク容量を減らす。`dry_run` なら何も変更しない。終了コードを返す。
pub async fn run(dry_run~ : Bool) -> Int32 {
  let available_before = @sys.available_bytes(@layout.root())
  let indexed = load_index()
  let files = scan_tasks()
  @sys.log("走査しました: \{files.length()} ファイル")

  let candidates_by_size : Map[Int64, Array[Candidate]] = Map([])
  for file in files {
    let (path, stat) = file
    let recorded = indexed.get(path).filter(entry => entry.matches(stat))
    let candidate : Candidate = {
      path,
      stat,
      recorded,
      hash: recorded.bind(entry => entry.hash),
    }
    match candidates_by_size.get(stat.size) {
      Some(group) => group.push(candidate)
      None => candidates_by_size[stat.size] = [candidate]
    }
  }

  let to_hash = candidates_by_size
    .values()
    .filter(group => group.length() >= 2)
    .flat_map(group => group.iter().filter(candidate => candidate.hash is None))
    .collect()
  for count, candidate in to_hash {
    candidate.hash = Some(@sys.sha256_hex(candidate.path))
    if (count + 1) % 1000 == 0 {
      @sys.log("ハッシュ計算中: \{count + 1} / \{to_hash.length()}")
    }
  }
  @sys.log("ハッシュ計算: \{to_hash.length()} ファイル")

  let next_entries = Map([])
  let replacements = []
  for _, group in candidates_by_size {
    let by_key : Map[(String, Int32), Array[Candidate]] = Map([])
    for candidate in group {
      match candidate.hash {
        None => next_entries[candidate.path] = candidate.entry(shared=false)
        Some(hash) =>
          match by_key.get((hash, candidate.stat.mode)) {
            Some(members) => members.push(candidate)
            None => by_key[(hash, candidate.stat.mode)] = [candidate]
          }
      }
    }
    for _, members in by_key {
      // 前回の代表が変わらず残っていればそれを使い続け、そのクローン済みのものは置き換えない
      let previous = members.search_by(file => {
        file.recorded is Some({ shared: false, .. })
      })
      let representative = members[previous.unwrap_or(0)]
      next_entries[representative.path] = representative.entry(shared=false)
      for file in members {
        if physical_equal(file, representative) {
          continue
        }
        if previous is Some(_) && file.recorded is Some({ shared: true, .. }) {
          next_entries[file.path] = file.entry(shared=true)
        } else {
          replacements.push((file, representative))
        }
      }
    }
  }
  let saving = replacements.fold(init=0L, (sum, pair) => sum + pair.0.stat.size)
  if dry_run {
    @sys.log(
      "置き換え対象: \{replacements.length()} ファイル(削減見込み \{format_bytes(saving)})。--dry-run のため変更していません",
    )
    return 0
  }

  let mut replaced = 0
  let mut replaced_bytes = 0L
  let mut changed = 0
  let mut not_cloned = 0
  for pair in replacements {
    let (target, representative) = pair
    // 走査後に変更されたファイルは、内容が変わっている可能性があるので触らない
    if !unchanged(target.path, target.stat) ||
      !unchanged(representative.path, representative.stat) {
      changed += 1
    } else if replace_with_clone(target.path, representative.path) {
      replaced += 1
      replaced_bytes += target.stat.size
      // 置き換えで inode と ctime が変わるので、置き換え後の状態を「共有済み」として記録する
      if @sys.lstat(target.path) is Some(stat) {
        next_entries[target.path] = { ..target, stat, }.entry(shared=true)
      }
    } else {
      not_cloned += 1
    }
  }
  save_index(next_entries)
  @sys.log(
    "置き換え: \{replaced} ファイル(削減見込み \{format_bytes(replaced_bytes)})",
  )
  if changed > 0 || not_cloned > 0 {
    @sys.log(
      "スキップ: 走査後に変更されていた \{changed} ファイル、複製できなかった \{not_cloned} ファイル",
    )
  }
  if available_before is Some(before) &&
    @sys.available_bytes(@layout.root()) is Some(after) {
    @sys.log("空き容量: \{format_bytes(before)} → \{format_bytes(after)}")
  }
  0
}
