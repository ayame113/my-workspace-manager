///|
test "branch slug and names" {
  inspect(
    @layout.branch_slug("feature/ABC-123/add_user_api"),
    content="feature-abc-123-add-user-api",
  )
  inspect(@layout.branch_slug("--release//v1.2--"), content="release-v1-2")
  inspect(@layout.branch_slug("日本語"), content="")
  inspect(@layout.is_valid_task_name("t-one"), content="true")
  inspect(@layout.is_valid_task_name("a--b"), content="false")
  inspect(@layout.is_repo_name_shape("example-API.docs_x"), content="true")
  inspect(@layout.is_repo_name_shape(".x"), content="false")
  inspect(@layout.parse_worktree_name("random-dir") is None, content="true")
  inspect(@layout.split_worktree_name("plain").repo, content="plain")
}
