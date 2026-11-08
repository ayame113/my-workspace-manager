///|
priv struct Outcome {
  status : String
  detail : String
}

///|
async fn update_readonly(
  bare : String,
  worktree : String,
  slug : String,
) -> Outcome {
  if @git.is_dirty(worktree) {
    return { status: "skipped", detail: "未コミット変更あり", }
  }
  let matches = @git.output([
      "-C", bare, "for-each-ref", "--format=%(refname:strip=3)", "refs/remotes/origin/",
    ])
    .split("\n")
    .filter(name => {
      name != "" &&
      name != "HEAD" &&
      @layout.branch_slug(name.to_owned()) == slug
    })
    .map(name => name.to_owned())
    .collect()
  guard matches is [branch] else {
    return {
      status: "error",
      detail: "slug に一致する origin ブランチが \{matches.length()} 件: \{matches.join(" ")}",
    }
  }
  let target = "origin/\{branch}"
  let rev_parse = async fn(rev : String) {
    @git.output(["-C", worktree, "rev-parse", rev])
  }
  if rev_parse("HEAD") == rev_parse(target) {
    return { status: "up-to-date", detail: target, }
  }
  ignore(
    @git.output(["-C", worktree, "checkout", "--quiet", "--detach", target]),
  )
  { status: "updated", detail: "\{target} (\{@git.short_head(worktree)})", }
}

///|
async fn update_work(worktree : String) -> Outcome {
  guard @git.current_branch(worktree) is Some(branch) else {
    return { status: "skipped", detail: "detached HEAD", }
  }
  if @git.is_dirty(worktree) {
    return { status: "skipped", detail: "未コミット変更あり", }
  }
  let upstream = @git.run([
    "-C", worktree, "rev-parse", "--verify", "--quiet", "@{upstream}",
  ])
  if upstream.exit_code != 0 {
    return { status: "skipped", detail: "upstream なし", }
  }
  let head = @git.short_head(worktree)
  let merge = @git.run([
    "-C", worktree, "merge", "--quiet", "--ff-only", "@{upstream}",
  ])
  if merge.exit_code != 0 {
    // 先頭は hint: の説明が続くことがあるので、結論の最終行(fatal: ...)を使う
    let reason = merge.stderr.trim().split("\n").last().unwrap_or("")
    return { status: "skipped", detail: "\{branch}: \{reason}", }
  }
  if @git.short_head(worktree) == head {
    { status: "up-to-date", detail: branch, }
  } else {
    { status: "updated", detail: "\{branch} (\{@git.short_head(worktree)})", }
  }
}

///|
fn failure(error : Error) -> Outcome raise {
  match @errors.expected_message(error) {
    Some(message) => { status: "error", detail: message.trim().to_owned(), }
    None => raise error
  }
}

///|
async fn cmd_pull(task_option : String?) -> Int32 {
  @layout.refuse_inside_agent("ws worktree pull")
  let task = @layout.resolve_existing_task(
    task_option, "--task <task> を指定してください",
  )
  let dir = @layout.task_dir(task)
  let entries = []
  for name in @layout.list_worktree_names(dir) {
    entries.push(("work", name, "\{dir}/\{name}"))
  }
  for name in @layout.list_worktree_names("\{dir}/_readonly") {
    entries.push(("ro", name, "\{dir}/_readonly/\{name}"))
  }
  if entries.is_empty() {
    @sys.log("worktree はありません")
    return 0
  }
  let fetch_failures : Map[String, Outcome] = Map([])
  let repos = []
  for entry in entries {
    let repo = @layout.split_worktree_name(entry.1).repo
    if !repos.contains(repo) {
      repos.push(repo)
    }
  }
  for repo in repos {
    try {
      let bare = @layout.require_bare(repo)
      ignore(@git.output(["-C", bare, "fetch", "--quiet", "origin", "--prune"]))
    } catch {
      error => fetch_failures[repo] = failure(error)
    }
  }
  let mut code = 0
  for entry in entries {
    let (kind, name, worktree) = entry
    let parsed = @layout.split_worktree_name(name)
    let outcome = match fetch_failures.get(parsed.repo) {
      Some(outcome) => outcome
      None =>
        try {
          if kind == "ro" {
            update_readonly(
              @layout.bare_path(parsed.repo),
              worktree,
              parsed.slug,
            )
          } else {
            update_work(worktree)
          }
        } catch {
          error => failure(error)
        }
    }
    if outcome.status == "error" {
      code = 1
    }
    @sys.log(
      "\{kind.pad_end(5, ' ')} \{name.pad_end(55, ' ')} \{outcome.status.pad_end(10, ' ')} \{outcome.detail}",
    )
  }
  code
}
