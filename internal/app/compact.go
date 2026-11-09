// compact.go は `ws compact` の本体。全 task の worktree にある node_modules を走査し、中身が同じファイルを
// APFS の clonefile(2) による複製(copy-on-write)に置き換えて、ディスク容量を減らす。
// 流れは「走査(scan) → サイズでグループ化 → 重複しそうなものだけ SHA-256 を計算 → 置き換え計画(plan) → 置き換え」。
// 処理結果の記録(インデックス)の読み書きは compact_index.go が担当する。
//
// Go では 1 ディレクトリ = 1 パッケージで、同じディレクトリのファイルは `package app` を宣言して名前を共有する。
// このファイルの関数も、他のファイルで定義された App 型や layout などをそのまま使える(import 不要)。
// TypeScript の「同じ namespace に属するファイル群」に近い。
package app

// import の括弧の中に、標準ライブラリ、空行、自作・外部パッケージの順で並べるのが Go の慣習。
// 使わない import があるとコンパイルエラーになる。
import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ayame113/my-workspace-manager/internal/fsx"
)

// CompactEventKind は進捗通知の種類。Go には enum が無いので、「int を元にした型 + const の連番」で代用する。
// TypeScript の `enum` や、リテラル型のユニオンに近い。
type CompactEventKind int

const (
	// iota は const ブロック内で 0, 1, 2... と 1 ずつ増える連番。2 行目以降は型と式が前行の繰り返しになる。
	// ここでは CompactScanProgress=0, CompactScanned=1, CompactHashProgress=2, CompactHashed=3。
	// 走査中(10000 件ごと)、走査完了、ハッシュ計算中(1000 件ごと)、ハッシュ計算完了の各通知を表す。
	CompactScanProgress CompactEventKind = iota
	CompactScanned
	CompactHashProgress
	CompactHashed
)

// CompactEvent は進捗を呼び出し元(CLI の表示側)に知らせるイベント。
// struct は TypeScript の interface / オブジェクト型に近いが、フィールドはすべて必須で、
// 代入しなかったフィールドは「ゼロ値」(数値は 0、文字列は ""、ポインタや map や slice は nil)になる。
// フィールド名が大文字で始まるものは他のパッケージから見える(公開)。小文字なら同じパッケージ内だけ。
// Count は処理済みの件数、Total は全体の件数(ハッシュ計算中のみ使う)。
type CompactEvent struct {
	Kind  CompactEventKind
	Count int
	Total int
}

// CompactResult は Compact の実行結果の集計。
// DryRun は実際には何も変更していないことを示す。
// Targets は置き換え対象になったファイル数、SavingBytes はそれらのサイズ合計(削減見込み)。
// Replaced / ReplacedBytes は実際に置き換えたファイル数とサイズ合計。
// Changed は走査後に変更されていたため触らなかった数、NotCloned は clonefile できなかった数。
type CompactResult struct {
	DryRun        bool
	Targets       int
	SavingBytes   int64
	Replaced      int
	ReplacedBytes int64
	Changed       int
	NotCloned     int
}

// 置き換え用の一時ファイル名の接頭辞。処理が中断すると一時ファイルが残るので、
// 次回の走査(scan)で、この接頭辞を持つファイルを見つけて削除する。
const compactTempPrefix = ".ws-compact-"

// compactFile は走査で見つけた 1 ファイルの情報。
// stat は走査時点の inode や ctime など、recorded は前回のインデックスに記録されていた内容(無ければ nil)、
// hash は SHA-256 の 16 進文字列(まだ計算していない、またはサイズが一意で不要なら空文字)。
// `*indexEntry` の `*` はポインタ(値そのものではなく、その場所への参照)で、nil を「無い」の意味に使える。
// TypeScript の `indexEntry | null` に近い。
type compactFile struct {
	path     string
	stat     fsx.Stat
	recorded *indexEntry
	hash     string
}

