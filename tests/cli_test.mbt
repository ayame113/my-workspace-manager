///|
async test "--version matches moon.mod" {
  let s = Sandbox::new()
  defer s.cleanup()
  let declared = @fs.read_text("moon.mod")
    .split("\n")
    .find_first(line => line.has_prefix("version = "))
    .map(line => line["version = ".length():].trim(chars="\"").to_owned())
  let outcome = s.exec(ws_binary.wait(), ["--version"])
  inspect(Some(outcome.stdout.trim().to_owned()) == declared, content="true")
  inspect(outcome.code, content="0")
}

///|
async test "Ctrl-C during npm ci leaves no partial node_modules" {
  let s = Sandbox::new()
  defer s.cleanup()
  s.setup_npm()
  s.setup_ws([
    "add", "--task", "t1", "--repo", "npmrepo", "--no-install", "--branch", "b1",
  ])
  write(
    "\{s.root}/fakebin/npm",
    "#!/bin/bash\nmkdir -p node_modules/partial\nexec sleep 30\n",
  )
  let worktree = "\{s.tasks}/t1/npmrepo--b1"
  let binary = ws_binary.wait()
  @async.with_task_group(group => {
    let ws = @process.spawn(
      group,
      binary,
      ["npm", "ci"],
      inherit_env=false,
      extra_env=s.env,
      cwd=worktree,
      stdin=Empty,
      stdout=ToFile("\{s.root}/out.txt"),
      stderr=ToFile("\{s.root}/err.txt"),
    )
    for _ in 0..<100 {
      if @fs.exists("\{worktree}/node_modules/partial") {
        break
      }
      @async.sleep(100)
    }
    ignore(@process.run("kill", ["-INT", ws.pid.to_string()]))
    s.note("rc=\{ws.wait()}")
  })
  s.note("stdout: \{s.normalize(@fs.read_text("\{s.root}/out.txt"))}")
  s.note("stderr: \{s.normalize(@fs.read_text("\{s.root}/err.txt"))}")
  s.note("node_modules left: \{@fs.exists("\{worktree}/node_modules")}")
  let cache = "\{s.ws_root}/.cache/node_modules"
  s.note(
    "cache entries: \{if @fs.exists(cache) { @fs.readdir(cache, include_hidden=true).length() } else { 0 }}",
  )
  inspect(
    s.transcript.to_string(),
    content=(
      #|rc=-2
      #|stdout: 
      #|stderr: 
      #|node_modules left: false
      #|cache entries: 0
      #|
    ),
  )
}
