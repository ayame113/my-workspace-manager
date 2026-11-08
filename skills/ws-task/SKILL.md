---
name: ws-task
description: タスク用の作業ディレクトリ(~/ws/tasks/<task>)の作成、git worktree の追加(作業用 / readonly 参照用 / 新規ブランチ)、一覧、ローカル worktree のリモート追従(ws pull)、node_modules の重複排除によるディスク容量の削減(ws compact)、規約違反の検査(ws lint)をするときに使う。「タスクを作って」「worktreeを追加」「〇〇ブランチを参照用に」「新しいブランチで作業を始めたい」「worktreeを最新にして」「容量を減らして」「workspace を点検して」などの依頼で起動する。
---

# ws-task: タスク単位の worktree 管理

`ws` コマンド(このリポジトリの MoonBit 製ネイティブバイナリ。`~/.moon/bin/ws` にインストールして使う)で、タスク(複数の PR / PBI をまとめる大きな単位)ごとに worktree を管理する。

## インストール

前提条件とインストール方法の詳細はリポジトリの README.md を参照。

```
moon install ./cmd/ws                                                      # clone 済みのリポジトリで
moon install https://github.com/ayame113/my-workspace-manager.git cmd/ws   # clone せずに
```

- `~/.moon/bin/ws` に入るので、`~/.moon/bin` を PATH に入れる。更新は同じコマンドを再実行する。
- `ws` が見つからない場合、エージェントは自分でインストールせず、上記の手順をユーザーに案内する。

## エージェントの行動規則(最重要)

この環境の git の ssh 鍵にはパスフレーズがあり、エージェントは ssh が必要な操作を実行できない(待ち状態でハングする)。

- fetch / clone / push / `ws pull` / `ws clone` は自分で実行しない。ユーザーに「別のターミナルで」実行してもらうよう、コピペできるコマンドを提示して依頼する。Claude Code の `!` プレフィックスはパスフレーズ入力ができないので案内しない。
  - 例: `ws pull --task <task>` / `ws clone git@github.com:<owner>/<repo>.git`
- `CLAUDECODE` 環境変数がある場合、`ws pull` と `ws clone` はエラーで即終了する(ハング防止)。
- ローカルの `origin/<branch>` は古い可能性がある。ブランチの内容に依存する作業の前に、GitHub MCP でリモートの最新コミットを取得し、`git -C ~/ws/repos/<repo>.git rev-parse origin/<branch>` と比較する。不一致ならユーザーに `ws pull --task <task>` の実行を依頼する。
- `ws add --create` の `--base`(start-point)は `origin/main` のように明示する。bare repo には通常ローカルの main ブランチが無い。

## ディレクトリ構成

```
~/ws/
├── repos/                      # bare repo。ここでは作業しない
│   └── <repo>.git
└── tasks/
    └── <task>/                 # claude / エディタはここで開く
        ├── _readonly/          # 参照専用 worktree
        │   └── <repo>--<branch-slug>/
        └── <repo>--<branch-slug>/   # 作業用 worktree(1 worktree = 1 ブランチ固定)
```

## 命名規則

| 対象 | 規則 | 例 |
| --- | --- | --- |
| task | `[a-z0-9-]+`。先頭末尾のハイフン不可、連続ハイフン不可 | `user-auth-refactor` |
| repo | リモートのリポジトリ名そのまま(大文字可) | `example-API-docs` |
| branch-slug | 小文字化 → `[a-z0-9-]` 以外を `-` に置換 → 連続 `-` を1つに圧縮 → 先頭末尾の `-` を除去 | `feature/ABC-123/add_user_api` → `feature-abc-123-add-user-api`、`release/v1.2` → `release-v1-2` |

## 各ルールの理由

- task ディレクトリは完了しても移動・リネームしない。Claude Code の履歴がパス単位で保存されるため。
- claude / エディタは task ルート(`~/ws/tasks/<task>`)で起動する。配下の worktree をまとめて扱え、履歴もタスク単位でまとまる。
- readonly は `origin/<branch>` を detached HEAD で checkout する。全タスクが同じ bare repo を共有し、git は同一ブランチを複数 worktree で checkout できないため。detached なら複数タスクから同じブランチを参照できる。
- readonly を `_readonly/` に1段下げる。エディタの git ツールが1階層しか見ないため、参照専用のものが変更管理の対象に混ざらないようにしている。
- `<repo>--<slug>` の区切りを `--` にする。slug に `--` は現れない(連続ハイフンは圧縮される)ので、名前から repo と slug を一意に分解できる。

## 使い方

