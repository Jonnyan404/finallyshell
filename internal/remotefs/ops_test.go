package remotefs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRename(t *testing.T) {
	c := newTestClient(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), []byte("a"))
	writeFile(t, filepath.Join(dir, "b.txt"), []byte("b"))

	np, err := Rename(c, filepath.Join(dir, "a.txt"), "c.txt")
	if err != nil || np != filepath.Join(dir, "c.txt") {
		t.Fatalf("rename: %q %v", np, err)
	}
	if got, _ := os.ReadFile(np); string(got) != "a" {
		t.Errorf("c.txt holds %q", got)
	}

	// A taken name is refused, and both files stay as they were.
	_, err = Rename(c, filepath.Join(dir, "c.txt"), "b.txt")
	var exists *ExistsError
	if !errors.As(err, &exists) || exists.Name != "b.txt" {
		t.Fatalf("rename onto b.txt: %v", err)
	}
	for name, want := range map[string]string{"b.txt": "b", "c.txt": "a"} {
		if got, _ := os.ReadFile(filepath.Join(dir, name)); string(got) != want {
			t.Errorf("%s holds %q", name, got)
		}
	}

	for _, bad := range []string{"", ".", "..", "x/y", "nul\x00"} {
		if _, err := Rename(c, filepath.Join(dir, "c.txt"), bad); !errors.Is(err, ErrBadName) {
			t.Errorf("rename to %q: %v", bad, err)
		}
	}
	// The same name is nothing to do.
	if np, err := Rename(c, filepath.Join(dir, "c.txt"), "c.txt"); err != nil || np != filepath.Join(dir, "c.txt") {
		t.Errorf("same name: %q %v", np, err)
	}
}

func TestMkdirCreateFile(t *testing.T) {
	c := newTestClient(t)
	dir := t.TempDir()
	p, err := Mkdir(c, dir, "logs")
	if fi, serr := os.Stat(p); err != nil || serr != nil || !fi.IsDir() {
		t.Fatalf("mkdir: %v %v", err, serr)
	}
	if _, err := Mkdir(c, dir, "logs"); !errors.As(err, new(*ExistsError)) {
		t.Errorf("mkdir again: %v", err)
	}

	p, err = CreateFile(c, dir, "notes.md")
	if fi, serr := os.Stat(p); err != nil || serr != nil || fi.Size() != 0 || fi.IsDir() {
		t.Fatalf("create: %v %v", err, serr)
	}
	// An existing file is never truncated.
	writeFile(t, p, []byte("keep me"))
	if _, err := CreateFile(c, dir, "notes.md"); !errors.As(err, new(*ExistsError)) {
		t.Errorf("create again: %v", err)
	}
	if got, _ := os.ReadFile(p); string(got) != "keep me" {
		t.Errorf("notes.md holds %q", got)
	}
	if _, err := CreateFile(c, dir, "a/b"); !errors.Is(err, ErrBadName) {
		t.Errorf("create a/b: %v", err)
	}
}

func TestRemoveAll(t *testing.T) {
	c := newTestClient(t)
	dir := t.TempDir()
	// A deep tree with more files in a folder than workers.
	for i := range 40 {
		writeFile(t, filepath.Join(dir, "app", "node_modules", fmt.Sprintf("m%d", i%4), fmt.Sprintf("f%d.js", i)), []byte("x"))
	}
	os.MkdirAll(filepath.Join(dir, "app", "empty", "deeper"), 0o755)
	// A link to a folder outside: the link goes, what it points to stays.
	writeFile(t, filepath.Join(dir, "keep", "data.txt"), []byte("data"))
	if err := os.Symlink(filepath.Join(dir, "keep"), filepath.Join(dir, "app", "keep-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "keep"), filepath.Join(dir, "top-link")); err != nil {
		t.Fatal(err)
	}

	if err := RemoveAll(context.Background(), c, filepath.Join(dir, "app")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "app")); !os.IsNotExist(err) {
		t.Errorf("app is still there: %v", err)
	}
	if err := RemoveAll(context.Background(), c, filepath.Join(dir, "top-link")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "top-link")); !os.IsNotExist(err) {
		t.Errorf("top-link is still there: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "keep", "data.txt")); err != nil || string(got) != "data" {
		t.Errorf("the folder a link pointed to lost data.txt: %q %v", got, err)
	}

	// A file, and a path that isn't there.
	if err := RemoveAll(context.Background(), c, filepath.Join(dir, "keep", "data.txt")); err != nil {
		t.Fatal(err)
	}
	if err := RemoveAll(context.Background(), c, filepath.Join(dir, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing: %v", err)
	}

	// A canceled delete removes nothing, a file as a folder.
	writeFile(t, filepath.Join(dir, "kept.txt"), []byte("x"))
	done, stop := context.WithCancel(context.Background())
	stop()
	if err := RemoveAll(done, c, filepath.Join(dir, "kept.txt")); !IsCanceled(err) {
		t.Errorf("canceled file: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "kept.txt")); err != nil {
		t.Errorf("a canceled delete removed kept.txt: %v", err)
	}

	// A canceled delete stops.
	writeFile(t, filepath.Join(dir, "big", "a", "f"), []byte("x"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := RemoveAll(ctx, c, filepath.Join(dir, "big")); !IsCanceled(err) {
		t.Errorf("canceled: %v", err)
	}
}
