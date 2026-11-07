///|
test "iso timestamp" {
  inspect(@sys.format_iso_timestamp(0L), content="1970-01-01T00:00:00.000Z")
  inspect(
    @sys.format_iso_timestamp(1794047642039L),
    content="2026-11-07T10:34:02.039Z",
  )
  inspect(
    @sys.format_iso_timestamp(951782400000L),
    content="2000-02-29T00:00:00.000Z",
  )
}

///|
test "trim like javascript" {
  inspect(@sys.trim("\u{feff} a b\n\u{3000}"), content="a b")
  inspect(@sys.trim("\u{0}x"), content="\u{0}x")
}
