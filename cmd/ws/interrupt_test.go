package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func buildWS(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "ws")
	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return binary
}

func writeNodeModules(t *testing.T, worktree string, files int) {
	t.Helper()
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(worktree, "node_modules", "p")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range files {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%d.js", i)), []byte("same content\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func leftovers(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && (strings.HasPrefix(entry.Name(), ".ws-compact-") || strings.HasSuffix(entry.Name(), ".tmp")) {
			found = append(found, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func TestCompactInterrupted(t *testing.T) {
	binary := buildWS(t)
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	for _, worktree := range []string{"a", "b"} {
		writeNodeModules(t, filepath.Join(root, "tasks", "t1", "repo--"+worktree), 4000)
	}
	env := append(os.Environ(), "WS_ROOT="+root, "HOME="+os.Getenv("HOME"))
	runCompact := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(binary, append([]string{"compact"}, args...)...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("compact %v: %v\n%s", args, err, out)
		}
		return string(out)
	}

	for _, delay := range []time.Duration{0, 20 * time.Millisecond, 60 * time.Millisecond, 150 * time.Millisecond} {
		cmd := exec.Command(binary, "compact")
		cmd.Env = env
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		// 最初の出力は、シグナルハンドラの設置後にしか出ない
		if _, err := bufio.NewReader(stdout).ReadString('\n'); err != nil {
			t.Fatal(err)
		}
		time.Sleep(delay)
		cmd.Process.Signal(syscall.SIGINT)
		io.Copy(io.Discard, stdout)
		err = cmd.Wait()
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok && exitErr.ExitCode() != 130 {
			t.Fatalf("delay %v: exit code %d, want 0 or 130", delay, exitErr.ExitCode())
		}
		if found := leftovers(t, root); len(found) != 0 {
			t.Fatalf("delay %v: leftover files %v", delay, found)
		}
	}

	runCompact()
	if out := runCompact("--dry-run"); !strings.Contains(out, "置き換え対象: 0 ファイル") {
		t.Fatalf("files are still left to replace after compact:\n%s", out)
	}
	for _, worktree := range []string{"a", "b"} {
		content, err := os.ReadFile(filepath.Join(root, "tasks", "t1", "repo--"+worktree, "node_modules", "p", "f0.js"))
		if err != nil || string(content) != "same content\n" {
			t.Fatalf("content = %q, err = %v", content, err)
		}
	}
	if found := leftovers(t, root); len(found) != 0 {
		t.Fatalf("leftover files %v", found)
	}
}
