package ui

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestDownloadPublication(t *testing.T) {
	dir := t.TempDir()
	write := func(content string) func(io.Writer) error {
		return func(w io.Writer) error { _, err := io.WriteString(w, content); return err }
	}
	var wg sync.WaitGroup
	for _, body := range []string{"one", "two"} {
		wg.Go(func() {
			dest, err := saveDownload(dir, "file.txt", write(body))
			if err != nil {
				t.Error(err)
				return
			}
			b, err := os.ReadFile(dest)
			if err != nil || string(b) != body {
				t.Errorf("download content %q, %v", b, err)
			}
		})
	}
	wg.Wait()
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("collision: %v %v", entries, err)
	}
	_, err = saveDownload(dir, "partial.txt", func(p io.Writer) error {
		if err := write("partial")(p); err != nil {
			return err
		}
		return errors.New("interrupted")
	})
	if err == nil {
		t.Fatal("interruption was lost")
	}
	entries, _ = os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatal("left partial file")
	}
	if _, err := saveDownload(filepath.Join(dir, "file.txt", "sub"), "x", write("")); err == nil {
		t.Fatal("invalid directory accepted")
	}
}
