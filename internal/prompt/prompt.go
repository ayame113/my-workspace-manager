// Package prompt は、足りない引数を対話的に尋ねる画面(TUI)を担当する。huh というライブラリで選択・入力の画面を描く。
// 依存の向きは cli → prompt → app。cli からは cli.Prompter という interface 越しに呼ばれるので、テストでは偽物に差し替えられる。
// prompt 自身は git やファイルを直接触らず、候補の一覧や入力の検証は app に頼む。
//
// huh の基本の考え方は次のとおり。
//   - Form は画面全体、Group は 1 画面に同時に出すまとまり、Field は個々の入力欄(Select、MultiSelect、Input、Confirm)。
//   - 入力欄に Value(&変数) でポインタを渡すと、ユーザーが操作した結果がその変数に書き込まれる。
//     Go は値渡しなので、「結果を受け取る変数の場所(ポインタ)」を渡す必要がある。
//     TypeScript なら、結果を返す代わりに書き込み先のオブジェクトを渡すイメージ。
//   - 画面を動かす(Run)と、Enter で確定された内容が変数に入る。変数は Run が終わった後で読むこと。
//   - Ctrl-C や Esc で中断されると huh.ErrUserAborted が返る。
package prompt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"charm.land/huh/v2"

	"github.com/ayame113/my-workspace-manager/internal/app"
)

// Prompter は対話入力をまとめた型。app(候補の取得と検証用)と、画面の入出力の置き場所を持つ。
//
// フィールドが小文字なので、このパッケージの外からは触れない。New を通して作る。
// in と out は io.Reader / io.Writer という interface で、「読める何か」「書ける何か」を表す(TypeScript の ReadableStream / WritableStream に近い)。
// 本物では標準入力と標準エラー出力が入り、テストでは偽の入出力を渡せる。
// 画面を標準エラー出力に描くのは、標準出力を「選んだ結果だけ」にしてシェルで使えるようにするため(README の wcd を参照)。
type Prompter struct {
	app *app.App
	in  io.Reader
	out io.Writer
}

// New は Prompter を作る。a は候補の取得と検証に使い、in と out には画面の入出力(通常は stdin と stderr)を渡す。
//
// *app.App の * はポインタ。App は大きな構造体なので、コピーせず同じものを共有するためにポインタで持つ。
// &Prompter{...} は構造体を作って、そのポインタを返す書き方。「app: a」は フィールド名: 値 の形(オブジェクトリテラル { app: a } と同じ)。
func New(a *app.App, in io.Reader, out io.Writer) *Prompter {
	return &Prompter{app: a, in: in, out: out}
}

// selectHeight は選択リストの表示行数。候補が多くても画面を埋めないよう、一定の行数でスクロールさせる。
const selectHeight = 12

// ErrAborted is returned when the user cancels a prompt (Ctrl-C / Esc).
//
// huh のエラー(huh.ErrUserAborted)をこの値に置き換えて返すので、呼び出し側は huh を import せずに
// errors.Is(err, prompt.ErrAborted) で中断を判定できる。cli はこれを終了コード 130 に変換する。
// errors.New は固定のメッセージを持つエラー値を作る。パッケージ変数に 1 つ作って使い回し、比較に使う(定数のような使い方)。
var ErrAborted = errors.New("入力が中断されました")

// run は fields を 1 つの画面(Form の 1 つの Group)にまとめて表示し、ユーザーが全部確定するまで待つ。
//
// 呼び出し側は、各入力欄の Value に渡した変数を、この関数が nil を返した「後で」読む。
// 中断されたら ErrAborted を返し、それ以外の失敗(端末が使えないなど)はそのまま返す。
// 全ての入力画面がこの関数を通るので、中断の扱いが 1 箇所に集まる。
//
// 「fields ...huh.Field」は可変長引数で、複数の入力欄を並べて渡せる。huh.Field は各種入力欄が満たす interface。
// 本体の 1 行目は、メソッドチェーン(TypeScript のビルダーパターンと同じ書き方)で、
// フォームを作り、入力元と出力先を設定し、ctx つきで実行している。RunWithContext は ctx がキャンセルされると中断する。
func (p *Prompter) run(ctx context.Context, fields ...huh.Field) error {
	err := huh.NewForm(huh.NewGroup(fields...)).WithInput(p.in).WithOutput(p.out).RunWithContext(ctx)
	// errors.Is(err, X) は、err が X そのもの、または X を包んだエラー(%w で包んだもの)かどうかを調べる。
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrAborted
	}
	return err
}

