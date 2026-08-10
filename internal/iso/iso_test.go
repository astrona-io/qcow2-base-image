package iso

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/kdomanski/iso9660"
)

func TestWriteCIDataRoundTrip(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "cidata.iso")

	userData := []byte("#cloud-config\nusers:\n  - name: ubuntu\n")
	metaData := []byte("instance-id: test-1\nlocal-hostname: test\n")

	if err := WriteCIData(outPath, userData, metaData); err != nil {
		t.Fatalf("WriteCIData: %v", err)
	}

	img := openTestImage(t, outPath)

	label, err := img.Label()
	if err != nil {
		t.Fatalf("Label: %v", err)
	}

	if label != "cidata" {
		t.Errorf("label = %q, want %q", label, "cidata")
	}

	files := readISOFiles(t, img)

	assertFileContent(t, files, "user-data", userData)
	assertFileContent(t, files, "meta-data", metaData)
}

func openTestImage(t *testing.T, outPath string) *iso9660.Image {
	t.Helper()

	f, err := os.Open(outPath) //nolint:gosec // outPath is a t.TempDir()-derived test path
	if err != nil {
		t.Fatalf("opening written iso: %v", err)
	}

	t.Cleanup(func() { _ = f.Close() })

	img, err := iso9660.OpenImage(f)
	if err != nil {
		t.Fatalf("OpenImage: %v", err)
	}

	return img
}

func readISOFiles(t *testing.T, img *iso9660.Image) map[string][]byte {
	t.Helper()

	root, err := img.RootDir()
	if err != nil {
		t.Fatalf("RootDir: %v", err)
	}

	children, err := root.GetAllChildren()
	if err != nil {
		t.Fatalf("GetAllChildren: %v", err)
	}

	got := map[string][]byte{}

	for _, c := range children {
		if c.IsDir() {
			continue
		}

		data, err := io.ReadAll(c.Reader())
		if err != nil {
			t.Fatalf("reading %s: %v", c.Name(), err)
		}

		got[c.Name()] = data
	}

	return got
}

func assertFileContent(t *testing.T, files map[string][]byte, name string, want []byte) {
	t.Helper()

	got, ok := files[name]
	if !ok {
		t.Fatalf("%s not found in iso, entries: %v", name, keys(files))
	}

	if string(got) != string(want) {
		t.Errorf("%s content mismatch: got %q, want %q", name, got, want)
	}
}

func keys(m map[string][]byte) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}

	return ks
}
