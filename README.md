# my-workspace-manager

git worktree をタスク単位で管理する個人用 CLI `ws` と、それを Claude Code から使うためのスキル(`skills/ws-task`)。

`~/ws/repos/<repo>.git` に bare repo を置き、タスクごとのディレクトリ `~/ws/tasks/<task>/` に作業用 worktree(`<repo>--<branch-slug>`)と参照用の readonly worktree(`_readonly/`)を並べる。node_modules は package-lock.json のハッシュと Node の ABI をキーに共有キャッシュし、APFS の clonefile で複製する。規約や運用の詳細は [skills/ws-task/SKILL.md](skills/ws-task/SKILL.md) を参照。

## 前提条件

- macOS 専用(node_modules の複製に `clonefile(2)` と `cp -c` を使うため)
- git
- npm / node(node_modules キャッシュを使う場合。nodenv の `.node-version` に従う)
- [MoonBit ツールチェーン](https://www.moonbitlang.com/download/)。標準ライブラリに `moonbitlang/core/async` を含む版が必要(moonc v0.10.20 で確認)

## インストール

```sh
# clone 済みのリポジトリから
moon install ./cmd/ws

# clone せずに git URL から
moon install https://github.com/ayame113/my-workspace-manager.git cmd/ws
```

`~/.moon/bin/ws` に入るので、`~/.moon/bin` を PATH に入れる。更新するときは同じコマンドを再実行する。

### Claude Code のスキル

```
/plugin marketplace add ayame113/my-workspace-manager
/plugin install ws@my-workspace-manager
```

## 使い方

| コマンド | 内容 |
| --- | --- |
| `ws new <task>` | `tasks/<task>/` を作る |
| `ws clone <url> [<repo>]` | bare repo を `repos/` に用意する |
| `ws add --repo <repo> --branch <branch> [-c --base <start-point> \| --readonly] [--no-install]` | worktree を作り、node_modules を用意する |
| `ws pull` | fetch して task 内の worktree を fast-forward で追従させる |
| `ws ls [--all]` | worktree の一覧 |
| `ws npm ci [-- <npm ci の引数>]` | node_modules を共有キャッシュ経由で用意する |
| `ws lint [--all] [--fix]` | 規約違反・壊れた worktree・node_modules の状態・削除が中断された trash の残骸を検査する |

`--task` を省略するとカレントディレクトリが属する task を使う。詳しくは `ws --help` / `ws <command> --help`。

終了コードは 0 が成功、1 が実行時の失敗(lint で未解決の問題がある場合を含む)、2 が使い方の誤り。`ws npm ci` で npm が失敗した場合は npm の終了コードを返す。

### 環境変数

| 変数 | 内容 |
| --- | --- |
| `WS_ROOT` | ワークスペースのルート。既定は `~/ws` |
| `WS_TEST_BIN` | 統合テストで使う ws バイナリ。未設定ならテストがビルドする |

## 開発

```sh
moon test --target native   # 単体テストと統合テスト(一時ディレクトリにローカル file:// リモートを作って ws を実行する)
moon check --deny-warn
moon fmt
moon info                   # pkg.generated.mbti を更新する
```
