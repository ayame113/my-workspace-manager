///|
async fn is_ws_cwd_inside(directory : String) -> Bool {
  let cwd = @fs.realpath(@env.current_dir().unwrap_or("."))
  let root = @fs.realpath(directory)
  cwd == root || cwd.has_prefix(root + "/")
}

///|
// branch -m が成功してから worktree move が失敗した場合に戻せるよう、2 つを 1 単位として中断させない
async fn rename_branch_and_move(
  bare : String,
  old_worktree : String,
  new_worktree : String,
  old : String,
  new : String,
  had_upstream~ : Bool,
) -> Unit {
  @async.protect_from_cancel(() => {
    ignore(
      @git.output(["-C", old_worktree, "branch", "--quiet", "-m", old, new]),
    )
    let moved = old_worktree != new_worktree
    if moved {
      let relocation = @git.run([
        "-C", bare, "worktree", "move", old_worktree, new_worktree,
      ])
      if relocation.exit_code != 0 {
        ignore(
          @git.run(["-C", old_worktree, "branch", "--quiet", "-m", new, old]),
        )
        raise @errors.GitError(relocation.stderr)
      }
    }
    if had_upstream {
      ignore(
        @git.output([
          "-C", new_worktree, "branch", "--quiet", "--unset-upstream", new,
        ]),
      )
    }
  })
}

///|
async fn cmd_rename(
  task : String,
  repo : String,
  old : String,
  new : String,
) -> Int32 {
  let bare = @layout.require_bare(repo)
  let old_slug = @layout.slug_or_raise(old)
  let new_slug = @layout.slug_or_raise(new)
  let old_worktree = "\{@layout.task_dir(task)}/\{repo}--\{old_slug}"
  let new_worktree = "\{@layout.task_dir(task)}/\{repo}--\{new_slug}"
  if !@sys.exists("\{old_worktree}/.git") ||
    @git.current_branch(old_worktree) != Some(old) {
    raise @errors.WsError(
      "作業用 worktree \{old_worktree} が無いか、ブランチ '\{old}' を checkout していません",
    )
  }
  // --branch は @{-1} などの略記を展開するので、入力と一致しない名前は受け付けない
  let check = @git.run(["check-ref-format", "--branch", new])
  let valid_name = check.exit_code == 0 && check.stdout == new
  if !valid_name {
    raise @errors.WsError("ブランチ名 '\{new}' は git で使えません")
  }
  if @git.ref_exists(bare, "refs/heads/\{new}") {
    raise @errors.WsError(
      "ローカルブランチ '\{new}' は既に存在します",
    )
  }
  let moved = old_slug != new_slug
  if moved {
    if @sys.exists(new_worktree) {
      raise @errors.WsError(
        "移動先 \{new_worktree} は既に存在します",
      )
    }
    if is_ws_cwd_inside(old_worktree) {
      raise @errors.WsError(
        "カレントディレクトリが \{old_worktree} 配下のため、worktree を移動できません。別のディレクトリで実行してください",
      )
    }
  }
  // origin 側が prune 済みだと @{upstream} は解決できないが設定は残るので、設定そのものを見る
  let had_upstream = @git.succeeds([
    "-C",
    old_worktree,
    "config",
    "--get",
    "branch.\{old}.merge",
  ])
  rename_branch_and_move(
    bare,
    old_worktree,
    if moved {
      new_worktree
    } else {
      old_worktree
    },
    old,
    new,
    had_upstream~,
  )
  @sys.log("リネームしました: \{old} → \{new}")
  if moved {
    @sys.log("新しいパス: \{new_worktree}")
  }
  if had_upstream {
    @sys.log(
      (
        $|origin/\{old} は残っています。リモートに反映するには(別ターミナルで):
        $|  git push -u origin \{new}
        $|  git push origin --delete \{old}
        $|※ \{old} の PR がある場合は、代わりに GitHub の Branches 画面でリネームし、
        $|  ws pull の後に git branch -u origin/\{new} を実行してください
      ),
    )
  }
  0
}
