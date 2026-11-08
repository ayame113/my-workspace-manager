# my-workspace-manager

git worktree をタスク単位で管理する個人用 CLI `ws` と、それを Claude Code や Codex から使うためのスキル(`skills/ws-task`)。

`~/ws/repos/<repo>.git` に bare repo を置き、タスクごとのディレクトリ `~/ws/tasks/<task>/` に作業用 worktree(`<repo>--<branch-slug>`)と参照用の readonly worktree(`_readonly/`)を並べる。node_modules は普段どおり `npm ci` / `npm i` で用意し、容量が気になったら `ws compact` で worktree 間の同一ファイルを APFS の clonefile で共有して減らす。規約や運用の詳細は [skills/ws-task/SKILL.md](skills/ws-task/SKILL.md) を参照。

## 前提条件

- macOS 専用(`ws compact` が APFS の `clonefile(2)` を使うため)
- git
- Go 1.27 以上(ビルド時のみ)

## インストール

```sh
go install github.com/ayame113/my-workspace-manager/cmd/ws@latest
```

`$(go env GOPATH)/bin`(既定は `~/go/bin`)に入るので、PATH に入れる。更新するときは同じコマンドを再実行する。

### スキル

`ws` に同梱したスキルを、ユーザー単位のスキルディレクトリに書き出す。ws を更新したら同じコマンドを再実行して上書きする。

```sh
ws skill install claude   # ~/.claude/skills/ws-task
ws skill install codex    # ~/.agents/skills/ws-task
ws skill install          # 対話で選ぶ
```

Claude Code では、代わりにプラグインとして入れることもできる(両方入れるとスキルが重複するので、どちらか一方にする)。

```
/plugin marketplace add ayame113/my-workspace-manager
/plugin install ws@my-workspace-manager
```

## 使い方

トップレベルは workspace 全体、`ws worktree` は task 内が対象。

| コマンド | 内容 |
| --- | --- |
| `ws new [<task>]` | `tasks/<task>/` を作る。引数なしで対話可能なら、repo と task 名を尋ねて worktree もまとめて作る |
| `ws clone <url> [<repo>]` | bare repo を `repos/` に用意する |
| `ws ls` | 全 task の一覧(1 task 1 行。worktree 数と dirty 数) |
| `ws lint` | workspace 全体の規約違反・壊れた worktree を検査する |
| `ws compact [--dry-run]` | 全 task の node_modules 内の同一ファイルを clonefile で共有し、容量を減らす |
| `ws worktree add [--repo <repo>] [--branch <branch>] [-c --base <start-point> \| --readonly]` | worktree を作る。足りない引数は対話で尋ねる |
| `ws worktree rename [--repo <repo>] [--branch <old>] [--to <new>]` | 作業用 worktree のブランチ名を変える。slug が変わればディレクトリも移動する。足りない引数は対話で尋ねる |
| `ws worktree ls` | task 内の worktree の一覧 |
| `ws worktree lint` | task 内の規約違反・壊れた worktree を検査する |
| `ws worktree pull` | fetch して task 内の worktree を fast-forward で追従させる |
| `ws skill install [claude\|codex]...` | ws-task スキルをユーザー単位でインストール・更新する。引数なしなら対話で選ぶ |

`ws worktree` 配下のコマンドは `--task <task>` を省略するとカレントディレクトリが属する task を使う。task の外で、対話可能なら一覧から選ばせる(`ls` / `lint` / `pull` も同じ)。詳しくは `ws --help` / `ws worktree --help` / `ws worktree <command> --help`。

`ws lint` は全 task の検査に加え、git に登録されているがディレクトリが無いもののうち、どの task にも属さないものも報告する。`ws worktree lint` は指定した task の分だけを報告する。lint は報告だけで修正はしない。直せるものには、実行すべき git コマンドを `fix:` に表示する。

終了コードは 0 が成功、1 が実行時の失敗(lint で問題が見つかった場合を含む)、2 が使い方の誤り、130 が対話入力の中断(huh 上の Ctrl-C)。それ以外の場面の Ctrl-C は、シグナルで素直に終了する。

`ws clone` が失敗したときは、作りかけの `repos/<repo>.git` を削除する。

### 対話入力