// entry はこのファイルの現在の状態を、インデックスに保存する形(indexEntry)に変換する。
// shared は「代表のクローンに置き換え済みか」。
//
// `func (f *compactFile) entry(...)` はメソッドの書き方。func とメソッド名の間の `(f *compactFile)` が「レシーバ」で、
// TypeScript のクラスメソッドの `this` に相当する(名前は自分で決める。慣習で型名の頭文字 1 字にする)。
// `*` が付くポインタレシーバは、呼び出し元の構造体そのものを参照する(値のコピーではない)。
// フィールドを書き換えたいとき、または構造体が大きいときに使う。
func (f *compactFile) entry(shared bool) indexEntry {
	return indexEntry{inode: f.stat.Ino, ctimeNs: f.stat.CtimeNs, size: f.stat.Size, hash: f.hash, shared: shared}
}

// inodeKey は「ディスク上の同一ファイル」を識別するキー。dev はデバイス(ボリューム)番号、ino は inode 番号。
// ハードリンクされた複数のパス名は同じ (dev, ino) を持つので、重複して数えないために使う。
// struct は比較可能なら map のキーにできる(TypeScript だと文字列に結合してキーにするところ)。
type inodeKey struct {
	dev int64
	ino uint64
}

// scanner は走査中の状態を持つ。seen は見つけた inode の集合、files は集めたファイル、
// notify は進捗通知のコールバック(関数を値として持てる。TypeScript の `(e: CompactEvent) => void` 型のプロパティ)。
// `map[inodeKey]bool` は TypeScript の `Map<inodeKey, boolean>` に近く、集合(Set)の代わりによく使う。
type scanner struct {
	seen   map[inodeKey]bool
	files  []*compactFile
	notify func(CompactEvent)
}

// scan collects non-empty regular files under node_modules (nested ones included). Symlinks are not followed and hard links count once.
// Temporary files left by an interrupted replaceWithClone are deleted when found.
// 引数は directory(走査するディレクトリ)と insideNodeModules(すでに node_modules の中にいるか)。
// 戻り値の error は、成功なら nil、失敗ならエラー値。Go には try/catch が無く、
// 例外の代わりに「関数がエラーを戻り値で返し、呼び出し側が毎回確認する」のが基本。
//
// この関数は再帰で、worktree のディレクトリから順に降りていき、node_modules の中にある通常ファイルだけを集める。
// node_modules の外のファイル(ソースコードなど)は対象外。
func (s *scanner) scan(directory string, insideNodeModules bool) error {
	// `:=` は「宣言と代入を同時に行う」短縮形で、型は右辺から推論される(TypeScript の const / let に近い)。
	// 関数が複数の値を返せるのも Go の特徴で、ここでは「エントリ一覧」と「エラー」の 2 つを受け取っている。
	entries, err := os.ReadDir(directory)
	// Go のエラー処理の定型句。エラーが起きていたら(err が nil でなければ)、すぐ呼び出し元に返す。
	// この形がコード中に何度も出てくる。
	if err != nil {
		return err
	}
	// range でスライス(可変長配列)を走査する。1 つ目の値はインデックス、2 つ目が要素。
	// インデックスは使わないので `_`(捨てる)と書く。使わない変数があるとコンパイルエラーになるため。
	// TypeScript の `for (const entry of entries)` に近い。
	for _, entry := range entries {
		// filepath.Join はパス区切りを正しく補いながら連結する(Node の path.join に相当)。
		path := filepath.Join(directory, entry.Name())
		if entry.IsDir() {
			// .git ディレクトリの中は node_modules を含まないので、走査しない。
			if entry.Name() == ".git" {
				continue
			}
			// `if 文の前置き; 条件 {` の形で、err をこの if の中だけで有効な変数として宣言している。
			// ディレクトリなら再帰して中へ降りる。名前が node_modules のディレクトリに入った時点から、
			// その配下(入れ子の node_modules を含む)はすべて対象になる。
			if err := s.scan(path, insideNodeModules || entry.Name() == "node_modules"); err != nil {
				return err
			}
			continue
		}
		// node_modules の外にある通常ファイルは対象外。
		if !insideNodeModules {
			continue
		}
		// 前回の置き換えが中断されて残った一時ファイルは、ここで消す。
		// `os.Remove` の戻り値(エラー)は無視している。消せなくても致命的ではないため。
		if strings.HasPrefix(entry.Name(), compactTempPrefix) {
			os.Remove(path)
			continue
		}
		// Lstat は symlink を辿らずにファイル自体の情報(inode、ctime、サイズ、種別、mode)を得る。
		stat, err := fsx.Lstat(path)
		// 同じ inode を指すハードリンクは 1 つだけ数える。ハードリンクどうしはもともと同じ実体を共有しているので、
		// 別々のファイルとして数えて重複扱いにしないため。
		key := inodeKey{stat.Dev, stat.Ino}
		// 次のものは対象外にして次のファイルへ進む(continue)。
		// Lstat に失敗したもの、通常ファイルでないもの(symlink など)、空ファイル、すでに見た inode。
		// Lstat が失敗したときの stat はゼロ値だが、`||` は左から評価して真になった時点で止まるので、err を最初に判定すれば安全。
		if err != nil || !stat.IsRegular || stat.Size == 0 || s.seen[key] {
			continue
		}
		s.seen[key] = true
		// append はスライスに要素を足し、「足した後のスライス」を返すので、自分自身に代入し直す。
		// `&compactFile{...}` は構造体を作って、そのポインタを得る書き方。
		s.files = append(s.files, &compactFile{path: path, stat: stat})
		// 10000 件ごとに進捗を通知する(`%` は剰余)。
		if len(s.files)%10000 == 0 {
			s.notify(CompactEvent{Kind: CompactScanProgress, Count: len(s.files)})
		}
	}
	return nil
}

