// Package afero2billy adapts an Afero filesystem to a billy filesystem
package afero2billy

import (
	"os"
	"path"
	"path/filepath"
	"sync"

	"github.com/go-git/go-billy/v5"
	"github.com/spf13/afero"
)

const (
	defaultDirectoryMode = 0o755
	defaultCreateMode    = 0o666
)

// Billy implements a go-billy filesystem backed by an Afero filesystem
type Billy struct {
	afero afero.Afero
	root  string
}

// New returns a billy filesystem backed by an input afero filesystem.
func New(fs afero.Fs) billy.Filesystem {
	return &Billy{
		root: filepath.FromSlash("/"),
		afero: afero.Afero{
			Fs: fs,
		},
	}
}

// Create creates the named file with mode 0666 (before umask), truncating
// it if it already exists. If successful, methods on the returned File can
// be used for I/O; the associated file descriptor has mode O_RDWR.
func (fs *Billy) Create(filename string) (billy.File, error) {
	return fs.OpenFile(filename, os.O_RDWR|os.O_CREATE|os.O_TRUNC, defaultCreateMode)
}

// Open opens the named file for reading. If successful, methods on the
// returned file can be used for reading; the associated file descriptor has
// mode O_RDONLY.
func (fs *Billy) Open(filename string) (billy.File, error) {
	return fs.OpenFile(filename, os.O_RDONLY, 0)
}

// OpenFile is the generalized open call; most users will use Open or Create
// instead. It opens the named file with specified flag (O_RDONLY etc.) and
// perm, (0666 etc.) if applicable. If successful, methods on the returned
// File can be used for I/O.
func (fs *Billy) OpenFile(filename string, flag int, perm os.FileMode) (billy.File, error) {
	if flag&os.O_CREATE != 0 {
		if err := fs.createDir(filename); err != nil {
			return nil, err
		}
	}

	f, err := fs.afero.Fs.OpenFile(filename, flag, perm)
	if err != nil {
		return nil, err
	}

	return &file{File: f}, err
}

// Stat returns a FileInfo describing the named file.
func (fs *Billy) Stat(filename string) (os.FileInfo, error) {
	return fs.afero.Fs.Stat(filename)
}

// Rename renames (moves) oldpath to newpath. If newpath already exists and
// is not a directory, Rename replaces it. OS-specific restrictions may
// apply when oldpath and newpath are in different directories.
func (fs *Billy) Rename(oldpath, newpath string) error {
	if err := fs.createDir(newpath); err != nil {
		return err
	}
	return fs.afero.Fs.Rename(oldpath, newpath)
}

// Remove removes the named file or directory.
func (fs *Billy) Remove(filename string) error {
	return fs.afero.Fs.Remove(filename)
}

// Join joins any number of path elements into a single path, adding a
// Separator if necessary. Join calls filepath.Clean on the result; in
// particular, all empty strings are ignored. On Windows, the result is a
// UNC path if and only if the first path element is a UNC path.
func (fs *Billy) Join(elem ...string) string {
	return path.Join(elem...)
}

// TempFile creates a new temporary file in the directory dir with a name
// beginning with prefix, opens the file for reading and writing, and
// returns the resulting *os.File. If dir is the empty string, TempFile
// uses the default directory for temporary files (see os.TempDir).
// Multiple programs calling TempFile simultaneously will not choose the
// same file. The caller can use f.Name() to find the pathname of the file.
// It is the caller's responsibility to remove the file when no longer
// needed.
func (fs *Billy) TempFile(dir, prefix string) (billy.File, error) {
	if err := fs.createDir(dir + string(os.PathSeparator)); err != nil {
		return nil, err
	}

	f, err := fs.afero.TempFile(dir, prefix)
	if err != nil {
		return nil, err
	}

	return &file{File: f}, nil
}

// ReadDir reads the directory named by dirname and returns a list of
// directory entries sorted by filename.
func (fs *Billy) ReadDir(path string) ([]os.FileInfo, error) {
	return fs.afero.ReadDir(path)
}

// MkdirAll creates a directory named path, along with any necessary
// parents, and returns nil, or else returns an error. The permission bits
// perm are used for all directories that MkdirAll creates. If path is/
// already a directory, MkdirAll does nothing and returns nil.
func (fs *Billy) MkdirAll(path string, perm os.FileMode) error {
	return fs.afero.Fs.MkdirAll(path, perm)
}

// Lstat returns a FileInfo describing the named file. If the file is a
// symbolic link, the returned FileInfo describes the symbolic link. Lstat
// makes no attempt to follow the link.
func (fs *Billy) Lstat(filename string) (os.FileInfo, error) {
	if lstater, ok := fs.afero.Fs.(afero.Lstater); ok {
		fi, _, err := lstater.LstatIfPossible(filename)
		return fi, err
	}
	return fs.Stat(path.Clean(filename))
}

// Symlink creates a symbolic-link from link to target. target may be an
// absolute or relative path, and need not refer to an existing node.
// Parent directories of link are created as necessary.
func (fs *Billy) Symlink(target, link string) error {
	parentDir := path.Dir(link)
	if err := fs.MkdirAll(parentDir, defaultDirectoryMode); err != nil {
		return err
	}

	if linker, ok := fs.afero.Fs.(afero.Linker); ok {
		return linker.SymlinkIfPossible(target, link)
	}

	return &os.LinkError{Op: "symlink", Old: target, New: link, Err: afero.ErrNoSymlink}
}

// Readlink returns the target path of link.
func (fs *Billy) Readlink(link string) (string, error) {
	if reader, ok := fs.afero.Fs.(afero.LinkReader); ok {
		return reader.ReadlinkIfPossible(link)
	}

	return "", &os.PathError{Op: "readlink", Path: link, Err: afero.ErrNoReadlink}
}

// Chroot returns a new filesystem from the same type where the new root is
// the given path. Files outside of the designated directory tree cannot be
// accessed.
func (fs *Billy) Chroot(basePath string) (billy.Filesystem, error) {
	return &Billy{
		root: basePath,
		afero: afero.Afero{
			Fs: afero.NewBasePathFs(fs.afero.Fs, basePath),
		},
	}, nil
}

// Root returns the root path of the filesystem.
func (fs *Billy) Root() string {
	return fs.root
}

// RemoveAll removes a directory path and any children it contains. It
// does not fail if the path does not exist (return nil).
func (fs *Billy) RemoveAll(filePath string) error {
	return fs.afero.Fs.RemoveAll(path.Clean(filePath))
}

// Capabilities implements the Capable interface.
func (fs *Billy) Capabilities() billy.Capability {
	return billy.DefaultCapabilities
}

// file is a wrapper for an os.File which adds support for file locking.
type file struct {
	afero.File
	m sync.Mutex
}

//Lock locks the file like e.g. flock. It protects against access from
// other processes.
func (f *file) Lock() error {
	f.m.Lock()
	return nil
}

// Unlock unlocks the file.
func (f *file) Unlock() error {
	f.m.Unlock()
	return nil
}

// createDir was copied from https://github.com/go-git/go-billy/blame/v5.0.0/osfs/os.go#L45
func (fs *Billy) createDir(fullpath string) error {
	dir := filepath.Dir(fullpath)
	if dir != "." {
		if err := fs.afero.Fs.MkdirAll(dir, defaultDirectoryMode); err != nil {
			return err
		}
	}

	return nil
}
