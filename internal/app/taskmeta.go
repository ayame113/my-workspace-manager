package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	taskMetaFile    = ".ws-task.json"
	keyDescription  = "description"
	keyInstructions = "_comment"
)

func (a *App) taskMetaPath(task string) string {
	return filepath.Join(a.layout.TaskDir(task), taskMetaFile)
}

// readTaskMeta returns every key of the task's metadata file, including ones this version of ws does not know,
// so that writing it back never drops what a newer ws stored. A missing file is an empty map.
func (a *App) readTaskMeta(task string) (map[string]json.RawMessage, error) {
	path := a.taskMetaPath(task)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	meta := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("%s を読めません: %w", path, err)
	}
	return meta, nil
}

func (a *App) writeTaskMeta(task string, meta map[string]json.RawMessage) error {
	path := a.taskMetaPath(task)
	delete(meta, keyInstructions)
	if len(meta) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	instructions, err := marshalReadable(fmt.Sprintf(
		"このファイルは ws が管理する。直接編集せず、説明の表示は `ws describe --task %[1]s`、変更は `ws describe --task %[1]s \"<1 行の説明>\"` で行う。", task))
	if err != nil {
		return err
	}
	meta[keyInstructions] = instructions
	data, err := marshalReadable(meta)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), taskMetaFile+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// marshalReadable keeps <, > and & as they are, since people and agents read this file.
func marshalReadable(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Description returns the task's one-line description, or "" when it has none.
func (a *App) Description(task string) (string, error) {
	meta, err := a.readTaskMeta(task)
	if err != nil {
		return "", err
	}
	raw, ok := meta[keyDescription]
	if !ok {
		return "", nil
	}
	var description string
	if err := json.Unmarshal(raw, &description); err != nil {
		return "", fmt.Errorf("%s の %s が文字列ではありません", a.taskMetaPath(task), keyDescription)
	}
	return description, nil
}

// SetDescription stores description as a single line (whitespace runs, including newlines, become one space).
// An empty description removes it. It returns the stored value.
func (a *App) SetDescription(task, description string) (string, error) {
	meta, err := a.readTaskMeta(task)
	if err != nil {
		return "", err
	}
	description = strings.Join(strings.Fields(description), " ")
	if description == "" {
		delete(meta, keyDescription)
	} else {
		raw, err := marshalReadable(description)
		if err != nil {
			return "", err
		}
		meta[keyDescription] = raw
	}
	return description, a.writeTaskMeta(task, meta)
}
