package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"

	"github.com/ayame113/my-workspace-manager/internal/fsx"
)

type CompactEventKind int

const (
	CompactScanProgress CompactEventKind = iota
	CompactScanned
	CompactHashProgress
	CompactHashed
)

type CompactEvent struct {
	Kind  CompactEventKind
	Count int
	Total int
}

type CompactResult struct {
	DryRun         bool
	Targets        int
	SavingBytes    int64
	Replaced       int
	ReplacedBytes  int64
	Changed        int
	NotCloned      int
	AvailableFirst *uint64
	AvailableLast  *uint64
}

type compactFile struct {
	path     string
	stat     fsx.Stat
	recorded *indexEntry
	hash     string
}

func (f *compactFile) entry(shared bool) indexEntry {
	return indexEntry{inode: f.stat.Ino, ctimeNs: f.stat.CtimeNs, size: f.stat.Size, hash: f.hash, shared: shared}
}

type inodeKey struct {
	dev int64
	ino uint64
}

type scanner struct {
	ctx    context.Context
	seen   map[inodeKey]bool
	files  []*compactFile
	notify func(CompactEvent)
}

// scan collects non-empty regular files under node_modules (nested ones included). Symlinks are not followed and hard links count once.
func (s *scanner) scan(directory string, insideNodeModules bool) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		if entry.IsDir() {
			if entry.Name() == ".git" {
				continue
			}
			if err := s.scan(path, insideNodeModules || entry.Name() == "node_modules"); err != nil {
				return err
			}
			continue
		}
		if !insideNodeModules {
			continue
		}
		stat, err := fsx.Lstat(path)
		key := inodeKey{stat.Dev, stat.Ino}
		if err != nil || !stat.IsRegular || stat.Size == 0 || s.seen[key] {
			continue
		}
		s.seen[key] = true
		s.files = append(s.files, &compactFile{path: path, stat: stat})
		if len(s.files)%10000 == 0 {
			s.notify(CompactEvent{Kind: CompactScanProgress, Count: len(s.files)})
		}
	}
	return nil
}

func (a *App) scanTasks(ctx context.Context, notify func(CompactEvent)) ([]*compactFile, error) {
	s := &scanner{ctx: ctx, seen: map[inodeKey]bool{}, notify: notify}
	if !fsx.Exists(a.layout.TasksRoot()) {
		return nil, nil
	}
	tasks, err := fsx.SubdirNames(a.layout.TasksRoot())
	if err != nil {
		return nil, err
	}
	for _, task := range tasks {
		for _, dir := range []string{a.layout.TaskDir(task), a.layout.ReadonlyDir(task)} {
			names, err := listWorktreeNames(dir)
			if err != nil {
				return nil, err
			}
			for _, name := range names {
				if err := s.scan(dir+"/"+name, false); err != nil {
					return nil, err
				}
			}
		}
	}
	return s.files, nil
}

// unchanged reports whether path still matches its scan (ctime moves on any content or mode change).
func unchanged(path string, scanned fsx.Stat) bool {
	now, err := fsx.Lstat(path)
	return err == nil && now.Ino == scanned.Ino && now.Size == scanned.Size && now.CtimeNs == scanned.CtimeNs
}

// replaceWithClone replaces target by a clone of representative; false means cloning is not possible.
// Within one directory the rename is atomic, so target always exists with either the old or the new content.
func replaceWithClone(target, representative string) (bool, error) {
	temporary := filepath.Join(filepath.Dir(target), ".ws-compact-"+filepath.Base(target))
	if err := os.Remove(temporary); err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if err := fsx.Clonefile(representative, temporary); err != nil {
		return false, nil
	}
	if err := os.Rename(temporary, target); err != nil {
		os.Remove(temporary)
		return false, err
	}
	return true, nil
}

type replacement struct {
	target         *compactFile
	representative *compactFile
}

type contentKey struct {
	hash string
	mode uint32
}

