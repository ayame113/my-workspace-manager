package fsx

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"slices"

	"golang.org/x/sys/unix"
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

// Clonefile makes a copy-on-write copy of src at dst, which must not exist. Symlinks are not followed.
func Clonefile(src, dst string) error {
	return unix.Clonefile(src, dst, unix.CLONE_NOFOLLOW)
}

// Lstat does not follow symlinks.
func Lstat(path string) (Stat, error) {
	var st unix.Stat_t
	if err := unix.Lstat(path, &st); err != nil {
		return Stat{}, err
	}
	return Stat{
		IsRegular: st.Mode&unix.S_IFMT == unix.S_IFREG,
		Size:      st.Size,
		Mode:      uint32(st.Mode & 0o7777),
		Dev:       int64(st.Dev),
		Ino:       st.Ino,
		CtimeNs:   st.Ctim.Sec*1_000_000_000 + st.Ctim.Nsec,
	}, nil
}
