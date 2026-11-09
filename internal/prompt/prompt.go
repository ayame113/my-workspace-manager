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

type Prompter struct {
	app *app.App
	in  io.Reader
	out io.Writer
}

func New(a *app.App, in io.Reader, out io.Writer) *Prompter {
	return &Prompter{app: a, in: in, out: out}
}

const selectHeight = 12

// ErrAborted is returned when the user cancels a prompt (Ctrl-C / Esc).
var ErrAborted = errors.New("入力が中断されました")

func (p *Prompter) run(ctx context.Context, fields ...huh.Field) error {
	err := huh.NewForm(huh.NewGroup(fields...)).WithInput(p.in).WithOutput(p.out).RunWithContext(ctx)
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrAborted
	}
	return err
}

func (p *Prompter) choose(ctx context.Context, title string, options []string) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("%s: 選択できる候補がありません", title)
	}
	var chosen string
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

func (p *Prompter) input(ctx context.Context, title, initial string, validate func(string) error) (string, error) {
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

func (p *Prompter) branchName(ctx context.Context, title, initial string) (string, error) {
	return p.input(ctx, title, initial, func(name string) error {
		if strings.TrimSpace(name) == "" {
			return errors.New("ブランチ名を入力してください")
		}
		return p.app.ValidateBranchName(ctx, strings.TrimSpace(name))
	})
}

type addMode string

const (
	modeExisting addMode = "既存ブランチ"
	modeNew      addMode = "新規ブランチ"
	modeReadonly addMode = "readonly(main など、複数の task から参照するブランチ)"
)

func (p *Prompter) chooseMode(ctx context.Context) (addMode, error) {
	var mode addMode
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
func (p *Prompter) ChooseTask(ctx context.Context) (string, error) {
	tasks, err := p.app.TaskNames()
	if err != nil {
		return "", err
	}
	if len(tasks) == 0 {
		return "", errors.New("task がありません。先に 'ws new' を実行してください")
	}
	choices := make([]Choice, len(tasks))
	for i, task := range tasks {
		label := task
		if description, err := p.app.Description(task); err == nil && description != "" {
			label += "  " + description
		}
		choices[i] = Choice{Label: label, Value: task}
	}
	return p.PickTask(ctx, choices)
}

// Choice is one option whose label is shown and matched by the filter, and whose value is returned.
type Choice struct {
	Label string
	Value string
}

// PickTask lets the user narrow the choices by typing any part of a label and returns the chosen value.
func (p *Prompter) PickTask(ctx context.Context, choices []Choice) (string, error) {
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
type NewTaskAnswers struct {
	app.NewTaskOptions
	Open bool
}

// New asks for the repos to create worktrees in (none is allowed), then for a task name, a description
// and whether to open the task in the editor.
func (p *Prompter) New(ctx context.Context) (NewTaskAnswers, error) {
	repos, err := p.app.RepoNames()
	if err != nil {
		return NewTaskAnswers{}, err
	}
	var chosen []string
	if len(repos) > 0 {
		chosen, err = p.chooseMany(ctx, "worktree を作る repo(Space で選択、何も選ばなければ task だけ作る)", repos)
		if err != nil {
			return NewTaskAnswers{}, err
		}
	}
	initial, err := p.app.RandomTaskName(chosen)
	if err != nil {
		return NewTaskAnswers{}, err
	}
	task, description, open := initial, "", true
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
	return NewTaskAnswers{
		NewTaskOptions: app.NewTaskOptions{Task: strings.TrimSpace(task), Repos: chosen, Description: description},
		Open:           open,
	}, nil
}

// Describe asks for a task's description, starting from the current one.
func (p *Prompter) Describe(ctx context.Context, task, current string) (string, error) {
	return p.input(ctx, fmt.Sprintf("task '%s' の説明(1 行、空にすると消す)", task), current, nil)
}

// Filtering is left off: huh starts a filtered MultiSelect in filter mode, where Space and Enter edit the filter instead of toggling and confirming.
func (p *Prompter) chooseMany(ctx context.Context, title string, options []string) ([]string, error) {
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
func (p *Prompter) ChooseSkillTargets(ctx context.Context) ([]string, error) {
	return p.chooseMany(ctx, "スキルのインストール先(Space で選択)", app.SkillTargets)
}

// Add asks for the fields of partial that are empty.
func (p *Prompter) Add(ctx context.Context, partial app.AddOptions) (app.AddOptions, error) {
	opts := partial
	var err error
	if opts.Task == "" {
		if opts.Task, err = p.ChooseTask(ctx); err != nil {
			return opts, err
		}
	}
	if opts.Repo == "" {
		repos, err := p.app.RepoNames()
		if err != nil {
			return opts, err
		}
		if opts.Repo, err = p.choose(ctx, "repo", repos); err != nil {
			return opts, err
		}
	}
	branches, err := p.app.BranchesOf(ctx, opts.Repo)
	if err != nil {
		return opts, err
	}

	if !opts.Create && !opts.ReadOnly && opts.Branch == "" {
		mode, err := p.chooseMode(ctx)
		if err != nil {
			return opts, err
		}
		opts.Create = mode == modeNew
		opts.ReadOnly = mode == modeReadonly
	}

	switch {
	case opts.ReadOnly && opts.Branch == "":
		opts.Branch, err = p.choose(ctx, "参照するブランチ(origin)", branches.Remote)
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
	case opts.Branch == "":
		opts.Branch, err = p.choose(ctx, "ブランチ", branches.All())
	}
	return opts, err
}

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
func (p *Prompter) Rename(ctx context.Context, partial app.RenameOptions) (app.RenameOptions, error) {
	opts := partial
	if opts.Task == "" {
		var err error
		if opts.Task, err = p.ChooseTask(ctx); err != nil {
			return opts, err
		}
	}
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
		labels := make([]string, len(candidates))
		for i, c := range candidates {
			labels[i] = c.Repo + "  " + c.Branch
		}
		label, err := p.choose(ctx, "名前を変える worktree", labels)
		if err != nil {
			return opts, err
		}
		picked := candidates[slices.Index(labels, label)]
		opts.Repo, opts.Branch = picked.Repo, picked.Branch
	}
	if opts.To == "" {
		var err error
		if opts.To, err = p.branchName(ctx, "新しいブランチ名", opts.Branch); err != nil {
			return opts, err
		}
	}
	return opts, nil
}