// plan picks one representative per group of identical files and lists the files to replace by its clone.
func plan(groups [][]*compactFile) (map[string]indexEntry, []replacement) {
	next := map[string]indexEntry{}
	var replacements []replacement
	for _, group := range groups {
		var keys []contentKey
		byKey := map[contentKey][]*compactFile{}
		for _, file := range group {
			if file.hash == "" {
				next[file.path] = file.entry(false)
				continue
			}
			key := contentKey{file.hash, file.stat.Mode}
			if _, ok := byKey[key]; !ok {
				keys = append(keys, key)
			}
			byKey[key] = append(byKey[key], file)
		}
		for _, key := range keys {
			members := byKey[key]
			// 前回の代表が変わらず残っていればそれを使い続け、そのクローン済みのものは置き換えない
			previous := slices.IndexFunc(members, func(f *compactFile) bool {
				return f.recorded != nil && !f.recorded.shared
			})
			representative := members[max(previous, 0)]
			next[representative.path] = representative.entry(false)
			for _, file := range members {
				if file == representative {
					continue
				}
				if previous >= 0 && file.recorded != nil && file.recorded.shared {
					next[file.path] = file.entry(true)
				} else {
					replacements = append(replacements, replacement{file, representative})
				}
			}
		}
	}
	return next, replacements
}

// Compact replaces identical files (same size, SHA-256 and mode) in the node_modules of every worktree by clones of one representative.
// With dryRun nothing is modified.
func (a *App) Compact(ctx context.Context, dryRun bool, notify func(CompactEvent)) (CompactResult, error) {
	result := CompactResult{DryRun: dryRun}
	if avail, err := fsx.AvailableBytes(a.layout.Root); err == nil {
		result.AvailableFirst = &avail
	}
	indexPath := a.layout.CompactIndexPath()
	indexed := loadCompactIndex(indexPath)
	files, err := a.scanTasks(ctx, notify)
	if err != nil {
		return result, err
	}
	notify(CompactEvent{Kind: CompactScanned, Count: len(files)})

	var sizes []int64
	bySize := map[int64][]*compactFile{}
	for _, file := range files {
		if recorded, ok := indexed[file.path]; ok && recorded.matches(file.stat) {
			file.recorded = &recorded
			file.hash = recorded.hash
		}
		if _, ok := bySize[file.stat.Size]; !ok {
			sizes = append(sizes, file.stat.Size)
		}
		bySize[file.stat.Size] = append(bySize[file.stat.Size], file)
	}
	groups := make([][]*compactFile, 0, len(sizes))
	var toHash []*compactFile
	for _, size := range sizes {
		group := bySize[size]
		groups = append(groups, group)
		if len(group) >= 2 {
			toHash = slices.AppendSeq(toHash, func(yield func(*compactFile) bool) {
				for _, file := range group {
					if file.hash == "" && !yield(file) {
						return
					}
				}
			})
		}
	}
	for i, file := range toHash {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if file.hash, err = fsx.SHA256Hex(file.path); err != nil {
			return result, err
		}
		if (i+1)%1000 == 0 {
			notify(CompactEvent{Kind: CompactHashProgress, Count: i + 1, Total: len(toHash)})
		}
	}
	notify(CompactEvent{Kind: CompactHashed, Count: len(toHash)})

	next, replacements := plan(groups)
	result.Targets = len(replacements)
	for _, r := range replacements {
		result.SavingBytes += r.target.stat.Size
	}
	if dryRun {
		return result, nil
	}

	var interrupted error
	for _, r := range replacements {
		if interrupted = ctx.Err(); interrupted != nil {
			break
		}
		// 走査後に変更されたファイルは、内容が変わっている可能性があるので触らない
		if !unchanged(r.target.path, r.target.stat) || !unchanged(r.representative.path, r.representative.stat) {
			result.Changed++
			continue
		}
		cloned, err := replaceWithClone(r.target.path, r.representative.path)
		if err != nil {
			return result, err
		}
		if !cloned {
			result.NotCloned++
			continue
		}
		result.Replaced++
		result.ReplacedBytes += r.target.stat.Size
		// 置き換えで inode と ctime が変わるので、置き換え後の状態を「共有済み」として記録する
		if stat, err := fsx.Lstat(r.target.path); err == nil {
			updated := *r.target
			updated.stat = stat
			next[r.target.path] = updated.entry(true)
		}
	}
	// 中断時も、ここまでに置き換えた分は記録して次回に引き継ぐ
	if err := saveCompactIndex(indexPath, next); err != nil {
		return result, err
	}
	if interrupted != nil {
		return result, interrupted
	}
	if avail, err := fsx.AvailableBytes(a.layout.Root); err == nil {
		result.AvailableLast = &avail
	}
	return result, nil
}