// scanTasks は全 task の全 worktree(作業用と readonly の両方)を走査して、対象ファイルの一覧を返す。
// tasks/ が無ければ、空の一覧を返す(nil は空のスライスとして扱える)。
//
// `func (a *App) ...` の App は、アプリ全体の依存(layout、git の実行など)を持つ構造体。
// ctx は context.Context で、キャンセルやタイムアウトを関数呼び出しの連鎖に伝えるための値。
// 慣習として第 1 引数に置く。この関数では受け取るだけで使っていない。
func (a *App) scanTasks(ctx context.Context, notify func(CompactEvent)) ([]*compactFile, error) {
	// `map[K]V{}` は空の map を作る(make でも作れる)。nil の map には書き込めないので、必ず初期化しておく。
	s := &scanner{seen: map[inodeKey]bool{}, notify: notify}
	if !fsx.Exists(a.layout.TasksRoot()) {
		return nil, nil
	}
	// task 名(tasks/ 直下のディレクトリ名)の一覧。
	tasks, err := fsx.SubdirNames(a.layout.TasksRoot())
	if err != nil {
		return nil, err
	}
	for _, task := range tasks {
		// 各 task について、作業用 worktree が並ぶ task ディレクトリと、参照用 worktree が並ぶ _readonly/ の両方を調べる。
		// `[]string{...}` は文字列スライスのリテラル。
		for _, dir := range []string{a.layout.TaskDir(task), a.layout.ReadonlyDir(task)} {
			// dir 直下にある worktree ディレクトリの名前一覧(`<repo>--<slug>` 形式のもの)。
			names, err := listWorktreeNames(dir)
			if err != nil {
				return nil, err
			}
			for _, name := range names {
				// worktree の中身を走査する。入口では node_modules の外にいるので false を渡す。
				if err := s.scan(dir+"/"+name, false); err != nil {
					return nil, err
				}
			}
		}
	}
	return s.files, nil
}

