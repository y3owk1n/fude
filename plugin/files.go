package plugin

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tetratelabs/wazero/api"
)

// listDir writes the entries of the folder at the path at (ptr, n) into
// memory the plugin allocates, each name followed by / for a folder, and
// each ended by a NUL byte (B586).
func (p *Plugin) listDir(ctx context.Context, m api.Module, ptr, n uint32) int64 {
	if p.call == nil || !p.allowed("fs.read", "list_dir") {
		return errDenied
	}
	name, ok := m.Memory().Read(ptr, n)
	if !ok {
		return errRange
	}
	at := time.Now()
	path, err := p.inside(string(name))
	var entries []fs.DirEntry
	if err == nil {
		entries, err = os.ReadDir(path)
	}
	p.traced(fmt.Sprintf("%s listed %s, %d entries", p.name, name, len(entries)), at, err)
	if err != nil {
		p.log(fmt.Sprintf("%s: list_dir: %v", p.name, err))
		return errFile
	}
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e.Name())
		if e.IsDir() {
			b.WriteByte('/')
		}
		b.WriteByte(0)
	}
	return give(ctx, m, b.String())
}

// stat writes what is at the path at (ptr, n) into memory the plugin
// allocates: d for a folder or f for a file, its size and when it last
// changed, in nanoseconds since 1970, separated by spaces, or nothing when
// nothing is there (B586).
func (p *Plugin) stat(ctx context.Context, m api.Module, ptr, n uint32) int64 {
	if p.call == nil || !p.allowed("fs.read", "stat") {
		return errDenied
	}
	name, ok := m.Memory().Read(ptr, n)
	if !ok {
		return errRange
	}
	at := time.Now()
	path, err := p.inside(string(name))
	var info fs.FileInfo
	if err == nil {
		info, err = os.Stat(path)
	}
	p.traced(fmt.Sprintf("%s stat %s", p.name, name), at, err)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return give(ctx, m, "")
	case err != nil:
		p.log(fmt.Sprintf("%s: stat: %v", p.name, err))
		return errFile
	}
	kind := "f"
	if info.IsDir() {
		kind = "d"
	}
	return give(ctx, m, kind+" "+strconv.FormatInt(info.Size(), 10)+" "+strconv.FormatInt(info.ModTime().UnixNano(), 10))
}

// writeFile writes the text at (dataPtr, dataLen) to the file at the path
// at (ptr, n), through a file beside it that takes its place (B587).
func (p *Plugin) writeFile(_ context.Context, m api.Module, ptr, n, dataPtr, dataLen uint32) int32 {
	data, ok := m.Memory().Read(dataPtr, dataLen)
	if !ok {
		return errRange
	}
	return p.change(m, "write_file", func(paths []string) error { return write(paths[0], data) }, ptr, n)
}

// makeDir creates the folder at the path at (ptr, n) and the folders above
// it (B587).
func (p *Plugin) makeDir(_ context.Context, m api.Module, ptr, n uint32) int32 {
	return p.change(m, "make_dir", func(paths []string) error { return os.MkdirAll(paths[0], 0o755) }, ptr, n)
}

// remove deletes the file at the path at (ptr, n), and refuses a folder
// (B587).
func (p *Plugin) remove(_ context.Context, m api.Module, ptr, n uint32) int32 {
	return p.change(m, "remove", func(paths []string) error {
		if info, err := os.Lstat(paths[0]); err == nil && info.IsDir() {
			return fmt.Errorf("remove %s: it is a folder", paths[0])
		}
		return os.Remove(paths[0])
	}, ptr, n)
}

// rename moves the file or folder at the path at (ptr, n) to the one at
// (toPtr, toLen), and its open buffer follows (B327, B587).
func (p *Plugin) rename(_ context.Context, m api.Module, ptr, n, toPtr, toLen uint32) int32 {
	return p.change(m, "rename", func(paths []string) error {
		if err := os.Rename(paths[0], paths[1]); err != nil {
			return err
		}
		p.call.Moved = append(p.call.Moved, [2]string{paths[0], paths[1]})
		return nil
	}, ptr, n, toPtr, toLen)
}

// change runs do with where the paths at each (ptr, n) pair are, once all
// are inside the project or the plugin's paths and none is open in a
// buffer with changes not written. It needs fs.write (B587).
func (p *Plugin) change(m api.Module, fn string, do func(paths []string) error, at ...uint32) int32 {
	if p.call == nil || !p.allowed("fs.write", fn) {
		return errDenied
	}
	var paths []string
	for i := 0; i < len(at); i += 2 {
		name, ok := m.Memory().Read(at[i], at[i+1])
		if !ok {
			return errRange
		}
		path, err := p.writable(string(name))
		if err != nil {
			p.log(fmt.Sprintf("%s: %s: %v", p.name, fn, err))
			return errFile
		}
		paths = append(paths, path)
	}
	start := time.Now()
	err := do(paths)
	p.traced(fmt.Sprintf("%s ran %s on %s", p.name, fn, strings.Join(paths, " ")), start, err)
	if err != nil {
		p.log(fmt.Sprintf("%s: %s: %v", p.name, fn, err))
		return errFile
	}
	return 0
}

// writable returns where the file at name is, if the plugin may change it:
// inside the project or its paths, and open in no buffer with changes not
// written (B587).
func (p *Plugin) writable(name string) (string, error) {
	path, err := p.inside(name)
	if err != nil {
		return "", err
	}
	real, err := resolve(path)
	if err != nil {
		return "", fmt.Errorf("find %s: %w", name, err)
	}
	// A buffer's path may be relative, or reach the file through a link.
	for _, b := range p.call.Buffers {
		if !b.Modified || b.Path == "" {
			continue
		}
		open, err := p.inside(b.Path)
		if err != nil {
			continue
		}
		if open, err = resolve(open); err == nil && open == real {
			return "", fmt.Errorf("%s is open with changes not written", name)
		}
	}
	return path, nil
}

// write writes data to the file at path through a file beside it, which
// takes its place, so a failed write leaves the file as it was. The file
// keeps its mode.
func write(path string, data []byte) error {
	mode := fs.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	_, err = tmp.Write(data)
	if err = errors.Join(err, tmp.Chmod(mode), tmp.Close()); err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		return errors.Join(fmt.Errorf("write %s: %w", path, err), os.Remove(tmp.Name()))
	}
	return nil
}
