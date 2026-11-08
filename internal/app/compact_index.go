package app

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/ayame113/my-workspace-manager/internal/fsx"
)

const compactIndexHeader = "ws-compact-index-v2"

// indexEntry records the state of a processed file. A matching inode, ctime and size mean the content is unchanged,
// so the recorded hash can be reused. ctime is used instead of mtime because npm pins mtime to a fixed date on extraction.
type indexEntry struct {
	inode   uint64
	ctimeNs int64
	size    int64
	// 空なら、サイズが一意で計算していない
	hash string
	// 同じキーの代表のクローンに置き換え済み
	shared bool
}

func (e indexEntry) matches(stat fsx.Stat) bool {
	return e.inode == stat.Ino && e.ctimeNs == stat.CtimeNs && e.size == stat.Size
}

// loadCompactIndex returns an empty index when the file is missing or corrupt, which reprocesses every file.
func loadCompactIndex(path string) map[string]indexEntry {
	entries := map[string]indexEntry{}
	data, err := os.ReadFile(path)
	if err != nil {
		return entries
	}
	lines := strings.Split(string(data), "\n")
	if lines[0] != compactIndexHeader {
		return map[string]indexEntry{}
	}
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 6)
		if len(fields) != 6 {
			return map[string]indexEntry{}
		}
		inode, err1 := strconv.ParseUint(fields[0], 10, 64)
		ctimeNs, err2 := strconv.ParseInt(fields[1], 10, 64)
		size, err3 := strconv.ParseInt(fields[2], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil {
			return map[string]indexEntry{}
		}
		hash := fields[3]
		if hash == "-" {
			hash = ""
		}
		entries[fields[5]] = indexEntry{inode: inode, ctimeNs: ctimeNs, size: size, hash: hash, shared: fields[4] == "1"}
	}
	return entries
}

// saveCompactIndex writes to a temporary file and renames it so an interruption never leaves a broken index.
func saveCompactIndex(path string, entries map[string]indexEntry) error {
	var text strings.Builder
	text.WriteString(compactIndexHeader + "\n")
	for _, file := range slices.Sorted(func(yield func(string) bool) {
		for file := range entries {
			if !yield(file) {
				return
			}
		}
	}) {
		entry := entries[file]
		shared, hash := "0", entry.hash
		if entry.shared {
			shared = "1"
		}
		if hash == "" {
			hash = "-"
		}
		fmt.Fprintf(&text, "%d\t%d\t%d\t%s\t%s\t%s\n", entry.inode, entry.ctimeNs, entry.size, hash, shared, file)
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, []byte(text.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
