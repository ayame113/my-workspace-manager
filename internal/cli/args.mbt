// usage エラーになる入力を旧実装と揃えるため、Node.js の `util.parseArgs`(strict、位置引数あり)と同じ規則で解析する。
// `--name value` / `--name=value` / 短縮フラグのまとめ書き / `--` 以降は位置引数 / `-` で始まる値(`--task --x`)は拒否

///|
pub(all) enum OptionKind {
  Text
  Flag
}

///|
pub(all) struct OptionSpec {
  name : String
  kind : OptionKind
  short : Char?
}

///|
pub fn text_option(name : String) -> OptionSpec {
  { name, kind: Text, short: None, }
}

///|
pub fn flag_option(name : String, short? : Char) -> OptionSpec {
  { name, kind: Flag, short, }
}

///|
pub struct ParsedArgs {
  texts : Map[String, String]
  flags : Set[String]
  positionals : Array[String]
}

///|
pub fn ParsedArgs::text(self : ParsedArgs, name : String) -> String? {
  self.texts.get(name)
}

///|
pub fn ParsedArgs::flag(self : ParsedArgs, name : String) -> Bool {
  self.flags.contains(name)
}

///|
fn find_long(
  specs : Array[OptionSpec],
  name : String,
) -> OptionSpec raise @errors.UsageError {
  guard specs.search_by(spec => spec.name == name) is Some(index) else {
    raise @errors.UsageError
  }
  specs[index]
}

///|
fn find_short(
  specs : Array[OptionSpec],
  short : Char,
) -> OptionSpec raise @errors.UsageError {
  guard specs.search_by(spec => spec.short == Some(short)) is Some(index) else {
    raise @errors.UsageError
  }
  specs[index]
}

///|
fn looks_like_option(value : String) -> Bool {
  value.length() > 1 && value.has_prefix("-")
}

///|
/// `args` を `specs` に従って解析する。未知・不正なオプション、または位置引数の数が
/// `min_positionals..=max_positionals` の外なら `UsageError`。
pub fn parse_command_args(
  args : Array[String],
  specs : Array[OptionSpec],
  min_positionals : Int32,
  max_positionals : Int32,
) -> ParsedArgs raise @errors.UsageError {
  let parsed : ParsedArgs = { texts: Map([]), flags: Set([]), positionals: [], }
  let pending = args.copy()
  pending.rev_in_place()
  let take_value = (spec : OptionSpec) => {
    guard pending.pop() is Some(value) && !looks_like_option(value) else {
      raise @errors.UsageError
    }
    parsed.texts.set(spec.name, value)
  }
  while pending.pop() is Some(arg) {
    if arg == "--" {
      while pending.pop() is Some(rest) {
        parsed.positionals.push(rest)
      }
      break
    }
    if arg.length() < 2 || !arg.has_prefix("-") {
      parsed.positionals.push(arg)
      continue
    }
    if arg.has_prefix("--") {
      let body = arg.exact_view(start=2).to_owned()
      match body.find("=") {
        Some(separator) if separator >= 1 => {
          let spec = find_long(specs, body.exact_view(end=separator).to_owned())
          guard spec.kind is Text else { raise @errors.UsageError }
          parsed.texts.set(
            spec.name,
            body.exact_view(start=separator + 1).to_owned(),
          )
        }
        _ => {
          let spec = find_long(specs, body)
          match spec.kind {
            Text => take_value(spec)
            Flag => parsed.flags.add(spec.name)
          }
        }
      }
      continue
    }
    let shorts = arg.exact_view(start=1).iter().collect()
    for index, short in shorts {
      let spec = find_short(specs, short)
      match spec.kind {
        Flag => parsed.flags.add(spec.name)
        Text if index == shorts.length() - 1 => take_value(spec)
        Text => {
          parsed.texts.set(spec.name, String::from_array(shorts[index + 1:]))
          break
        }
      }
    }
  }
  let count = parsed.positionals.length()
  if count < min_positionals || count > max_positionals {
    raise @errors.UsageError
  }
  parsed
}
