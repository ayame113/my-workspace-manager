///|
/// コマンドラインの誤り。usage を表示して終了コード 2 にする。
pub(all) suberror UsageError

///|
/// ユーザー向けメッセージ付きの想定内の失敗。`ws: <message>` を表示して終了コード 1 にする。
pub(all) suberror WsError {
  WsError(String)
}

///|
/// git が非 0 で終了した。git の stderr をそのまま持つ。
pub(all) suberror GitError {
  GitError(String)
}

///|
/// `WsError` か `GitError` ならそのメッセージ、それ以外のエラーなら `None`。
pub fn expected_message(error : Error) -> String? {
  match error {
    WsError(message) => Some(message)
    GitError(message) => Some(message)
    _ => None
  }
}
