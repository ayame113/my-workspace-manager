# my-workspace-manager

git worktree をタスク単位で管理する個人用 CLI `ws` と、それを Claude Code から使うためのスキル(`skills/ws-task`)。

`~/ws/repos/<repo>.git` に bare repo を置き、タスクごとのディレクトリ `~/ws/tasks/<task>/` に作業用 worktree(`<repo>--<branch-slug>`)と参照用の readonly worktree(`_readonly/`)を並べる。node_modules は普段どおり `npm ci` / `npm i` で用意し、容量が気になったら `ws compact` で worktree 間の同一ファイルを APFS の clonefile で共有して減らす。規約や運用の詳細は [skills/ws-task/SKILL.md](skills/ws-task/SKILL.md) を参照。

## 前提条件

- macOS 専用(`ws compact` が APFS の `clonefile(2)` を使うため)
- git
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

トップレベルは workspace 全体、`ws worktree` は task 内が対象。

| コマンド | 内容 |
| --- | --- |
| `ws new <task>` | `tasks/<task>/` を作る |
| `ws clone <url> [<repo>]` | bare repo を `repos/` に用意する |
| `ws ls` | 全 task の一覧(1 task 1 行。worktree 数と dirty 数) |
| `ws lint [--fix]` | workspace 全体の規約違反・壊れた worktree を検査する |
| `ws compact [--dry-run]` | 全 task の node_modules 内の同一ファイルを clonefile で共有し、容量を減らす |
| `ws worktree add --repo <repo> --branch <branch> [-c --base <start-point> \| --readonly]` | worktree を作る |
| `ws worktree rename --repo <repo> --branch <old> --to <new>` | 作業用 worktree のブランチ名を変える。slug が変わればディレクトリも移動する |
| `ws worktree ls` | task 内の worktree の一覧 |
| `ws worktree lint [--fix]` | task 内の規約違反・壊れた worktree を検査する |
| `ws worktree pull` | fetch して task 内の worktree を fast-forward で追従させる |

`ws worktree` 配下のコマンドは `--task <task>` を省略するとカレントディレクトリが属する task を使う。詳しくは `ws --help` / `ws worktree --help` / `ws worktree <command> --help`。

`ws lint` は全 task の検査に加え、git に登録されているがディレクトリが無いもののうち、どの task にも属さないものも報告する。`ws worktree lint` は指定した task の分だけを報告する。

終了コードは 0 が成功、1 が実行時の失敗(lint で未解決の問題がある場合を含む)、2 が使い方の誤り。

### ws worktree rename

作業用 worktree のブランチをローカルでリネームする。ネットワークは使わないので、エージェントからも実行できる。

- 事前に、worktree が `<old>` を checkout していること、`<new>` が妥当な名前でローカルに無いこと、slug が変わるなら移動先が空いていることを確認する。slug が変わる場合、ws を実行しているカレントディレクトリが対象 worktree 配下だと拒否する。
- `git branch -m` の後、slug が変わるときだけ `git worktree move` する。移動に失敗したらブランチ名を戻す。
- `<old>` に upstream があった場合は upstream を外し、リモートへの反映手順(`git push -u origin <new>` と旧ブランチの削除)を表示する。リモートは変更しない。`_readonly/` の worktree は対象外。

### ws compact

`$WS_ROOT/tasks` 配下の全 worktree の node_modules(入れ子を含む)にある通常ファイルのうち、サイズ・SHA-256・mode が同じものを、代表ファイルの clonefile(copy-on-write)に原子的に置き換える。symlink と空ファイルは対象外。

- 処理したファイルの inode・ctime・size・ハッシュと共有済みかどうかを `$WS_ROOT/.compact-index` に記録する。2 回目以降は記録と一致する(変更されていない)ファイルのハッシュ計算と置き換えを省き、追加・変更されたファイルだけを処理する(npm は展開時に mtime を固定値にするため ctime を使う)。インデックスを消すと全件処理に戻る。
- 表示する削減見込みは置き換えたファイルのサイズの合計。`du` はクローンを区別しないので、実際の削減量は表示される実行前後の空き容量の差で見る。
- 実行中の dev server 等が開いているファイルは旧 inode のまま残るので、影響はほぼ無い。

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
