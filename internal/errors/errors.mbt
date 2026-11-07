///|
/// 想定内の失敗。`ws: <message>` を表示して終了コード 1 にする。
pub(all) suberror WsError {
  WsError(String)
}

///|
/// git が非 0 で終了した。git の stderr をそのまま持つ。
pub(all) suberror GitError {
  GitError(String)
}

///|
/// `WsError` か `GitError` ならそのメッセージ、それ以外は `None`。
pub fn expected_message(error : Error) -> String? {
  match error {
    WsError(message) | GitError(message) => Some(message)
    _ => None
  }
}
