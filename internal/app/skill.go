package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ayame113/my-workspace-manager/skills"
)

// SkillTargets are the agents whose user-level skills directory ws can install into.
var SkillTargets = []string{"claude", "codex"}

// SkillsDir returns the user-level skills directory of target.
func (a *App) SkillsDir(target string) (string, error) {
	home := a.getenv("HOME")
	if home == "" {
		return "", errors.New("HOME が設定されていません")
	}
	switch target {
	case "claude":
		return filepath.Join(home, ".claude", "skills"), nil
	case "codex":
		// Codex は ~/.codex ではなく ~/.agents/skills からユーザー単位の skill を読む
		return filepath.Join(home, ".agents", "skills"), nil
	}
	return "", fmt.Errorf("不明なインストール先です: %s", target)
}

// InstallSkills writes the skills bundled in the binary into the skills directory of target,
// overwriting files of the same name, and returns the directory of each skill.
// Files that an older version wrote and this one no longer ships are left in place.
func (a *App) InstallSkills(target string) ([]string, error) {
	base, err := a.SkillsDir(target)
	if err != nil {
		return nil, err
	}
	entries, err := fs.ReadDir(skills.FS, ".")
	if err != nil {
		return nil, err
	}
	var installed []string
	for _, entry := range entries {
		dir := filepath.Join(base, entry.Name())
		if err := copyEmbedded(entry.Name(), dir); err != nil {
			return installed, fmt.Errorf("%s に書き込めません: %w", dir, err)
		}
		installed = append(installed, dir)
	}
	return installed, nil
}

func copyEmbedded(src, dst string) error {
	return fs.WalkDir(skills.FS, src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(skills.FS, path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
