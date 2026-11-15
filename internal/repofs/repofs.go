// SPDX-License-Identifier: MIT

// Package repofs is docsync's only access to the files of the directory it
// checks. Reads are confined to that directory with an os.Root: docsync
// reads regular files inside it, follows a symbolic link only when it
// resolves inside it, and keeps every file and the whole run within a size
// budget. Writes (docsync api) are confined to one directory below it and
// never follow a symbolic link. Whatever breaks a rule is refused with a
// *RefusedError, never skipped.
package repofs

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// The limits of one run. They are far above the documents and Go sources of
// any real repository: a file or a tree beyond them is refused.
const (
	// MaxFileSize is the largest file docsync reads: 64 MiB.
	MaxFileSize = 64 << 20
	// MaxTotalSize is the most docsync reads in one run, over all files:
	// 1 GiB.
	MaxTotalSize = 1 << 30
	// MaxEntries is the most directory entries one run lists: 1,048,576.
	MaxEntries = 1 << 20
)

// Limits bound what one FS reads.
type Limits struct {
	File    int64 // bytes of one file
	Total   int64 // bytes of all files together
	Entries int   // directory entries listed
}

// Default are the limits docsync runs with.
var Default = Limits{File: MaxFileSize, Total: MaxTotalSize, Entries: MaxEntries}

// RefusedError is a path of the checked directory that docsync does not
// read, write or remove.
type RefusedError struct {
	Op     string // read, list, write or remove
	Path   string // slash-separated, relative to the checked directory
	Reason string
}

func (e *RefusedError) Error() string {
	return fmt.Sprintf("cannot %s %s: %s", e.Op, strconv.Quote(e.Path), e.Reason)
}

// FS reads one directory tree. It is not safe for concurrent use.
type FS struct {
	dir     string
	root    *os.Root
	lim     Limits
	read    int64
	listed  int
	refused []*RefusedError
}

// Open opens the directory dir.
func Open(dir string, lim Limits) (*FS, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &FS{dir: dir, root: root, lim: lim}, nil
}

// Dir returns the directory as Open was given it.
func (f *FS) Dir() string { return f.dir }

// Close releases the directory.
func (f *FS) Close() error { return f.root.Close() }

// Refused returns every refusal of the run so far, in order. A check that
// meets a refused file goes on without it; the run then fails with these.
func (f *FS) Refused() []*RefusedError { return f.refused }

func (f *FS) refuse(op, rel, reason string) error {
	e := &RefusedError{Op: op, Path: rel, Reason: reason}
	f.refused = append(f.refused, e)
	return e
}

// fail keeps an error that says the file does not exist and turns any
// other into a refusal.
func (f *FS) fail(op, rel string, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	return f.refuse(op, rel, err.Error())
}

// ReadFile returns the content of the regular file rel (slash-separated,
// relative to the directory). The error wraps fs.ErrNotExist when there is
// no such file; any other error is a *RefusedError: rel leaves the
// directory, is not a regular file, or is beyond a limit.
func (f *FS) ReadFile(rel string) ([]byte, error) {
	name := filepath.FromSlash(rel)
	fi, err := f.root.Stat(name)
	if err != nil {
		return nil, f.fail("read", rel, err)
	}
	if err := f.admit(rel, fi); err != nil {
		return nil, err
	}
	fh, err := f.root.OpenFile(name, os.O_RDONLY|openFlag, 0)
	if err != nil {
		return nil, f.fail("read", rel, err)
	}
	defer func() { _ = fh.Close() }()
	// The file may have been replaced since the Stat.
	if fi, err = fh.Stat(); err != nil {
		return nil, f.fail("read", rel, err)
	}
	if err := f.admit(rel, fi); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.Grow(int(fi.Size()) + bytes.MinRead)
	if _, err := b.ReadFrom(io.LimitReader(fh, f.lim.File+1)); err != nil {
		return nil, f.fail("read", rel, err)
	}
	n := int64(b.Len())
	if n > f.lim.File {
		return nil, f.refuse("read", rel, fmt.Sprintf("larger than %d bytes, the most docsync reads of one file", f.lim.File))
	}
	if f.read += n; f.read > f.lim.Total {
		return nil, f.refuse("read", rel, fmt.Sprintf("reading it exceeds %d bytes, the most docsync reads in one run", f.lim.Total))
	}
	return b.Bytes(), nil
}

