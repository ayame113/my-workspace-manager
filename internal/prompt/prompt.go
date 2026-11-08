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

func (p *Prompter) run(ctx context.Context, field huh.Field) error {
	err := huh.NewForm(huh.NewGroup(field)).WithInput(p.in).WithOutput(p.out).RunWithContext(ctx)
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
	field := huh.NewInput().Title(title).Value(&value).Validate(validate)
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
	modeReadonly addMode = "readonly(origin のブランチを参照用に)"
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

// Add asks for the fields of partial that are empty.
func (p *Prompter) Add(ctx context.Context, partial app.AddOptions) (app.AddOptions, error) {
	opts := partial
	var err error
	if opts.Task == "" {
		tasks, err := p.app.TaskNames()
		if err != nil {
			return opts, err
		}
		if opts.Task, err = p.choose(ctx, "task", tasks); err != nil {
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
			if opts.Branch, err = p.branchName(ctx, "新しいブランチ名", ""); err != nil {
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
		tasks, err := p.app.TaskNames()
		if err != nil {
			return opts, err
		}
		if opts.Task, err = p.choose(ctx, "task", tasks); err != nil {
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
