package cli

import (
	"slices"

	"github.com/spf13/cobra"

	"github.com/ayame113/my-workspace-manager/internal/app"
)

type completeFunc = func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective)

func candidates(names []string, err error) ([]cobra.Completion, cobra.ShellCompDirective) {
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// disableFileCompletion stops shells from offering file names where ws takes no paths.
func disableFileCompletion(cmd *cobra.Command) {
	if cmd.ValidArgsFunction == nil && len(cmd.ValidArgs) == 0 {
		cmd.ValidArgsFunction = cobra.NoFileCompletions
	}
	for _, child := range cmd.Commands() {
		disableFileCompletion(child)
	}
}

func (r *runner) completeTasks(*cobra.Command, []string, string) ([]cobra.Completion, cobra.ShellCompDirective) {
	names, err := r.app.TaskNames()
	return candidates(slices.Sorted(slices.Values(names)), err)
}

func (r *runner) completeRepos(*cobra.Command, []string, string) ([]cobra.Completion, cobra.ShellCompDirective) {
	return candidates(r.app.RepoNames())
}

// completeAddBranch needs --repo given earlier on the line.
func (r *runner) completeAddBranch(opts *app.AddOptions) completeFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if opts.Repo == "" {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		branches, err := r.app.BranchesOf(cmd.Context(), opts.Repo)
		if opts.ReadOnly {
			return candidates(branches.Remote, err)
		}
		return candidates(branches.All(), err)
	}
}

func (r *runner) completeBase(opts *app.AddOptions) completeFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if opts.Repo == "" {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		branches, err := r.app.BranchesOf(cmd.Context(), opts.Repo)
		bases := make([]string, len(branches.Remote))
		for i, branch := range branches.Remote {
			bases[i] = "origin/" + branch
		}
		return candidates(bases, err)
	}
}

// completeRenameBranch offers the branches checked out in the task's work worktrees, narrowed by --repo when given.
func (r *runner) completeRenameBranch(opts *app.RenameOptions) completeFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		task, err := r.app.ResolveTask(opts.Task)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		all, err := r.app.RenameCandidates(cmd.Context(), task)
		var branches []string
		for _, c := range all {
			if opts.Repo == "" || c.Repo == opts.Repo {
				branches = append(branches, c.Branch)
			}
		}
		return candidates(branches, err)
	}
}
