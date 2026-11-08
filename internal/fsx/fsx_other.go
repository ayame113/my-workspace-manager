//go:build !darwin

package fsx

import "errors"

func Clonefile(src, dst string) error { return errors.ErrUnsupported }

func Lstat(path string) (Stat, error) { return Stat{}, errors.ErrUnsupported }

func AvailableBytes(path string) (uint64, error) { return 0, errors.ErrUnsupported }
