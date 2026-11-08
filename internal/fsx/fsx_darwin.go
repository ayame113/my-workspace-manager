package fsx

import (
	"golang.org/x/sys/unix"
)

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

// AvailableBytes returns the free space of the filesystem containing path.
func AvailableBytes(path string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}
