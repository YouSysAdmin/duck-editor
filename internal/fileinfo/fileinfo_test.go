package fileinfo

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDescribe(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}

	img := filepath.Join(dir, "a.png")
	if err := os.WriteFile(img, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	if got, err := Describe(img); err != nil || got != "PNG image, 3 x 2" {
		t.Fatalf("png = %q %v", got, err)
	}

	// The test binary itself is an executable of this platform.
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	got, err := Describe(exe)
	if err != nil || !(strings.HasPrefix(got, "ELF ") || strings.HasPrefix(got, "Mach-O ") || strings.HasPrefix(got, "PE ")) {
		t.Fatalf("executable = %q %v", got, err)
	}

	gz := filepath.Join(dir, "a.gz")
	if err := os.WriteFile(gz, []byte{0x1f, 0x8b, 8, 0, 0, 0, 0, 0}, 0o644); err != nil {
		t.Fatal(err)
	}

	if got, _ := Describe(gz); got != "application/x-gzip" {
		t.Fatalf("gzip = %q", got)
	}
}