`ws new`(引数なし)、`ws worktree add`、`ws worktree rename`、`ws skill install`(引数なし)は、必須の引数が足りないときに [huh](https://github.com/charmbracelet/huh) で対話的に尋ねる。部分的に指定した引数はそのまま使い、足りないものだけを尋ねる。

- 対話は stdin と stderr の両方が端末で、かつ `CLAUDECODE` が未設定のときだけ出る。それ以外(パイプ、スクリプト、エージェント)では、足りない引数は使い方の誤り(終了コード 2)になる。
- `add`: task(カレントディレクトリから決まらなければ一覧から)→ repo(`repos/*.git`)→ 種類(既存ブランチ / 新規ブランチ / readonly)→ ブランチ。既存ブランチはローカルと origin のブランチから絞り込みで選ぶ。新規ブランチは名前(`git check-ref-format` で検証)と起点(origin のブランチ。既定は `origin/HEAD`、無ければ `origin/main`)を尋ねる。
- `rename`: task 内の作業用 worktree から選び、新しいブランチ名を入力する(初期値は現在の名前)。
- `add` の新規ブランチ名の初期値は、ローカルにも origin にも無いランダムな名前(`brave-otter` のような `<形容詞>-<名詞>`)。Enter だけで採用できる。`rename` の初期値は現在の名前のまま。
- `ws worktree ls` / `lint` / `pull`: `--task` を省略して task の外にいるときは、task 一覧から絞り込みで選ぶ。task が 1 つも無ければ、選ばせずにエラーにする。`pull` のエージェント拒否は task の解決より先に行う。
- `ws new`(引数なし): repo を複数選ぶ(0 個でもよい。repo が無ければこの手順は出ない)→ task 名を入力する。task 名の初期値は `<repo 名>-<形容詞>-<名詞>`。repo 名は小文字化して `[a-z0-9]` 以外の連続をハイフン 1 つにし、複数選んだときは共通する先頭の語を 1 回だけ書いて残りを続ける(`app-web` と `app-api` なら `app-web-api-<形容詞>-<名詞>`)。repo を選ばなければ `<形容詞>-<名詞>`。既存の task と重ならない名前になる。
  - 選んだ各 repo に、新規ブランチで作業用 worktree を作る(`worktree add --create` 相当)。ブランチ名は尋ねず、全 repo 共通のランダムな名前(どの repo のローカルにも origin にも無いもの)。起点は `origin/HEAD`、無ければ `origin/main`(どちらも無い repo があれば、何も作らずエラー)。
  - 途中の worktree 作成に失敗したら、作成済みのものは残し、どこまでできたかを表示して終了コード 1 にする。
  - `ws new <task>` のように引数を渡したときは、従来どおり task を作るだけで対話しない。対話できない環境で引数が無ければ終了コード 2。
- 決まった内容は、引数を全部渡した場合と同じ処理で実行される。

### ws worktree rename

作業用 worktree のブランチをローカルでリネームする。ネットワークは使わないので、エージェントからも実行できる。

- 事前に、worktree が `<old>` を checkout していること、`<new>` が妥当な名前でローカルに無いこと、slug が変わるなら移動先が空いていることを確認する。slug が変わる場合、ws を実行しているカレントディレクトリが対象 worktree 配下だと拒否する。
- `git branch -m` の後、slug が変わるときだけ `git worktree move` する。移動に失敗したらブランチ名を戻す(戻せなかった場合は、手動で戻すコマンドを表示する)。
- `<old>` に upstream があった場合は upstream を外し、リモートへの反映手順(`git push -u origin <new>` と旧ブランチの削除)を表示する。upstream を外せなかった場合も rename は成功として扱い、警告を出す。リモートは変更しない。`_readonly/` の worktree は対象外。

### ws compact

`$WS_ROOT/tasks` 配下の全 worktree の node_modules(入れ子を含む)にある通常ファイルのうち、サイズ・SHA-256・mode が同じものを、代表ファイルの clonefile(copy-on-write)に原子的に置き換える。symlink と空ファイルは対象外。

- 処理したファイルの inode・ctime・size・ハッシュと共有済みかどうかを `$WS_ROOT/.compact-index` に記録する。2 回目以降は記録と一致する(変更されていない)ファイルのハッシュ計算と置き換えを省き、追加・変更されたファイルだけを処理する(npm は展開時に mtime を固定値にするため ctime を使う)。インデックスを消すと全件処理に戻る。
- 表示する削減見込みは置き換えたファイルのサイズの合計。`du` はクローンを区別しないので、実際の削減量は `df` の実行前後の差で見る。
- 実行中の dev server 等が開いているファイルは旧 inode のまま残るので、影響はほぼ無い。
- 中断で残った `.ws-compact-*` の一時ファイルは、次回の走査で削除する。

### 環境変数

| 変数 | 内容 |
| --- | --- |
| `WS_ROOT` | ワークスペースのルート。既定は `~/ws` |

## 開発

```sh
go test ./...   # 単体テストと結合テスト(testscript。一時ディレクトリにローカル file:// リモートを作って ws を実行する)
go vet ./...
gofmt -l .
```

構成はフロント / ロジック / 外部呼び出しに分けている。依存の向きは `cli → prompt, app`、`prompt → app`、`app → layout, gitexec, fsx`。

| パッケージ | 役割 |
| --- | --- |
| `cmd/ws` | `main`。終了コードで `os.Exit` する |
| `internal/cli` | フロント。cobra のコマンドツリー、フラグから `app` の Options への変換、エラーから終了コードへの変換、結果の整形出力 |
| `internal/prompt` | フロント(TUI)。huh による対話入力。`cli` から interface 越しに呼ばれる |
| `internal/app` | ロジック。各コマンドのユースケース(UI を知らない) |
| `internal/layout` | `WS_ROOT` 配下のパスと命名規則(slug、task 名の検証など) |
| `internal/gitexec` | 外部呼び出し。git の実行(`Runner` interface) |
| `internal/fsx` | OS 呼び出し。clonefile、lstat(`golang.org/x/sys/unix`) |