// unchanged reports whether path still matches its scan (ctime moves on any content or mode change).
// 走査時点の状態(scanned)と今の状態を比べ、変わっていなければ true を返す。
// 比べるのは inode、サイズ、ctime。ctime は内容やmode(権限)が変わると更新される。
// 走査してから置き換えまでの間に、npm install などでファイルが変わっていないかの確認に使う。
func unchanged(path string, scanned fsx.Stat) bool {
	now, err := fsx.Lstat(path)
	return err == nil && now.Ino == scanned.Ino && now.Size == scanned.Size && now.CtimeNs == scanned.CtimeNs
}

// replaceWithClone replaces target by a clone of representative; false means cloning is not possible.
// Within one directory the rename is atomic, so target always exists with either the old or the new content.
// target を representative の clonefile で置き換える。引数は「置き換えられるファイル」と「代表ファイル」。
// 戻り値は「置き換えたか」と「エラー」の 2 つ。(false, nil) は、エラーではないが clonefile できなかったことを表す
// (別のボリュームにある、APFS でないなど)。
//
// 背景: clonefile(2) は APFS の機能で、ファイルの内容(ディスク上のブロック)を共有する複製を作る。
// ハードリンクは「同じ inode に名前が複数付く」ので、片方への書き込みが他方にも見えてしまう。
// clonefile は inode が別で、書き込んだ時点で書いた部分だけが別ブロックに分かれる(copy-on-write)。
// そのため、node_modules のファイルを書き換えるツール(パッチ、ビルド)があっても他の worktree を壊さない。
//
// なぜ一時ファイル経由か: clonefile は既存のファイルの上には作れない(作成先は存在してはいけない)。
// かといって、先に target を消してから作ると、その間や失敗時に target が存在しない状態が生まれる。
// そこで同じディレクトリに一時ファイルとして複製を作り、rename で target の上に重ねる。
// 同じディレクトリ内の rename は原子的(途中の状態が見えない)なので、target は常に古い内容か新しい内容のどちらかで存在する。
// 一時ファイルを別のディレクトリ(別ボリュームかもしれない)に作ると rename できないので、必ず同じディレクトリに作る。
func replaceWithClone(target, representative string) (bool, error) {
	// 一時ファイルのパス。target と同じディレクトリに、接頭辞を付けた名前で作る。
	temporary := filepath.Join(filepath.Dir(target), compactTempPrefix+filepath.Base(target))
	// 前回の中断で同名の一時ファイルが残っていたら消す。無かった(IsNotExist)場合はエラーとして扱わない。
	if err := os.Remove(temporary); err != nil && !os.IsNotExist(err) {
		return false, err
	}
	// clonefile(2) で複製を作る。失敗は「このファイルは置き換えできなかった」として、エラーにせず false を返す。
	// `return false, nil` のように、戻り値は複数をカンマで並べて返す。
	if err := fsx.Clonefile(representative, temporary); err != nil {
		return false, nil
	}
	// 一時ファイルを target の上に rename して、原子的に置き換える。失敗したら一時ファイルを片付けてエラーを返す。
	if err := os.Rename(temporary, target); err != nil {
		os.Remove(temporary)
		return false, err
	}
	return true, nil
}

// replacement は「target を representative のクローンに置き換える」という計画の 1 件。
type replacement struct {
	target         *compactFile
	representative *compactFile
}

// contentKey は「同じ内容」の判定キー。SHA-256 と mode(実行権限など)が両方同じものを同一とみなす。
// mode が違うファイルを同じにしてしまうと、実行権限が変わってしまうので、mode もキーに含める。
// (サイズは呼び出し前にグループ分けで揃えてある。)
type contentKey struct {
	hash string
	mode uint32
}

