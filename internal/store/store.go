// Package store holds the file primitives every state writer shares: an mkdir lock, an atomic write and a JSONL append.
package store

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/proc"
)

// Thresholds copied from sdkit_lock in scripts/env.sh: after 3 s a lock whose owner is gone is broken, after 10 s any lock is.
const (
	deadAfter  = 3 * time.Second
	stuckAfter = 10 * time.Second
	maxWait    = 30 * time.Second
	poll       = 10 * time.Millisecond
)

// Lock takes <path>.lock (a directory made by os.Mkdir, which is atomic) and returns the function that releases it.
func Lock(path string) (func(), error) {
	l := path + ".lock"
	if err := os.MkdirAll(filepath.Dir(l), 0o755); err != nil {
		return nil, err
	}
	start := time.Now()
	for {
		err := os.Mkdir(l, 0o755)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if st, e := os.Stat(l); e == nil {
			age := time.Since(st.ModTime())
			if age >= stuckAfter || (age >= deadAfter && ownerGone(l)) {
				os.RemoveAll(l)
				continue
			}
		}
		if time.Since(start) > maxWait {
			return nil, errors.New("store: timed out waiting for lock " + l)
		}
		time.Sleep(poll)
	}
	os.WriteFile(filepath.Join(l, "pid"), []byte(strconv.Itoa(os.Getpid())), 0o644)
	return func() { os.RemoveAll(l) }, nil
}

func ownerGone(l string) bool {
	b, err := os.ReadFile(filepath.Join(l, "pid"))
	pid, e := strconv.Atoi(strings.TrimSpace(string(b)))
	return err == nil && e == nil && pid > 0 && !proc.Alive(pid)
}

// Update runs fn on the current content of path (nil when missing) under the lock and writes its result atomically.
func Update(path string, fn func(old []byte) ([]byte, error)) error {
	unlock, err := Lock(path)
	if err != nil {
		return err
	}
	defer unlock()
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	out, err := fn(old)
	if err != nil {
		return err
	}
	return WriteAtomic(path, out)
}

// WriteAtomic writes data to a temp file in the same directory and renames it over path (retrying up to 1 s on Windows).
func WriteAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if e := f.Close(); err == nil {
		err = e
	}
	if err == nil {
		err = rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}

func rename(from, to string) error {
	deadline := time.Now().Add(time.Second)
	for {
		err := os.Rename(from, to)
		if err == nil || runtime.GOOS != "windows" || time.Now().After(deadline) {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// AppendLine appends line plus a newline in a single O_APPEND write.
func AppendLine(path, line string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = f.WriteString(strings.TrimSuffix(line, "\n") + "\n")
	if e := f.Close(); err == nil {
		err = e
	}
	return err
}