```
ws new <task>                              # tasks/<task>/ を作成(既存なら何もせず成功)
ws clone <url> [<repo>]                    # 【ユーザーが実行】bare repo を repos/ に用意
ws add --repo <repo> --branch <branch>     # git switch <branch> 相当。作業用 worktree を作る
ws add --repo <repo> --branch <branch> --create --base <start-point>   # git switch -c 相当(-c は --create の短縮形)。新規ブランチ(upstream なし)
ws add --repo <repo> --branch <branch> --readonly   # origin/<branch> を detached で _readonly/ に作る
ws pull                                    # 【ユーザーが実行】fetch して task 内の worktree を追従させる
ws ls [--all]                             # task の worktree 一覧 / --all は全 task
ws compact [--dry-run]                     # 全 task の node_modules 内の同一ファイルを clonefile で共有し、容量を減らす
ws lint [--all] [--fix]                   # 規約違反・壊れた worktree を検査する
```

- `add` / `pull` / `ls` / `lint` は `--task <task>` で task を指定する。省略するとカレントディレクトリが属する task を使う(`--task .` も同じ)。task の外では `--task`(`ls` / `lint` は `--all` でも可)の指定が必要。`add` は `--repo` と `--branch` が必須で、位置引数は受け付けない。オプションの順序は自由。
- 以下のコマンド例の `--task` は省略している(task ルートで実行する前提)。task 外から実行するなら `--task <task>` を付ける。
- `ws add` はネットワークを使わない。fetch も自動 clone もしない。bare repo が無ければ、実行すべき `ws clone` コマンドがエラーに表示される。
- `ws add --repo <repo> --branch <branch>` のブランチ解決:
  1. ローカルブランチがあればそれで worktree を作る。
  2. なければローカルの `origin/<branch>` から、同名ブランチを upstream 付きで作る。
  3. どちらも無ければ失敗する。リモートにあるはずなら、ユーザーに `ws pull` を依頼する。
- `--create` には `--base <start-point>` が必須で、`--base` は git にそのまま渡される(`origin/` は補完されない)。`--create` なしの `--base` は使えない。ローカルブランチが既にあれば失敗する。`--readonly` と `--create` / `--base` は併用できない。
- `ws pull`: task 内の worktree が使う repo ごとに1回だけ `git fetch origin --prune` し、続けて各 worktree を更新する。
  - readonly: 最新の `origin/<branch>` に detached で付け直す。
  - 作業用: upstream があれば `git merge --ff-only`。マージコミットや rebase は作らない。
  - 未コミット変更あり、upstream なし、upstream と分岐、はスキップして理由を表示する。一致するブランチが0件 / 複数件の readonly と、その他の失敗は error として表示され、終了コードは 1 になる。

## node_modules と容量(ws compact)

- node_modules は普段どおり `npm ci` / `npm i` で用意する(ws は関与しない)。npm は https なのでエージェントが実行してよい。
- 容量が気になったら `ws compact` を実行する。`$WS_ROOT/tasks` 配下の全 worktree の node_modules にある、内容(SHA-256)と mode が同じファイルを、APFS の clonefile(copy-on-write)で同じブロックを共有するファイルに置き換える。`--dry-run` で対象件数と削減見込みだけ確認できる。
- ネットワークを使わないのでエージェントが実行してよい。実行中の dev server 等が開いているファイルは旧 inode のまま残るので、影響はほぼ無い。
- 2 回目以降は `$WS_ROOT/.compact-index` の記録を使い、追加・変更されたファイルだけを処理する。
- `du` はクローンを区別しないので、削減量は表示される空き容量の差で確認する。

## lint

`ws lint`(`--all` で全 task、省略時は現在の task)は報告のみ、`--fix` で修正する。検出項目は、ディレクトリ名と実ブランチの不一致、upstream が別名ブランチ、readonly の破綻、壊れた worktree、命名規則違反。

- エージェントは `--fix` を勝手に実行しない。まず `ws lint` で報告し、内容をユーザーに伝えて確認を取る。

## 注意点

- 同一ブランチを checkout できる作業用 worktree は、全タスクを通じて1つだけ。二重に `ws add` すると git のエラーで失敗する。別タスクで参照したいだけなら `--readonly` を使う。
- readonly で作業を始めたくなったら、readonly には手を加えず `ws add` で作業用 worktree を作る。
- fork 由来のブランチ(`origin/<branch>` が無いもの)は `--readonly` できない。
- bare repo には `branch.autoSetupMerge=simple` を自動設定する(`ws add` / `ws pull` のたびに適用)。手で `git switch -c x origin/main` と切ったブランチの upstream が base になり、エディタの push で base に push されるのを防ぐため。この場合 upstream は付かず、`git push` はエラーで止まる。push はユーザーに依頼し、新規ブランチなら `git push -u origin <branch>` を案内する。
- task 名を決めるときは命名規則に合う名前をユーザーに確認してから `ws new` する。