// plan picks one representative per group of identical files and lists the files to replace by its clone.
// plan は、同じサイズごとのグループ(groups)から、置き換えの計画を立てる。純粋な計算で、ファイルには触らない。
// 戻り値は 2 つ。1 つ目は「実行後のインデックスの内容(パス → 記録)」、2 つ目は「置き換えるファイルのリスト」。
//
// `[][]*compactFile` は「compactFile へのポインタのスライス」のスライス、つまり 2 次元配列。
func plan(groups [][]*compactFile) (map[string]indexEntry, []replacement) {
	// 次回のインデックスに保存する内容。パスをキーにする。
	next := map[string]indexEntry{}
	// `var 名前 型` は、初期値を与えずに宣言する書き方。スライスのゼロ値は nil で、そのまま append できる。
	var replacements []replacement
	for _, group := range groups {
		// keys は byKey に最初に登場した順にキーを並べたもの。Go の map は反復の順序が不定なので、
		// 結果を毎回同じにするために、順序を別に持っている。
		var keys []contentKey
		// 同じ内容(hash と mode)のファイルをまとめる map。
		byKey := map[contentKey][]*compactFile{}
		for _, file := range group {
			// hash が空なのは、同じサイズのファイルが他に無くてハッシュを計算しなかったもの。
			// 重複する相手がいないので、置き換えず、そのまま記録だけする。
			if file.hash == "" {
				next[file.path] = file.entry(false)
				continue
			}
			key := contentKey{file.hash, file.stat.Mode}
			// `v, ok := m[k]` は map の存在確認の定型。ok が false ならキーが無い(値は不要なので `_` で捨てる)。
			// TypeScript の `map.has(key)` に近い。初めて見たキーだけを keys に追加する。
			if _, ok := byKey[key]; !ok {
				keys = append(keys, key)
			}
			byKey[key] = append(byKey[key], file)
		}
		for _, key := range keys {
			members := byKey[key]
			// 前回の代表が変わらず残っていればそれを使い続け、そのクローン済みのものは置き換えない
			// 条件に合う最初の要素の位置を返す(無ければ -1)。`func(f *compactFile) bool { ... }` は無名関数(クロージャ)で、
			// TypeScript のアロー関数 `(f) => ...` に近い。
			previous := slices.IndexFunc(members, func(f *compactFile) bool {
				return f.recorded != nil && !f.recorded.shared
			})
			// previous が -1(前回の代表が見つからない)なら先頭の要素を代表にする。
			// max は Go 1.21 で入った組み込み関数。
			representative := members[max(previous, 0)]
			next[representative.path] = representative.entry(false)
			for _, file := range members {
				// ポインタどうしの比較なので、「同じオブジェクトか」を調べている。
				if file == representative {
					continue
				}
				// 前回の代表が残っていて、このファイルがすでにそのクローンになっているなら、もう一度置き換える必要は無い。
				// そうでなければ置き換え対象に加える。
				if previous >= 0 && file.recorded != nil && file.recorded.shared {
					next[file.path] = file.entry(true)
				} else {
					replacements = append(replacements, replacement{file, representative})
				}
			}
		}
	}
	return next, replacements
}

