///|
priv struct PullEntry {
  kind : String
  name : String
  worktree : String
}

///|
priv struct Outcome {
  status : String
  detail : String
}

///|
async fn remote_branches_matching_slug(
  bare : String,
  slug : String,
) -> Array[String] {
  @sys.split_to_array(
    @git.output([
      "-C", bare, "for-each-ref", "--format=%(refname:strip=3)", "refs/remotes/origin/",
    ]),
    "\n",
  ).filter(name => {
    name != "" && name != "HEAD" && @layout.branch_slug(name) == slug
  })
}

///|
async fn update_readonly_worktree(
  bare : String,
  worktree : String,
  slug : String,
) -> Outcome {
  if @git.is_dirty(worktree) {
    return { status: "skipped", detail: "未コミット変更あり", }
  }
  let matches = remote_branches_matching_slug(bare, slug)
  if matches.length() != 1 {
    return {
      status: "error",
      detail: "slug に一致する origin ブランチが \{matches.length()} 件: \{matches.join(" ")}",
    }
  }
  let target = "origin/\{matches[0]}"
  let before = @git.output(["-C", worktree, "rev-parse", "HEAD"])
  if before == @git.output(["-C", worktree, "rev-parse", target]) {
    return { status: "up-to-date", detail: target, }
  }
  ignore(
    @git.output(["-C", worktree, "checkout", "--quiet", "--detach", target]),
  )
  {
    status: "updated",
    detail: "\{target} (\{@git.output(["-C", worktree, "rev-parse", "--short", "HEAD"])})",
  }
}

///|
async fn update_work_worktree(worktree : String) -> Outcome {
  guard @git.current_branch(worktree) is Some(branch) else {
    return { status: "skipped", detail: "detached HEAD", }
  }
  if @git.is_dirty(worktree) {
    return { status: "skipped", detail: "未コミット変更あり", }
  }
  let upstream = @git.run([
    "-C", worktree, "rev-parse", "--verify", "--quiet", "@{upstream}",
  ])
  if !upstream.ok {
    return { status: "skipped", detail: "upstream なし", }
  }
  let head = @git.output(["-C", worktree, "rev-parse", "HEAD"])
  if head == upstream.stdout {
    return { status: "up-to-date", detail: branch, }
  }
  if @git.is_ancestor(worktree, upstream.stdout, head) {
    return {
      status: "up-to-date",
      detail: "\{branch} (upstream より先行)",
    }
  }
  if !@git.is_ancestor(worktree, head, upstream.stdout) {
    return {
      status: "skipped",
      detail: "\{branch} は upstream と分岐しています(fast-forward 不可)",
    }
  }
  ignore(
    @git.output(["-C", worktree, "merge", "--quiet", "--ff-only", "@{upstream}"]),
  )
  {
    status: "updated",
    detail: "\{branch} (\{@git.output(["-C", worktree, "rev-parse", "--short", "HEAD"])})",
  }
}

///|
async fn cmd_pull(args : Array[String]) -> Int32 {
  @layout.refuse_inside_agent("ws pull")
  let parsed = parse_command_args(args, [text_option("task")], 0, 0)
  let task = @layout.resolve_existing_task(
    parsed.text("task"),
    "--task <task> を指定してください",
  )
  let dir = @layout.task_dir(task)
  let readonly_dir = @sys.join([dir, "_readonly"])
  let entries : Array[PullEntry] = []
  for name in @layout.list_worktree_names(dir) {
    entries.push({ kind: "work", name, worktree: @sys.join([dir, name]), })
  }
  for name in @layout.list_worktree_names(readonly_dir) {
    entries.push({
      kind: "ro",
      name,
      worktree: @sys.join([readonly_dir, name]),
    })
  }
  if entries.length() == 0 {
    @sys.log("worktree はありません")
    return 0
  }

  let repos : Array[String] = []
  for entry in entries {
    let repo = @layout.repo_of(entry.name)
    if !repos.contains(repo) {
      repos.push(repo)
    }
  }
  let fetch_failures : Map[String, String] = Map([])
  for repo in repos {
    try {
      let bare = @layout.require_bare(repo)
      ignore(@git.output(["-C", bare, "fetch", "--quiet", "origin", "--prune"]))
    } catch {
      error =>
        match @errors.expected_message(error) {
          Some(message) => fetch_failures.set(repo, @sys.trim(message))
          None => raise error
        }
    }
  }

  let results : Array[(PullEntry, Outcome)] = []
  for entry in entries {
    let repo = @layout.repo_of(entry.name)
    let outcome = match fetch_failures.get(repo) {
      Some(failure) => { status: "error", detail: failure, }
      None =>
        try {
          if entry.kind == "ro" {
            update_readonly_worktree(
              @layout.bare_path(repo),
              entry.worktree,
              @sys.slice_from(entry.name, repo.length() + 2),
            )
          } else {
            update_work_worktree(entry.worktree)
          }
        } catch {
          error =>
            match @errors.expected_message(error) {
              Some(message) => { status: "error", detail: @sys.trim(message), }
              None => raise error
            }
        }
    }
    results.push((entry, outcome))
  }

  for result in results {
    let (entry, outcome) = result
    @sys.log(
      "\{entry.kind.pad_end(5, ' ')} \{entry.name.pad_end(55, ' ')} \{outcome.status.pad_end(10, ' ')} \{outcome.detail}",
    )
  }
  if results.iter().any(result => result.1.status == "error") {
    1
  } else {
    0
  }
}
