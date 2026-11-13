//go:build linux

// Package storagecontrol measures fixed, operator-bound filesystem metadata.
// It never opens database files, enumerates directories, or accepts user URLs.
package storagecontrol

import (
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"math"
	"os"
	"strings"
)

var ErrUnavailable = errors.New("storage metadata unavailable")

type Binding struct {
	Path   string `json:"path"`
	Device string `json:"device"`
	FSID   string `json:"fsid"`
}
type Filesystem struct {
	Device     string `json:"device"`
	FSID       string `json:"fsid"`
	Total      int64  `json:"total"`
	Used       int64  `json:"used"`
	Available  int64  `json:"available"`
	Inodes     int64  `json:"inodes"`
	FreeInodes int64  `json:"free_inodes"`
}
type Sampler struct {
	files    []*os.File
	bindings []Binding
}

func Open(bindings []Binding) (*Sampler, error) {
	if len(bindings) < 1 || len(bindings) > 4 {
		return nil, ErrUnavailable
	}
	s := &Sampler{bindings: bindings}
	seen := map[string]bool{}
	for _, b := range bindings {
		if !strings.HasPrefix(b.Path, "/") || b.Device == "" || b.FSID == "" || seen[b.Device] {
			s.Close()
			return nil, ErrUnavailable
		}
		seen[b.Device] = true
		fd, e := unix.Open(b.Path, unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if e != nil {
			s.Close()
			return nil, ErrUnavailable
		}
		s.files = append(s.files, os.NewFile(uintptr(fd), "storage-metadata"))
	}
	if _, e := s.Collect(); e != nil {
		s.Close()
		return nil, e
	}
	return s, nil
}
func (s *Sampler) Close() {
	if s != nil {
		for _, f := range s.files {
			f.Close()
		}
	}
}
func identity(st *unix.Stat_t, fs *unix.Statfs_t) (string, string) {
	return fmt.Sprintf("%d:%d", unix.Major(uint64(st.Dev)), unix.Minor(uint64(st.Dev))), fmt.Sprintf("%08x:%08x", uint32(fs.Fsid.Val[0]), uint32(fs.Fsid.Val[1]))
}

// Identify is only an offline operator/bootstrap helper, never registration.
func Identify(path string) (Binding, error) {
	fd, e := unix.Open(path, unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return Binding{}, ErrUnavailable
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	var fs unix.Statfs_t
	if unix.Fstat(fd, &st) != nil || unix.Fstatfs(fd, &fs) != nil {
		return Binding{}, ErrUnavailable
	}
	d, id := identity(&st, &fs)
	return Binding{path, d, id}, nil
}
func bytes(n uint64, size int64) (int64, bool) {
	if size <= 0 || n > uint64(math.MaxInt64)/uint64(size) {
		return 0, false
	}
	return int64(n) * size, true
}
func (s *Sampler) Collect() ([]Filesystem, error) {
	if s == nil || len(s.files) != len(s.bindings) {
		return nil, ErrUnavailable
	}
	out := make([]Filesystem, 0, len(s.files))
	for i, f := range s.files {
		var st unix.Stat_t
		var fs unix.Statfs_t
		if unix.Fstat(int(f.Fd()), &st) != nil || unix.Fstatfs(int(f.Fd()), &fs) != nil {
			return nil, ErrUnavailable
		}
		dev, id := identity(&st, &fs)
		b := s.bindings[i]
		if dev != b.Device || id != b.FSID || fs.Bfree > fs.Blocks || fs.Bavail > fs.Bfree || fs.Ffree > fs.Files || fs.Files > math.MaxInt64 {
			return nil, ErrUnavailable
		}
		total, a := bytes(fs.Blocks, fs.Frsize)
		used, c := bytes(fs.Blocks-fs.Bfree, fs.Frsize)
		avail, d := bytes(fs.Bavail, fs.Frsize)
		if !a || !c || !d || total <= 0 || fs.Files == 0 {
			return nil, ErrUnavailable
		}
		out = append(out, Filesystem{dev, id, total, used, avail, int64(fs.Files), int64(fs.Ffree)})
	}
	return out, nil
}