// Compact replaces identical files (same size, SHA-256 and mode) in the node_modules of every worktree by clones of one representative.
// With dryRun nothing is modified.
// Compact は `ws compact` の本体で、CLI から呼ばれる。
// 引数: dryRun が true なら計画だけ立てて何も変更しない。notify は進捗を受け取るコールバック。
// 戻り値: 集計結果(CompactResult)とエラー。エラーでも、それまでの集計を result に入れて返す箇所がある。
//
// 処理の流れ: (1) 前回のインデックスを読む (2) 走査 (3) サイズごとにグループ化 (4) 必要なものだけハッシュ計算
// (5) 置き換え計画 (6) dry-run でなければ置き換え (7) インデックスを保存。
func (a *App) Compact(ctx context.Context, dryRun bool, notify func(CompactEvent)) (CompactResult, error) {
	// フィールド名を指定して構造体を作る。指定しなかったフィールドはゼロ値(数値は 0)になる。
	result := CompactResult{DryRun: dryRun}
	indexPath := a.layout.CompactIndexPath()
	// 前回の記録を読む。ファイルが無い、壊れているときは空の map が返り、全ファイルを処理し直す。
	indexed := loadCompactIndex(indexPath)
	files, err := a.scanTasks(ctx, notify)
	if err != nil {
		return result, err
	}
	notify(CompactEvent{Kind: CompactScanned, Count: len(files)})

	// 同じサイズのファイルは内容も同じかもしれないが、サイズが違えば必ず内容が違う。
	// そこでまずサイズでグループ分けして、ハッシュを計算する(読み込みが重い)ファイルを絞り込む。
	// sizes はサイズが最初に出てきた順(map の順序が不定なので、再現性のために別に持つ)。
	var sizes []int64
	bySize := map[int64][]*compactFile{}
	for _, file := range files {
		// 前回の記録があり、inode・ctime・サイズが一致するなら、内容は変わっていないとみなして、記録されたハッシュを使い回す。
		// これでハッシュ計算(ファイル全体の読み込み)を省ける。npm は展開時に mtime を固定にするので、判定には ctime を使う。
		if recorded, ok := indexed[file.path]; ok && recorded.matches(file.stat) {
			// `&recorded` は変数 recorded のポインタ。ループごとに新しい recorded が宣言されるので、別のファイルと混ざらない。
			file.recorded = &recorded
			file.hash = recorded.hash
		}
		if _, ok := bySize[file.stat.Size]; !ok {
			sizes = append(sizes, file.stat.Size)
		}
		bySize[file.stat.Size] = append(bySize[file.stat.Size], file)
	}
	// make でスライスを作る。第 2 引数が長さ、第 3 引数が容量(あらかじめ確保しておく要素数)。
	groups := make([][]*compactFile, 0, len(sizes))
	var toHash []*compactFile
	for _, size := range sizes {
		group := bySize[size]
		groups = append(groups, group)
		// 同じサイズのファイルが 1 つだけなら、重複しようがないのでハッシュ計算は不要。
		// 2 つ以上あり、まだハッシュが無いものだけを計算対象にする。
		if len(group) >= 2 {
			for _, file := range group {
				if file.hash == "" {
					toHash = append(toHash, file)
				}
			}
		}
	}
	// ハッシュ計算。ファイル全体を読むので、ここが一番時間がかかる。1000 件ごとに進捗を通知する。
	for i, file := range toHash {
		// `=`(`:=` ではない)は既存の変数への代入。file.hash と、外側で宣言済みの err に同時に代入している。
		if file.hash, err = fsx.SHA256Hex(file.path); err != nil {
			return result, err
		}
		if (i+1)%1000 == 0 {
			notify(CompactEvent{Kind: CompactHashProgress, Count: i + 1, Total: len(toHash)})
		}
	}
	notify(CompactEvent{Kind: CompactHashed, Count: len(toHash)})

	// 置き換えの計画を立てる。ここまでは読み取りだけで、ファイルは変更していない。
	next, replacements := plan(groups)
	result.Targets = len(replacements)
	for _, r := range replacements {
		result.SavingBytes += r.target.stat.Size
	}
	// dry-run はここで終了。インデックスも保存しない。
	if dryRun {
		return result, nil
	}

	// 計画に沿って 1 件ずつ置き換える。
	for _, r := range replacements {
		// 走査後に変更されたファイルは、内容が変わっている可能性があるので触らない
		if !unchanged(r.target.path, r.target.stat) || !unchanged(r.representative.path, r.representative.stat) {
			result.Changed++
			continue
		}
		// clonefile を使えなかった場合(cloned が false)は、エラーにせず件数だけ数えて続ける。
		cloned, err := replaceWithClone(r.target.path, r.representative.path)
		if err != nil {
			return result, err
		}
		if !cloned {
			result.NotCloned++
			continue
		}
		result.Replaced++
		result.ReplacedBytes += r.target.stat.Size
		// 置き換えで inode と ctime が変わるので、置き換え後の状態を「共有済み」として記録する
		if stat, err := fsx.Lstat(r.target.path); err == nil {
			updated := *r.target
			updated.stat = stat
			next[r.target.path] = updated.entry(true)
		}
	}
	// 最後にインデックスを保存する。保存の成否が、そのまま関数のエラーになる。
	return result, saveCompactIndex(indexPath, next)
}
