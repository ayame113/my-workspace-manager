///|
test "branch slug" {
  inspect(
    @layout.branch_slug("feature/ABC-123/add_user_api"),
    content="feature-abc-123-add-user-api",
  )
  inspect(@layout.branch_slug("release/v1.2"), content="release-v1-2")
  inspect(@layout.branch_slug("--a//b--"), content="a-b")
  inspect(@layout.branch_slug("日本語"), content="")
  inspect(@layout.branch_slug("\u{130}x"), content="i-x")
  inspect(@layout.branch_slug("release/wave3.1"), content="release-wave3-1")
}

///|
test "names" {
  inspect(@layout.is_valid_task_name("t-one"), content="true")
  inspect(@layout.is_valid_task_name("Bad_Task"), content="false")
  inspect(@layout.is_valid_task_name("a--b"), content="false")
  inspect(@layout.is_valid_task_name("-a"), content="false")
  inspect(@layout.is_repo_name_shape("example-API.docs_x"), content="true")
  inspect(@layout.is_repo_name_shape(".x"), content="false")
  inspect(@layout.repo_of("myrepo--fresh"), content="myrepo")
  inspect(@layout.repo_of("plain"), content="plain")
  inspect(@layout.parse_worktree_name("random-dir") is None, content="true")
  inspect(@layout.parse_worktree_name("--x") is None, content="true")
  inspect(
    @layout.parse_worktree_name("npmrepo--d9")
    .map(name => name.slug)
    .unwrap_or(""),
    content="d9",
  )
}
