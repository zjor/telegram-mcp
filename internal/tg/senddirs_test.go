package tg

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func sdSetup(t *testing.T) (root, allowed, other string) {
	t.Helper()
	root = t.TempDir()
	allowed = filepath.Join(root, "allowed")
	other = filepath.Join(root, "other")
	for _, d := range []string{allowed, other, filepath.Join(allowed, "sub")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{filepath.Join(allowed, "a.txt"), filepath.Join(other, "a.txt")} {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return
}

func TestSendDirs_Inside(t *testing.T) {
	_, allowed, _ := sdSetup(t)
	d := NewSendDirs([]string{allowed}, nil)
	p := filepath.Join(allowed, "a.txt")
	f, resolved, err := d.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	want, _ := filepath.EvalSymlinks(p)
	if resolved != want {
		t.Errorf("resolved = %q, want %q", resolved, want)
	}
}

func TestSendDirs_Outside(t *testing.T) {
	_, allowed, other := sdSetup(t)
	d := NewSendDirs([]string{allowed}, nil)
	if _, _, err := d.Open(filepath.Join(other, "a.txt")); !errors.Is(err, ErrOutsideSendDirs) {
		t.Errorf("err = %v", err)
	}
}

func TestSendDirs_Missing(t *testing.T) {
	_, allowed, other := sdSetup(t)
	d := NewSendDirs([]string{allowed}, nil)
	if _, _, err := d.Open(filepath.Join(other, "none.txt")); !errors.Is(err, ErrOutsideSendDirs) {
		t.Errorf("err = %v", err)
	}
	if _, _, err := d.Open(filepath.Join(allowed, "none.txt")); !errors.Is(err, ErrOutsideSendDirs) {
		t.Errorf("missing inside: err = %v", err)
	}
}

func TestSendDirs_DotDot(t *testing.T) {
	_, allowed, _ := sdSetup(t)
	d := NewSendDirs([]string{allowed}, nil)
	if _, _, err := d.Open(allowed + "/../other/a.txt"); !errors.Is(err, ErrOutsideSendDirs) {
		t.Errorf("err = %v", err)
	}
}

func TestSendDirs_DotDotNoExistenceLeak(t *testing.T) {
	root, allowed, _ := sdSetup(t) // root/other exists, root/nodir does not
	d := NewSendDirs([]string{allowed}, nil)
	var results []string
	for _, dir := range []string{"other", "nodir"} {
		f, resolved, err := d.Open(allowed + "/../" + dir + "/../allowed/a.txt")
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		f.Close()
		results = append(results, resolved)
	}
	if results[0] != results[1] {
		t.Errorf("results differ: %v", results)
	}
	_ = root
}

func TestSendDirs_SymlinkOut(t *testing.T) {
	_, allowed, other := sdSetup(t)
	link := filepath.Join(allowed, "link")
	if err := os.Symlink(filepath.Join(other, "a.txt"), link); err != nil {
		t.Fatal(err)
	}
	d := NewSendDirs([]string{allowed}, nil)
	if _, _, err := d.Open(link); !errors.Is(err, ErrOutsideSendDirs) {
		t.Errorf("err = %v", err)
	}
}

func TestSendDirs_PrefixTrap(t *testing.T) {
	root := t.TempDir()
	b := filepath.Join(root, "a", "b")
	bc := filepath.Join(root, "a", "bc")
	for _, d := range []string{b, bc} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(bc, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := NewSendDirs([]string{b}, nil)
	if _, _, err := d.Open(filepath.Join(bc, "x")); !errors.Is(err, ErrOutsideSendDirs) {
		t.Errorf("err = %v", err)
	}
}

func TestSendDirs_AllowedDirIsSymlink(t *testing.T) {
	root, allowed, _ := sdSetup(t)
	linkDir := filepath.Join(root, "linkdir")
	if err := os.Symlink(allowed, linkDir); err != nil {
		t.Fatal(err)
	}
	d := NewSendDirs([]string{linkDir}, nil)
	for _, p := range []string{filepath.Join(linkDir, "a.txt"), filepath.Join(allowed, "a.txt")} {
		f, _, err := d.Open(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		f.Close()
	}
}

func TestSendDirs_Directory(t *testing.T) {
	_, allowed, _ := sdSetup(t)
	d := NewSendDirs([]string{allowed}, nil)
	if _, _, err := d.Open(filepath.Join(allowed, "sub") + "/"); !errors.Is(err, ErrNotRegular) {
		t.Errorf("err = %v", err)
	}
}

func TestSendDirs_MissingDirWarned(t *testing.T) {
	root := t.TempDir()
	n := 0
	NewSendDirs([]string{filepath.Join(root, "none")}, func(string, error) { n++ })
	if n != 1 {
		t.Errorf("warn calls = %d", n)
	}
}
