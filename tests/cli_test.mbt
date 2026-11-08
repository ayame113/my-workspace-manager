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