// choose は options から 1 つを選ばせ、選ばれた文字列を返す。文字を打つと候補を絞り込める。
//
// options が空だと選べないので、画面を出さずにエラーを返す。title は画面の見出し。中断されたら ErrAborted を返す。
func (p *Prompter) choose(ctx context.Context, title string, options []string) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("%s: 選択できる候補がありません", title)
	}
	// 選択結果を受け取る変数。ゼロ値(空文字)で作り、下の Value(&chosen) で huh に書き込み先として渡す。
	// 画面を動かす p.run の後で読む(run の前はまだ何も入っていない)。
	var chosen string
	// huh.NewSelect[string]() は 1 つ選ぶ欄を作る。[string] はジェネリクスの型引数で、「選択肢の値の型は string」という意味。
	// TypeScript の Select<string>() と同じ。値が string 以外(下の chooseMode の addMode など)でも同じ部品を使える。
	// huh.NewOptions は値の一覧から、表示名と値が同じ選択肢を作る。後ろの ... は、スライスを可変長引数に展開する書き方。
	// Filtering(true) で文字入力による絞り込みが有効になる。Height は表示行数。
	field := huh.NewSelect[string]().
		Title(title).
		Options(huh.NewOptions(options...)...).
		Filtering(true).
		Height(selectHeight).
		Value(&chosen)
	if err := p.run(ctx, field); err != nil {
		return "", err
	}
	return chosen, nil
}

