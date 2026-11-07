///|
fn add_specs() -> Array[@cli.OptionSpec] {
  [
    @cli.text_option("task"),
    @cli.text_option("repo"),
    @cli.flag_option("create", short='c'),
    @cli.flag_option("no-install"),
  ]
}

///|
fn describe(args : Array[String], min~ : Int32 = 0, max~ : Int32 = 0) -> String {
  let parsed = @cli.parse_command_args(args, add_specs(), min, max) catch {
    _ => return "usage error"
  }
  let flags = ["create", "no-install"].filter(name => parsed.flag(name))
  "task=\{parsed.text("task").unwrap_or("-")} repo=\{parsed.text("repo").unwrap_or("-")} flags=\{flags.join(",")} positionals=\{parsed.positionals.join(",")}"
}

///|
test "long options with separate and inline values" {
  inspect(
    describe(["--task", "t", "--repo=r"]),
    content="task=t repo=r flags= positionals=",
  )
  inspect(
    describe(["--task=", "--no-install"]),
    content="task= repo=- flags=no-install positionals=",
  )
  inspect(
    describe(["--task", "a", "--task", "b"]),
    content="task=b repo=- flags= positionals=",
  )
  inspect(
    describe(["--task", "-"]),
    content="task=- repo=- flags= positionals=",
  )
}

///|
test "short flags and groups" {
  inspect(describe(["-c"]), content="task=- repo=- flags=create positionals=")
  inspect(describe(["-cc"]), content="task=- repo=- flags=create positionals=")
  inspect(describe(["-x"]), content="usage error")
  inspect(describe(["-cx"]), content="usage error")
}

///|
test "rejected forms" {
  inspect(describe(["--task"]), content="usage error")
  inspect(describe(["--task", "--bogus"]), content="usage error")
  inspect(describe(["--bogus"]), content="usage error")
  inspect(describe(["--create=yes"]), content="usage error")
  inspect(describe(["--=x"]), content="usage error")
  inspect(describe(["extra"]), content="usage error")
}

///|
test "positionals and double dash" {
  inspect(
    describe(["a", "--task", "t", "-", "--", "--repo"], max=3),
    content="task=t repo=- flags= positionals=a,-,--repo",
  )
  inspect(describe(["--task=t"], min=1, max=1), content="usage error")
}
