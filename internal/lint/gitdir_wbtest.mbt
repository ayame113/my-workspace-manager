///|
test "gitdir line matching follows the javascript regex" {
  inspect(
    match_gitdir_line("gitdir: /a/b\n").unwrap_or("<none>"),
    content="/a/b",
  )
  inspect(match_gitdir_line("x\ngitdir:/c").unwrap_or("<none>"), content="/c")
  inspect(match_gitdir_line("gitdir:\n/d").unwrap_or("<none>"), content="/d")
  inspect(match_gitdir_line("gitdir:  ").unwrap_or("<none>"), content=" ")
  inspect(match_gitdir_line("gitdir:\n").unwrap_or("<none>"), content="<none>")
  inspect(
    match_gitdir_line(" gitdir: /e").unwrap_or("<none>"),
    content="<none>",
  )
}
