package imagefetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadWritesFileAtomically(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("fake image bytes"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "image.img")

	if err := Download(context.Background(), srv.Client(), srv.URL, dest); err != nil {
		t.Fatalf("Download: %v", err)
	}

	data, err := os.ReadFile(dest) //nolint:gosec // dest is a t.TempDir()-derived test path
	if err != nil {
		t.Fatalf("reading downloaded file: %v", err)
	}

	if string(data) != "fake image bytes" {
		t.Errorf("got %q, want %q", data, "fake image bytes")
	}

	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		t.Errorf(".part file should not remain after successful download")
	}
}

func TestDownloadNon200Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "image.img")

	if err := Download(context.Background(), srv.Client(), srv.URL, dest); err == nil {
		t.Fatal("expected error for 404 response")
	}

	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Error("destination file should not exist after failed download")
	}
}

func TestParseSHA256Sums(t *testing.T) {
	doc := []byte(
		"abc123def456  noble-server-cloudimg-amd64.img\n" +
			"deadbeef00 *noble-server-cloudimg-arm64.img\n" +
			"cafef00d  ./noble-desktop.iso\n" +
			"SHA256 (Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2) = 28680fe5b371a5a82ebf43a31926e086a168e59949d03969c5093e7071f90b7f\n" +
			"SHA256 (./Fedora-Cloud-Base-Generic-44-1.7.aarch64.qcow2) = 11223344556677889900aabbccddeeff11223344556677889900aabbccddeeff\n",
	)

	cases := map[string]string{
		"noble-server-cloudimg-amd64.img":                "abc123def456",
		"noble-server-cloudimg-arm64.img":                "deadbeef00",
		"noble-desktop.iso":                              "cafef00d",
		"Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2":  "28680fe5b371a5a82ebf43a31926e086a168e59949d03969c5093e7071f90b7f",
		"Fedora-Cloud-Base-Generic-44-1.7.aarch64.qcow2": "11223344556677889900aabbccddeeff11223344556677889900aabbccddeeff",
	}
	for filename, want := range cases {
		got, err := ParseSHA256Sums(doc, filename)
		if err != nil {
			t.Errorf("ParseSHA256Sums(%q): %v", filename, err)
			continue
		}

		if got != want {
			t.Errorf("ParseSHA256Sums(%q) = %q, want %q", filename, got, want)
		}
	}
}

func TestParseSHA256SumsNotFound(t *testing.T) {
	doc := []byte("abc123  something-else.img\n")
	if _, err := ParseSHA256Sums(doc, "missing.img"); err == nil {
		t.Fatal("expected error for missing filename")
	}
}

func TestVerifyFileMatchAndMismatch(t *testing.T) {
	dir := t.TempDir()

	path := filepath.Join(dir, "data.bin")
	if err := os.WriteFile(path, []byte("hello world"), 0o600); err != nil {
		t.Fatal(err)
	}

	// sha256("hello world")
	const correctDigest = "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"

	sums := []byte(correctDigest + "  data.bin\n")
	if err := VerifyFile(sums, "data.bin", path); err != nil {
		t.Errorf("expected match, got error: %v", err)
	}

	badSums := []byte("0000000000000000000000000000000000000000000000000000000000000000  data.bin\n")
	if err := VerifyFile(badSums, "data.bin", path); err == nil {
		t.Error("expected checksum mismatch error")
	}
}
