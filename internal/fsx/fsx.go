package fsx

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"slices"
)

type Stat struct {
	IsRegular bool
	Size      int64
	Mode      uint32
	Dev       int64
	Ino       uint64
	CtimeNs   int64
}

// Exists follows symlinks; any failure to stat counts as absent.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func RealPath(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}

// SubdirNames returns the sorted names of directories directly under dir, excluding symlinks.
func SubdirNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	slices.Sort(names)
	return names, nil
}

func SHA256Hex(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
