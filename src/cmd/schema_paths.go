package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	errRefusedVolume = errors.New("is a network or device path")
	errProjectLink   = errors.New("is a link, which schema fetch doesn't follow in a project on Windows")
)

// refuseProjectLinks reports whether projectPath refuses links. Package-level
// so tests can exercise that on any platform.
var refuseProjectLinks = runtime.GOOS == "windows"

// networkOrDeviceVolume reports whether vol, as filepath.VolumeName returns
// it, is anything but a drive letter: a UNC share or a device path, in any of
// the spellings Windows takes, \??\ included. It is always "" elsewhere.
func networkOrDeviceVolume(vol string) bool {
	return vol != "" && (len(vol) != 2 || vol[1] != ':')
}

// projectPath returns the path to open for p, a path that project content
// names relative to base, which the user chose and isn't limited. It is
// joined and cleaned as check does, and callers open only what it returns:
// opening another spelling of p would undo the check.
//
// p must not be a network or device path. On Windows no component it adds to
// base may be a link or a junction either. They are refused, not resolved, as
// any resolution of our own could differ from the system's; with none, the
// cleaned path is the one the system opens. A component that doesn't exist
// ends the walk, as nothing past it can be opened.
func projectPath(base, p string) (string, error) {
	if networkOrDeviceVolume(filepath.VolumeName(p)) {
		return "", fmt.Errorf("%q %w: pass the realm with --dir if you trust it", p, errRefusedVolume)
	}
	start, err := filepath.Abs(base)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", base, err)
	}
	full := filepath.Join(start, p)
	if filepath.IsAbs(p) {
		full = filepath.Clean(p)
		start = filepath.VolumeName(full) + string(filepath.Separator)
	}
	if !refuseProjectLinks {
		return full, nil
	}

	rel, err := filepath.Rel(start, full)
	if err != nil {
		return "", fmt.Errorf("resolving %q: %w", p, err)
	}
	cur := start
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		if part == "." || part == ".." {
			continue // up from base, through what the user chose
		}
		fi, err := os.Lstat(cur) // doesn't follow cur
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("looking at %s: %w", cur, err)
		}
		if isLinkOrJunction(cur, fi.Mode()) {
			return "", fmt.Errorf("%s %w: pass the realm it leads to with --dir if you trust it", cur, errProjectLink)
		}
	}
	return full, nil
}

// isLinkOrJunction reports whether path, of the given mode, is a symbolic link
// or a junction. A junction is irregular and reads as a link; other irregular
// files, such as a cloud drive's placeholders, don't.
func isLinkOrJunction(path string, mode fs.FileMode) bool {
	if mode&fs.ModeSymlink != 0 {
		return true
	}
	if mode&fs.ModeIrregular == 0 {
		return false
	}
	_, err := os.Readlink(path)
	return err == nil
}