// admit checks a file before it is read.
func (f *FS) admit(rel string, fi fs.FileInfo) error {
	switch {
	case fi.IsDir():
		return f.refuse("read", rel, "a directory where a file is expected")
	case !fi.Mode().IsRegular():
		return f.refuse("read", rel, "not a regular file ("+fi.Mode().Type().String()+")")
	case fi.Size() > f.lim.File:
		return f.refuse("read", rel, fmt.Sprintf("%d bytes, more than the %d docsync reads of one file", fi.Size(), f.lim.File))
	case f.read+fi.Size() > f.lim.Total:
		return f.refuse("read", rel, fmt.Sprintf("reading it exceeds %d bytes, the most docsync reads in one run", f.lim.Total))
	}
	return nil
}

// Exists reports whether the directory has an entry rel of any kind; a
// symbolic link counts, wherever it points.
func (f *FS) Exists(rel string) bool {
	_, err := f.root.Lstat(filepath.FromSlash(rel))
	return err == nil
}

// ReadDir returns the entries of the directory rel (slash-separated, "."
// for the root), sorted by name. The error wraps fs.ErrNotExist when there
// is no such directory; any other error is a *RefusedError.
func (f *FS) ReadDir(rel string) ([]fs.DirEntry, error) {
	name := filepath.FromSlash(rel)
	fi, err := f.root.Stat(name)
	if err != nil {
		return nil, f.fail("list", rel, err)
	}
	if !fi.IsDir() {
		return nil, f.refuse("list", rel, "not a directory")
	}
	d, err := f.root.OpenFile(name, os.O_RDONLY|openFlag, 0)
	if err != nil {
		return nil, f.fail("list", rel, err)
	}
	defer func() { _ = d.Close() }()
	var out []fs.DirEntry
	for {
		batch, err := d.ReadDir(256)
		if f.listed += len(batch); f.listed > f.lim.Entries {
			return nil, f.refuse("list", rel, fmt.Sprintf("the run lists more than %d directory entries", f.lim.Entries))
		}
		out = append(out, batch...)
		if errors.Is(err, io.EOF) || err == nil && len(batch) == 0 {
			break
		}
		if err != nil {
			return nil, f.fail("list", rel, err)
		}
	}
	slices.SortFunc(out, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return out, nil
}

// Walk calls fn for every entry below the root, depth first and in lexical
// order like fs.WalkDir, with rel slash-separated. A symbolic link is
// reported as itself and not followed. fs.SkipDir returned for a directory
// skips it; any other error stops the walk.
func (f *FS) Walk(fn func(rel string, e fs.DirEntry) error) error { return f.walk(".", fn) }

func (f *FS) walk(dir string, fn func(string, fs.DirEntry) error) error {
	entries, err := f.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		rel := path.Join(dir, e.Name())
		if err := fn(rel, e); err != nil {
			if e.IsDir() && errors.Is(err, fs.SkipDir) {
				continue
			}
			return err
		}
		if e.IsDir() {
			if err := f.walk(rel, fn); err != nil {
				return err
			}
		}
	}
	return nil
}

