//go:build unix

// Package safefile reads regular files with a size limit and without blocking.
package safefile

import (
	"fmt"
	"io"
	"os"
	"syscall"
)

// Read opens path once, checks the open descriptor, and reads at most limit
// bytes from it. It refuses anything that is not a regular file. O_NONBLOCK
// stops a FIFO or a device from blocking the open. When the file is larger
// than limit, truncated is true and data holds the first limit bytes.
func Read(path string, limit int64) (data []byte, truncated bool, err error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	if !st.Mode().IsRegular() {
		return nil, false, fmt.Errorf("%s is not a regular file", path)
	}
	data, err = io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > limit {
		return data[:limit], true, nil
	}
	return data, false, nil
}

// ReadAll is Read, but a file larger than limit is an error.
func ReadAll(path string, limit int64) ([]byte, error) {
	data, truncated, err := Read(path, limit)
	if err != nil {
		return nil, err
	}
	if truncated {
		return nil, fmt.Errorf("%s is larger than %d bytes", path, limit)
	}
	return data, nil
}
