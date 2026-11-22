package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// maxPathLinks bounds the links checkProjectPath follows for one path.
const maxPathLinks = 40

var errTooManyLinks = errors.New("too many links")

// refusedPath reports whether schema fetch refuses to read p when the project
// names it. Package-level so tests can exercise it on any platform.
var refusedPath = func(p string) bool {
	return networkOrDeviceVolume(filepath.VolumeName(p))
}

// networkOrDeviceVolume reports whether vol, as filepath.VolumeName returns
// it, is anything but a drive letter: a UNC share or a device path, in any of
// the spellings Windows takes, \??\ included. It is always "" elsewhere.
func networkOrDeviceVolume(vol string) bool {
	return vol != "" && (len(vol) != 2 || vol[1] != ':')
}

// checkProjectPath reports an error when p, a path that project content
// names, is a refused path or leads to one through a link. A relative p is
// relative to base, which the user chose and isn't limited. Each component is
// looked at with Lstat and each link with Readlink, so nothing is opened
// through a link before its target has been checked; EvalSymlinks would open
// it. A path that doesn't exist is fine.
func checkProjectPath(base, p string) error {
	abs, err := filepath.Abs(base)
	if err != nil {
		return fmt.Errorf("checking %s: %w", p, err)
	}
	hops := 0
	if _, err := followChecked(abs, p, &hops); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("checking %s: %w", p, err)
	}
	return nil
}

// followChecked resolves p from base one component at a time, checking each
// link's target before following it, and returns where it leads.
func followChecked(base, p string, hops *int) (string, error) {
	if refusedPath(p) {
		return "", fmt.Errorf("%q is a network or device path, which schema fetch doesn't read from a project: pass the realm with --dir if you trust it", p)
	}
	cur, rest := base, filepath.ToSlash(p)
	switch vol := filepath.VolumeName(p); {
	case filepath.IsAbs(p):
		cur, rest = vol+string(filepath.Separator), filepath.ToSlash(p[len(vol):])
	case vol != "":
		// C:dir is relative to that drive's current directory.
		abs, err := filepath.Abs(p)
		if err != nil {
			return "", fmt.Errorf("resolving %q: %w", p, err)
		}
		cur, rest = vol+string(filepath.Separator), filepath.ToSlash(abs[len(vol):])
	case strings.HasPrefix(rest, "/"):
		cur = filepath.VolumeName(base) + string(filepath.Separator) // rooted, on base's drive
	}
	for _, part := range strings.Split(rest, "/") {
		if part == "" || part == "." {
			continue
		}
		next := filepath.Join(cur, part)
		if part == ".." {
			cur = next
			continue
		}
		fi, err := os.Lstat(next)
		if err != nil {
			return "", fmt.Errorf("looking at %s: %w", next, err)
		}
		// A Windows junction is irregular rather than a symbolic link.
		if fi.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			target, err := os.Readlink(next)
			switch {
			case err != nil && fi.Mode()&fs.ModeSymlink != 0:
				return "", fmt.Errorf("reading the link %s: %w", next, err)
			case err == nil:
				*hops++
				if *hops > maxPathLinks {
					return "", fmt.Errorf("following %s: %w", next, errTooManyLinks)
				}
				resolved, err := followChecked(cur, target, hops)
				if err != nil {
					return "", fmt.Errorf("following the link %s: %w", next, err)
				}
				next = resolved
			}
		}
		cur = next
	}
	return cur, nil
}