// Update makes the directory dir of the tree (slash-separated: docs/api)
// hold the files of write, and removes the files of remove. Names are
// slash-separated and relative to dir; a name of write is one path
// element. A file that already has its content is left alone.
//
// Nothing outside dir changes and no symbolic link is followed: dir and
// each of its parents must be a directory, not a link (a missing one is
// created), and every file Update replaces or removes must be a regular
// file. Everything is checked before the first change, so a refusal (a
// *RefusedError) changes nothing. It returns the paths it wrote and
// removed, relative to the tree.
func (f *FS) Update(dir string, write map[string][]byte, remove []string) (wrote, removed []string, err error) {
	for n := range write {
		if strings.Contains(n, "/") || !local(n) {
			return nil, nil, f.refuse("write", path.Join(dir, n), "not a file name inside "+dir)
		}
	}
	for _, n := range remove {
		if !local(n) {
			return nil, nil, f.refuse("remove", path.Join(dir, n), "not inside "+dir)
		}
	}
	// dir and its parents: real directories, or missing.
	exists, cur := true, ""
	for _, el := range strings.Split(dir, "/") {
		cur = path.Join(cur, el)
		fi, err := f.root.Lstat(filepath.FromSlash(cur))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			exists = false
		case err != nil:
			return nil, nil, f.fail("write", cur, err)
		case fi.Mode()&fs.ModeSymlink != 0:
			return nil, nil, f.refuse("write", cur, "a symbolic link; docsync writes only into a directory of the tree itself")
		case !fi.IsDir():
			return nil, nil, f.refuse("write", cur, "not a directory")
		}
		if !exists {
			break
		}
	}
	var sub *os.Root
	if exists {
		if sub, err = f.root.OpenRoot(filepath.FromSlash(dir)); err != nil {
			return nil, nil, f.fail("write", dir, err)
		}
		defer func() { _ = sub.Close() }()
	}
	// Every file to replace or remove: a regular file, or missing.
	var rm []string
	for _, n := range remove {
		if sub == nil {
			continue
		}
		ok, err := f.regular(sub, "remove", dir, n)
		if err != nil {
			return nil, nil, err
		}
		if ok {
			rm = append(rm, n)
		}
	}
	names := make([]string, 0, len(write))
	for n := range write {
		names = append(names, n)
	}
	slices.Sort(names)
	var wr []string
	for _, n := range names {
		if sub != nil {
			ok, err := f.regular(sub, "write", dir, n)
			if err != nil {
				return nil, nil, err
			}
			if ok && same(sub, n, write[n]) {
				continue
			}
		}
		wr = append(wr, n)
	}
	if len(rm) == 0 && len(wr) == 0 {
		return nil, nil, nil
	}
	// The changes.
	if sub == nil {
		cur = ""
		for _, el := range strings.Split(dir, "/") {
			cur = path.Join(cur, el)
			if err := f.root.Mkdir(filepath.FromSlash(cur), 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
				return nil, nil, f.fail("write", cur, err)
			}
			if fi, err := f.root.Lstat(filepath.FromSlash(cur)); err != nil || !fi.IsDir() {
				return nil, nil, f.refuse("write", cur, "not a directory")
			}
		}
		if sub, err = f.root.OpenRoot(filepath.FromSlash(dir)); err != nil {
			return nil, nil, f.fail("write", dir, err)
		}
		defer func() { _ = sub.Close() }()
	}
	for _, n := range rm {
		if err := sub.Remove(filepath.FromSlash(n)); err != nil {
			return wrote, removed, f.fail("remove", path.Join(dir, n), err)
		}
		removed = append(removed, path.Join(dir, n))
	}
	for _, n := range wr {
		if err := replace(sub, n, write[n]); err != nil {
			return wrote, removed, f.fail("write", path.Join(dir, n), err)
		}
		wrote = append(wrote, path.Join(dir, n))
	}
	return wrote, removed, nil
}

// local reports whether the slash-separated name stays inside the
// directory it is relative to.
func local(n string) bool {
	return n != "" && path.Clean(n) == n && !strings.Contains(n, `\`) && filepath.IsLocal(filepath.FromSlash(n))
}

// regular reports whether dir/n is a regular file (false when it is
// missing) and refuses anything else.
func (f *FS) regular(sub *os.Root, op, dir, n string) (bool, error) {
	fi, err := sub.Lstat(filepath.FromSlash(n))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, f.fail(op, path.Join(dir, n), err)
	case !fi.Mode().IsRegular():
		return false, f.refuse(op, path.Join(dir, n), "not a regular file ("+fi.Mode().Type().String()+")")
	}
	return true, nil
}

// same reports whether the file n has exactly the content data.
func same(sub *os.Root, n string, data []byte) bool {
	fh, err := sub.OpenFile(filepath.FromSlash(n), os.O_RDONLY|openFlag, 0)
	if err != nil {
		return false
	}
	defer func() { _ = fh.Close() }()
	old, err := io.ReadAll(io.LimitReader(fh, int64(len(data))+1))
	return err == nil && bytes.Equal(old, data)
}

// replace writes data to a new file next to n and renames it over n: the
// rename replaces the directory entry and follows no link.
func replace(sub *os.Root, n string, data []byte) error {
	tmp := "." + n + ".docsync-" + strconv.Itoa(os.Getpid())
	fh, err := sub.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, err = fh.Write(data)
	if cerr := fh.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = sub.Rename(tmp, n)
	}
	if err != nil {
		_ = sub.Remove(tmp)
	}
	return err
}
