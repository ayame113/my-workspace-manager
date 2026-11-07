///|
// バッファされる println だと、stdio を継承した子プロセスの出力より後に表示されてしまうため、fd に直接書く

///|
let epipe = 32

///|
// 読み手が先に終了しても(`ws ls | head -1`)処理を途中で止めないよう、閉じたパイプへの出力は捨てる
async fn write_ignoring_closed_pipe(
  output : @stdio.Output,
  text : String,
) -> Unit {
  output.write(@utf8.encode(text)) catch {
    @os_error.OsError(_, raw_code~, ..) if raw_code == epipe => ()
    error => raise error
  }
}

///|
pub async fn write_stdout(text : String) -> Unit {
  write_ignoring_closed_pipe(@stdio.stdout, text)
}

///|
pub async fn write_stderr(text : String) -> Unit {
  write_ignoring_closed_pipe(@stdio.stderr, text)
}

///|
pub async fn log(text : String) -> Unit {
  write_stdout(text + "\n")
}

///|
pub async fn log_error(text : String) -> Unit {
  write_stderr(text + "\n")
}
