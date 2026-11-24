package ui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// saveDownload stages the transfer in the destination directory. Linking the
// finished file publishes it atomically and never replaces an existing name.
func saveDownload(dir, name string, write func(io.Writer) error) (string, error) {
	if !filepath.IsLocal(name) || name == "." || filepath.Base(name) != name {
		return "", fmt.Errorf("invalid download name %q", name)
	}
	if dir == "" {
		dir = os.TempDir()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, ".teams-download-*")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := write(f); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 0; ; i++ {
		candidate := name
		if i > 0 {
			candidate = fmt.Sprintf("%s (%d)%s", stem, i, ext)
		}
		dest := filepath.Join(dir, candidate)
		err := os.Link(tmp, dest)
		if err == nil {
			return dest, nil
		}
		if errors.Is(err, os.ErrExist) {
			continue
		}
		// Filesystems without hard links can still publish by renaming over a
		// name reserved exclusively after the transfer has finished.
		reservation, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if err = reservation.Close(); err == nil {
			err = os.Rename(tmp, dest)
		}
		if err != nil {
			os.Remove(dest)
			return "", err
		}
		return dest, nil
	}
}