// input は 1 行のテキストを入力させ、前後の空白を除いた文字列を返す。
//
// initial は入力欄に最初から入っている値。validate が nil でなければ、Enter を押したときに検証し、
// エラーを返した場合はその文言を画面に出して確定させない(nil なら検証しない)。
// 「validate func(string) error」は、文字列を受け取って error を返す関数を引数に取る(TypeScript の (v: string) => Error | null)。
func (p *Prompter) input(ctx context.Context, title, initial string, validate func(string) error) (string, error) {
	// 入力欄の値は変数に直接書き込まれる。initial で初期化しておくと、それが入力欄の初期値になる。
	value := initial
	field := huh.NewInput().Title(title).Value(&value)
	if validate != nil {
		field = field.Validate(validate)
	}
	if err := p.run(ctx, field); err != nil {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

// branchName はブランチ名を入力させる。空や、git のブランチ名として不正な値は、その場でエラー表示して確定させない。
// 実際の検証は app.ValidateBranchName(git check-ref-format を使う)に任せ、ここでは空白だけの入力を先に弾く。
func (p *Prompter) branchName(ctx context.Context, title, initial string) (string, error) {
	// 末尾の func(name string) error { ... } は無名関数(クロージャ)で、input の validate 引数として渡している。
	// 外側の p と ctx を内側から参照している点がクロージャの特徴(TypeScript のアロー関数と同じ)。
	return p.input(ctx, title, initial, func(name string) error {
		if strings.TrimSpace(name) == "" {
			return errors.New("ブランチ名を入力してください")
		}
		return p.app.ValidateBranchName(ctx, strings.TrimSpace(name))
	})
}

// addMode は ws worktree add の対話で選ぶ「worktree の種類」。
//
// 「type addMode string」は、string を元にした新しい型を定義している。TypeScript の
// type AddMode = "既存ブランチ" | "新規ブランチ" | ... の文字列リテラル型に近い。
// 値の文字列がそのまま画面に表示される文言になる。
type addMode string

// const ( ... ) で、関連する定数をまとめて宣言する。型を addMode に揃えて、普通の文字列と取り違えないようにしている。
const (
	modeExisting addMode = "既存ブランチ"
	modeNew      addMode = "新規ブランチ"
	modeReadonly addMode = "readonly(main など、複数の task から参照するブランチ)"
)

// chooseMode は worktree の種類(既存ブランチ / 新規ブランチ / readonly)を選ばせる。
//   - 既存ブランチ: すでにあるブランチで作業用 worktree を作る
//   - 新規ブランチ: 起点(base)から新しいブランチを切って作業用 worktree を作る
//   - readonly: main など複数の task から参照するブランチを、detached HEAD で参照専用に置く
//
// huh.NewSelect[addMode]() は型引数が addMode で、選んだ値が addMode 型のまま返る。
// 候補が 3 つだけなので Filtering(絞り込み)は付けていない。
func (p *Prompter) chooseMode(ctx context.Context) (addMode, error) {
	var mode addMode
	// huh.NewOptions に値を直接並べると、その値が候補の表示名と戻り値の両方になる。
	field := huh.NewSelect[addMode]().
		Title("worktree の種類").
		Options(huh.NewOptions(modeExisting, modeNew, modeReadonly)...).
		Value(&mode)
	if err := p.run(ctx, field); err != nil {
		return "", err
	}
	return mode, nil
}

// ChooseTask lets the user pick from the existing tasks.
//
// 既存の task を一覧から選ばせ、選んだ task 名を返す。各行には task の説明も添える。
// task が 1 つも無いとエラーを返す(新規作成は ws new を案内する)。
// 先頭が大文字のメソッドは他のパッケージ(cli)から呼べる公開メソッド。小文字のものはこのパッケージ内専用。
func (p *Prompter) ChooseTask(ctx context.Context) (string, error) {
	tasks, err := p.app.TaskNames()
	if err != nil {
		return "", err
	}
	if len(tasks) == 0 {
		return "", errors.New("task がありません。先に 'ws new' を実行してください")
	}
	// make([]Choice, n) は長さ n のスライスを作る(TypeScript の new Array(n) に近い)。あとで choices[i] に代入して埋める。
	// for i, task := range tasks は添字と値の両方を取り出すループ(tasks.forEach((task, i) => ...) に相当)。
	choices := make([]Choice, len(tasks))
	for i, task := range tasks {
		label := task
		// 説明が読めない、または空のときは、task 名だけを表示する。説明の読み込みに失敗しても選択自体は続けたいので、エラーは無視する。
		if description, err := p.app.Description(task); err == nil && description != "" {
			label += "  " + description
		}
		choices[i] = Choice{Label: label, Value: task}
	}
	return p.PickTask(ctx, choices)
}

// Choice is one option whose label is shown and matched by the filter, and whose value is returned.
//
// 画面に出す文字列(Label)と、選ばれたときに返す値(Value)を分けた選択肢。
// 例: Label は "my-task  説明文"、Value は "my-task"。絞り込みは Label に対して行われるので、説明の語でも探せる。
type Choice struct {
	Label string
	Value string
}

// PickTask lets the user narrow the choices by typing any part of a label and returns the chosen value.
//
// choices から 1 つ選ばせ、選ばれた Choice の Value を返す。ChooseTask と、ws ls の絞り込み画面の両方から使われる。
// 中断されたら ErrAborted を返す。
func (p *Prompter) PickTask(ctx context.Context, choices []Choice) (string, error) {
	// huh.Option[string] は「表示名 + 値」の組(値の型が string)。huh.NewOption(表示名, 値) で作る。
	// 先ほどの NewOptions と違い、表示名と値を別々に持てる。
	options := make([]huh.Option[string], len(choices))
	for i, c := range choices {
		options[i] = huh.NewOption(c.Label, c.Value)
	}
	var chosen string
	field := huh.NewSelect[string]().
		Title("task(文字を打つと task 名や説明で絞り込める)").
		Options(options...).
		Filtering(true).
		Height(selectHeight).
		Value(&chosen)
	if err := p.run(ctx, field); err != nil {
		return "", err
	}
	return chosen, nil
}

// NewTaskAnswers is what the user chose for `ws new`.
//
// ws new の対話で決まった内容。app.NewTaskOptions(task 名、repo、説明)に、「作成後にエディタで開くか」を足したもの。
// フィールド名を付けずに型だけを書く(下の app.NewTaskOptions)と埋め込み(embedding)になり、
// NewTaskOptions のフィールドが answers.Task のように直接読める。継承に似ているが、実体は「フィールドとして持っているだけ」。
type NewTaskAnswers struct {
	app.NewTaskOptions
	Open bool
}

// New asks for the repos to create worktrees in (none is allowed), then for a task name, a description
// and whether to open the task in the editor.
//
// 手順は、(1) repo を複数選ぶ(repo が 1 つも無ければ飛ばす)、(2) 選んだ repo から task 名の初期値を作る、
// (3) task 名・説明・「エディタで開くか」を 1 画面で入力、の順。途中で失敗または中断したら、そこで返る。
func (p *Prompter) New(ctx context.Context) (NewTaskAnswers, error) {
	repos, err := p.app.RepoNames()
	if err != nil {
		return NewTaskAnswers{}, err
	}
	var chosen []string
	if len(repos) > 0 {
		// 「=」と「:=」の違い: := は新しい変数の宣言と代入、= は既にある変数への代入。
		// ここは上で宣言済みの chosen と err に代入したいので = を使う(:= だと if の中だけの別の変数になってしまう)。
		chosen, err = p.chooseMany(ctx, "worktree を作る repo(Space で選択、何も選ばなければ task だけ作る)", repos)
		if err != nil {
			return NewTaskAnswers{}, err
		}
	}
	initial, err := p.app.RandomTaskName(chosen)
	if err != nil {
		return NewTaskAnswers{}, err
	}
	// 3 つの変数を 1 行でまとめて宣言する。入力欄の初期値になる(task 名は提案、説明は空、「開く」は既定でオン)。
	task, description, open := initial, "", true
	// 複数の入力欄を 1 つの画面にまとめて表示する。上から順に Enter で進み、全部確定すると run が戻ってくる。
	// Input は 1 行入力、Confirm は「はい / いいえ」(Affirmative と Negative でボタンの文言を変えている)。
	// Validate は task 名が重複・不正でないかを Enter のたびに検証し、エラーなら確定させない。
	// Value(&変数) の変数は、run が成功した後(下の if err の次の行以降)で読む。
	err = p.run(ctx,
		huh.NewInput().Title("task 名").Value(&task).Validate(func(name string) error {
			return p.app.ValidateNewTaskName(strings.TrimSpace(name))
		}),
		huh.NewInput().Title("説明(1 行、空でもよい)").Value(&description),
		huh.NewConfirm().Title("作成後に zed で開く").Affirmative("開く").Negative("開かない").Value(&open),
	)
	if err != nil {
		return NewTaskAnswers{}, err
	}
	// 画面で確定した値を結果の構造体にまとめて返す。task 名は前後の空白を除く。
	return NewTaskAnswers{
		NewTaskOptions: app.NewTaskOptions{Task: strings.TrimSpace(task), Repos: chosen, Description: description},
		Open:           open,
	}, nil
}

// Describe asks for a task's description, starting from the current one.
//
// task の説明(1 行)を入力させる。current が最初から入力欄に入っていて、空にして確定すると説明を消す扱いになる。
// fmt.Sprintf は printf 形式で文字列を作る(TypeScript のテンプレートリテラルに近い)。
func (p *Prompter) Describe(ctx context.Context, task, current string) (string, error) {
	return p.input(ctx, fmt.Sprintf("task '%s' の説明(1 行、空にすると消す)", task), current, nil)
}

// Filtering is left off: huh starts a filtered MultiSelect in filter mode, where Space and Enter edit the filter instead of toggling and confirming.
//
// options から複数を選ばせ、選ばれたものを返す(0 個でもよい)。Space で選択を切り替え、Enter で確定する。
// 絞り込み(Filtering)を付けない理由は上のとおり。付けると最初から絞り込み入力の状態で始まり、
// Space や Enter が「選択・確定」ではなく「絞り込みの文字入力」になって、操作できなくなる(huh の挙動)。
func (p *Prompter) chooseMany(ctx context.Context, title string, options []string) ([]string, error) {
	// MultiSelect の結果は、選ばれた値のスライス。何も選ばれなければ長さ 0 のまま(nil)。
	// huh.NewMultiSelect[string]() も NewSelect と同じくジェネリクスで、値の型が string。
	var chosen []string
	field := huh.NewMultiSelect[string]().
		Title(title).
		Options(huh.NewOptions(options...)...).
		Height(selectHeight).
		Value(&chosen)
	if err := p.run(ctx, field); err != nil {
		return nil, err
	}
	return chosen, nil
}

// ChooseSkillTargets asks which agents to install the skills for.
//
// ws skill install を引数なしで実行したときに、インストール先(claude / codex)を複数選ばせる。app.SkillTargets が選択肢の一覧。
func (p *Prompter) ChooseSkillTargets(ctx context.Context) ([]string, error) {
	return p.chooseMany(ctx, "スキルのインストール先(Space で選択)", app.SkillTargets)
}

// Add asks for the fields of partial that are empty.
//
// ws worktree add の引数のうち、指定されていないもの(空のフィールド)だけを対話で尋ね、埋めた AddOptions を返す。
// 指定済みの引数はそのまま使う。流れは、task → repo → 種類(既存 / 新規 / readonly)→ ブランチ(新規なら名前と起点)。
// 呼び出し元(cli)は、これが返した値を、引数を全部渡したときと同じ処理に渡す。
//
// partial は構造体の値渡しなので、書き換えてもコピーだけが変わり、呼び出し側の値には影響しない。
// エラー時も途中までの opts を返しているが、呼び出し側は err を先に見る前提。
func (p *Prompter) Add(ctx context.Context, partial app.AddOptions) (app.AddOptions, error) {
	opts := partial
	var err error
	// task が空なら一覧から選ばせる。「if opts.Task, err = ...; err != nil」は、代入してからエラーを判定する書き方。
	// ここの「=」は、既に宣言した opts.Task と err への代入。
	if opts.Task == "" {
		if opts.Task, err = p.ChooseTask(ctx); err != nil {
			return opts, err
		}
	}
	// repo が空なら、repos/ にある bare repo の一覧から選ばせる。
	// 内側の「repos, err :=」は、このブロックの中だけの新しい err(外側の err を隠す = シャドーイング)。
	// 内側で処理しきって return するので、外側の err とは混ざらない。
	if opts.Repo == "" {
		repos, err := p.app.RepoNames()
		if err != nil {
			return opts, err
		}
		if opts.Repo, err = p.choose(ctx, "repo", repos); err != nil {
			return opts, err
		}
	}
	// 選んだ repo のブランチ一覧(ローカルと origin)を取る。この後の候補表示に使う。
	branches, err := p.app.BranchesOf(ctx, opts.Repo)
	if err != nil {
		return opts, err
	}

	// 種類を決めるフラグ(--create / --readonly)もブランチも指定されていなければ、種類を尋ねる。
	// ブランチだけ指定されていれば「既存ブランチ」と決まるので、尋ねない。
	if !opts.Create && !opts.ReadOnly && opts.Branch == "" {
		mode, err := p.chooseMode(ctx)
		if err != nil {
			return opts, err
		}
		opts.Create = mode == modeNew
		opts.ReadOnly = mode == modeReadonly
	}

	// 種類ごとに、足りないブランチ(と起点)を尋ねる。
	// 式を書かない「switch {」は、上から順に条件を評価して、最初に真になった case だけを実行する(if / else if の連なりと同じ)。
	// Go の switch は case の最後で自動的に抜ける(break を書かなくてよい)。
	switch {
	// readonly: 参照できるのは origin のブランチだけなので、リモートのブランチから選ばせる。
	case opts.ReadOnly && opts.Branch == "":
		opts.Branch, err = p.choose(ctx, "参照するブランチ(origin)", branches.Remote)
	// 新規ブランチ: 名前(初期値はランダムな名前)と、起点(base)を尋ねる。指定済みのものは尋ねない。
	case opts.Create:
		if opts.Branch == "" {
			initial, err := p.app.RandomBranchName(ctx, []string{opts.Repo})
			if err != nil {
				return opts, err
			}
			if opts.Branch, err = p.branchName(ctx, "新しいブランチ名", initial); err != nil {
				return opts, err
			}
		}
		if opts.Base == "" {
			opts.Base, err = p.chooseBase(ctx, opts.Repo, branches.Remote)
		}
	// 既存ブランチ: ローカルと origin のブランチから選ばせる。
	case opts.Branch == "":
		opts.Branch, err = p.choose(ctx, "ブランチ", branches.All())
	}
	// switch の中で最後に代入された err(無ければ nil)を、そのまま返す。
	return opts, err
}

// chooseBase は新規ブランチの起点(base)を、origin のブランチから選ばせる。選択肢は "origin/<branch>" の形で、選んだ文字列を返す。
//
// 初期選択は app.DefaultBase(origin/HEAD、無ければ origin/main)。それが候補に無ければ先頭の候補にする。
// remote は origin のブランチ名("origin/" 抜き)の一覧。候補が空ならエラーを返す。
func (p *Prompter) chooseBase(ctx context.Context, repo string, remote []string) (string, error) {
	bases := make([]string, len(remote))
	for i, branch := range remote {
		bases[i] = "origin/" + branch
	}
	defaultBase, err := p.app.DefaultBase(ctx, repo)
	if err != nil {
		return "", err
	}
	if len(bases) == 0 {
		return "", errors.New("起点にできる origin のブランチがありません")
	}
	// Select は、Value に渡した変数に最初から入っている値を、初期選択として表示する。
	// そのため chosen を空で作らず、既定の起点で初期化してから渡している。
	chosen := defaultBase
	if !slices.Contains(bases, chosen) {
		chosen = bases[0]
	}
	field := huh.NewSelect[string]().
		Title("起点(--base)").
		Options(huh.NewOptions(bases...)...).
		Filtering(true).
		Height(selectHeight).
		Value(&chosen)
	if err := p.run(ctx, field); err != nil {
		return "", err
	}
	return chosen, nil
}

// Rename asks for the fields of partial that are empty.
//
// ws worktree rename の引数のうち、指定されていないものだけを対話で尋ねる。
// 流れは、task → 名前を変える worktree(repo とブランチの組)→ 新しいブランチ名。
// 対象は作業用 worktree で、readonly は含まない。新しいブランチ名の初期値は現在のブランチ名。
// 実際のリネームは行わず、決まった RenameOptions を返すだけ。
func (p *Prompter) Rename(ctx context.Context, partial app.RenameOptions) (app.RenameOptions, error) {
	opts := partial
	if opts.Task == "" {
		var err error
		if opts.Task, err = p.ChooseTask(ctx); err != nil {
			return opts, err
		}
	}
	// repo かブランチのどちらかが未指定なら、task 内の作業用 worktree から選ばせる。
	// 指定済みの値があれば、それに合う候補だけに絞って提示する。
	if opts.Repo == "" || opts.Branch == "" {
		all, err := p.app.RenameCandidates(ctx, opts.Task)
		if err != nil {
			return opts, err
		}
		var candidates []app.RenameCandidate
		for _, c := range all {
			if (opts.Repo == "" || c.Repo == opts.Repo) && (opts.Branch == "" || c.Branch == opts.Branch) {
				candidates = append(candidates, c)
			}
		}
		// 画面に出すラベルは「repo  ブランチ」の形。choose は選ばれた文字列を返すので、
		// あとで slices.Index でラベルの位置を逆引きして、元の候補(Repo と Branch)に戻す。
		labels := make([]string, len(candidates))
		for i, c := range candidates {
			labels[i] = c.Repo + "  " + c.Branch
		}
		label, err := p.choose(ctx, "名前を変える worktree", labels)
		if err != nil {
			return opts, err
		}
		picked := candidates[slices.Index(labels, label)]
		// 2 つの変数に同時に代入する(TypeScript の [a, b] = [x, y] と似ている)。
		opts.Repo, opts.Branch = picked.Repo, picked.Branch
	}
	// 新しいブランチ名が未指定なら、現在のブランチ名を初期値にして入力させる(Enter だけなら変更なし)。
	if opts.To == "" {
		var err error
		if opts.To, err = p.branchName(ctx, "新しいブランチ名", opts.Branch); err != nil {
			return opts, err
		}
	}
	return opts, nil
}
