///|
test "join normalizes like node" {
  inspect(@sys.join(["/a/b/", "tasks", "t"]), content="/a/b/tasks/t")
  inspect(@sys.join(["root", "..", "..", "x"]), content="../x")
  inspect(@sys.join(["/", "..", "x"]), content="/x")
  inspect(@sys.join(["/a", "."]), content="/a")
  inspect(@sys.join(["a", "b/"]), content="a/b/")
  inspect(@sys.join(["", ""]), content=".")
}

///|
test "dirname and basename like node" {
  inspect(@sys.dirname("package-lock.json"), content=".")
  inspect(@sys.dirname("bin/package-lock.json"), content="bin")
  inspect(@sys.dirname("/a/b/"), content="/a")
  inspect(@sys.dirname("/a"), content="/")
  inspect(@sys.basename("/a/b.git/"), content="b.git")
  inspect(@sys.basename("git@host:owner/repo.git"), content="repo.git")
  inspect(@sys.basename(""), content="")
}

///|
test "relative like node" {
  inspect(@sys.relative("/r/tasks", "/r/tasks/t/x", cwd="/"), content="t/x")
  inspect(@sys.relative("/r/tasks", "/r/tasks", cwd="/"), content="")
  inspect(
    @sys.relative("/r/tasks", "/r/other/x", cwd="/"),
    content="../other/x",
  )
  inspect(@sys.relative("tasks", "/c/tasks/a", cwd="/c"), content="a")
}
