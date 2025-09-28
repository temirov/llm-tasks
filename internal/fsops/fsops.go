package fsops

import (
	"io/fs"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
)

// FS is an abstract filesystem used across the app and tests.
type FS interface {
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte, perm os.FileMode) error
	Stat(name string) (fs.FileInfo, error)
	Rename(oldpath, newpath string) error
	MkdirAll(path string, perm os.FileMode) error
	WalkDir(root string, fn fs.WalkDirFunc) error

	Join(elem ...string) string
	Base(name string) string
	Dir(name string) string
	Ext(name string) string
	Clean(name string) string
}

// ---------- OS-backed implementation ----------

type OS struct{}

func NewOS() OS { return OS{} }

func (OS) ReadFile(name string) ([]byte, error) { return os.ReadFile(filepath.Clean(name)) }
func (OS) WriteFile(name string, b []byte, p os.FileMode) error {
	return os.WriteFile(filepath.Clean(name), b, p)
}
func (OS) Stat(name string) (fs.FileInfo, error)     { return os.Stat(filepath.Clean(name)) }
func (OS) Rename(a, b string) error                  { return os.Rename(a, b) }
func (OS) MkdirAll(path string, p os.FileMode) error { return os.MkdirAll(filepath.Clean(path), p) }
func (OS) WalkDir(root string, fn fs.WalkDirFunc) error {
	return filepath.WalkDir(filepath.Clean(root), fn)
}
func (OS) Join(elem ...string) string { return filepath.Join(elem...) }
func (OS) Base(name string) string    { return filepath.Base(name) }
func (OS) Dir(name string) string     { return filepath.Dir(name) }
func (OS) Ext(name string) string     { return filepath.Ext(name) }
func (OS) Clean(name string) string   { return filepath.Clean(name) }

// ---------- In-memory implementation (for tests/integration) ----------

type Mem struct{ Fs afero.Fs }

func NewMem() Mem { return Mem{Fs: afero.NewMemMapFs()} }

func (m Mem) ReadFile(name string) ([]byte, error) { return afero.ReadFile(m.Fs, filepath.Clean(name)) }
func (m Mem) WriteFile(name string, b []byte, p os.FileMode) error {
	return afero.WriteFile(m.Fs, filepath.Clean(name), b, p)
}
func (m Mem) Stat(name string) (fs.FileInfo, error) { return m.Fs.Stat(filepath.Clean(name)) }
func (m Mem) Rename(a, b string) error              { return m.Fs.Rename(a, b) }
func (m Mem) MkdirAll(path string, p os.FileMode) error {
	return m.Fs.MkdirAll(filepath.Clean(path), p)
}
func (m Mem) WalkDir(root string, fn fs.WalkDirFunc) error {
	root = filepath.Clean(root)
	return afero.Walk(m.Fs, root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		de := memDirEntry{info}
		return fn(p, de, nil)
	})
}

type memDirEntry struct{ os.FileInfo }

func (d memDirEntry) Type() fs.FileMode          { return d.Mode().Type() }
func (d memDirEntry) Info() (fs.FileInfo, error) { return d.FileInfo, nil }

func (Mem) Join(elem ...string) string { return filepath.Join(elem...) }
func (Mem) Base(name string) string    { return filepath.Base(name) }
func (Mem) Dir(name string) string     { return filepath.Dir(name) }
func (Mem) Ext(name string) string     { return filepath.Ext(name) }
func (Mem) Clean(name string) string   { return filepath.Clean(name) }

// ---------- High-level façade used by tasks ----------

type Ops struct{ FS FS }

func NewOps(fs FS) Ops { return Ops{FS: fs} }

type FileInfo struct {
	AbsolutePath string
	BaseName     string
	Extension    string
	MIMEType     string
	SizeBytes    int64
}

// Inventory walks a root directory and returns basic file metadata.
// Skips "_sorted" and dot-directories.
func (o Ops) Inventory(root string) ([]FileInfo, error) {
	var out []FileInfo
	err := o.FS.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == "_sorted" || strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil {
			return statErr
		}
		ext := strings.ToLower(filepath.Ext(p))
		base := strings.TrimSuffix(filepath.Base(p), ext)

		m := mime.TypeByExtension(ext)
		if m == "" {
			switch ext {
			case ".3mf":
				m = "application/zip"
			case ".stl", ".obj", ".mtl":
				m = "application/octet-stream"
			case ".csv", ".txt", ".md", ".json":
				m = "text/plain; charset=utf-8"
			default:
				m = "application/octet-stream"
			}
		}
		out = append(out, FileInfo{
			AbsolutePath: p,
			BaseName:     base,
			Extension:    ext,
			MIMEType:     m,
			SizeBytes:    info.Size(),
		})
		return nil
	})
	return out, err
}

func (o Ops) EnsureDir(path string) error    { return o.FS.MkdirAll(filepath.Dir(path), 0o755) }
func (o Ops) MoveFile(from, to string) error { return o.FS.Rename(from, to) }
func (o Ops) FileExists(p string) bool       { _, err := o.FS.Stat(p); return err == nil }
