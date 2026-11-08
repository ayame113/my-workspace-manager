package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){"ws": main})
}

func TestScript(t *testing.T) {
	t.Parallel()
	testscript.Run(t, testscript.Params{
		Dir:   "../../testdata/script",
		Setup: setupEnv,
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			"wsrc":       cmdWsRC,
			"mkremote":   cmdMkRemote,
			"pushchange": cmdPushChange,
		},
	})
}

func setupEnv(env *testscript.Env) error {
	const date = "2026-01-01T00:00:00Z"
	gitconfig := filepath.Join(env.WorkDir, "gitconfig")
	if err := os.WriteFile(gitconfig, []byte("[init]\n\tdefaultBranch = review\n"), 0o644); err != nil {
		return err
	}
	for _, dir := range []string{"remotes", "outside/p"} {
		if err := os.MkdirAll(filepath.Join(env.WorkDir, dir), 0o755); err != nil {
			return err
		}
	}
	env.Setenv("WS_ROOT", filepath.Join(env.WorkDir, "root"))
	env.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	env.Setenv("GIT_CONFIG_GLOBAL", gitconfig)
	env.Setenv("GIT_AUTHOR_NAME", "t")
	env.Setenv("GIT_AUTHOR_EMAIL", "a@b")
	env.Setenv("GIT_COMMITTER_NAME", "t")
	env.Setenv("GIT_COMMITTER_EMAIL", "a@b")
	env.Setenv("GIT_AUTHOR_DATE", date)
	env.Setenv("GIT_COMMITTER_DATE", date)
	return nil
}

func runGit(ts *testscript.TestScript, dir string, args ...string) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	for _, name := range []string{"PATH", "HOME", "TMPDIR", "GIT_CONFIG_NOSYSTEM", "GIT_CONFIG_GLOBAL", "GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL", "GIT_AUTHOR_DATE", "GIT_COMMITTER_DATE"} {
		cmd.Env = append(cmd.Env, name+"="+ts.Getenv(name))
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		ts.Fatalf("git %v: %v\n%s", args, err, out.String())
	}
}

// cmdMkRemote: mkremote <name> [branch...] creates remotes/<name>.git from a seed repo holding the branches.
func cmdMkRemote(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) < 1 {
		ts.Fatalf("usage: mkremote name [branch...]")
	}
	name := args[0]
	work := ts.Getenv("WORK")
	seed := filepath.Join(work, "seed-"+name)
	if _, err := os.Stat(seed); err != nil {
		ts.Check(os.MkdirAll(seed, 0o755))
		runGit(ts, seed, "init", "-q", ".")
		ts.Check(os.WriteFile(filepath.Join(seed, "f"), []byte("1\n"), 0o644))
		runGit(ts, seed, "add", "-A")
		runGit(ts, seed, "commit", "-qm", "init")
	}
	for _, branch := range args[1:] {
		runGit(ts, seed, "branch", branch)
	}
	runGit(ts, seed, "clone", "-q", "--bare", ".", filepath.Join(work, "remotes", name+".git"))
}

// cmdPushChange: pushchange <name> <branch> commits to the branch in the seed repo and pushes it.
func cmdPushChange(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 2 {
		ts.Fatalf("usage: pushchange name branch")
	}
	name, branch := args[0], args[1]
	work := ts.Getenv("WORK")
	seed := filepath.Join(work, "seed-"+name)
	runGit(ts, seed, "checkout", "-q", branch)
	content, err := os.ReadFile(filepath.Join(seed, "f"))
	ts.Check(err)
	ts.Check(os.WriteFile(filepath.Join(seed, "f"), append(content, []byte(branch+"\n")...), 0o644))
	runGit(ts, seed, "commit", "-qam", branch)
	runGit(ts, seed, "push", "-q", filepath.Join(work, "remotes", name+".git"), branch)
}

// cmdWsRC: wsrc <code> [args...] runs ws in the current directory and requires the given exit code.
func cmdWsRC(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) < 1 {
		ts.Fatalf("usage: wsrc code [args...]")
	}
	want, err := strconv.Atoi(args[0])
	ts.Check(err)
	var binary string
	for _, dir := range filepath.SplitList(ts.Getenv("PATH")) {
		if candidate := filepath.Join(dir, "ws"); fileExists(candidate) {
			binary = candidate
			break
		}
	}
	if binary == "" {
		ts.Fatalf("ws not found in PATH")
	}
	cmd := exec.Command(binary, args[1:]...)
	cmd.Dir = ts.Getenv("PWD")
	for _, name := range []string{"PATH", "HOME", "TMPDIR", "WS_ROOT", "CLAUDECODE", "GIT_CONFIG_NOSYSTEM", "GIT_CONFIG_GLOBAL", "GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL", "GIT_AUTHOR_DATE", "GIT_COMMITTER_DATE"} {
		cmd.Env = append(cmd.Env, name+"="+ts.Getenv(name))
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err = cmd.Run()
	got := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		got = exitErr.ExitCode()
	} else if err != nil {
		ts.Fatalf("run ws: %v", err)
	}
	if got != want {
		ts.Fatalf("ws %v: exit code %d, want %d\n%s", args[1:], got, want, out.String())
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
