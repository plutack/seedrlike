package upload

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestZipFolderRoundTripAndCRC(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "bundle")
	fixtures := map[string][]byte{
		"nested/deep/text.txt":   bytes.Repeat([]byte("compressible text\n"), 200000),
		"video.mkv":              bytes.Repeat([]byte{0, 255, 23, 42, 128, 1, 2, 3}, 200000),
		"empty.txt":              {},
		"unicode/épisode 01.txt": []byte("Unicode path and contents: 日本語"),
	}
	for name, data := range fixtures {
		path := filepath.Join(source, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for run := 0; run < 3; run++ { // Exercise pooled compressor reuse as well.
		destination := filepath.Join(root, "output.zip")
		if err := ZipFolder(source, destination, nil, func(int64, int64) {}); err != nil {
			t.Fatal(err)
		}
		archive, err := zip.OpenReader(destination)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, entry := range archive.File {
			if entry.FileInfo().IsDir() {
				continue
			}
			rel, err := filepath.Rel("bundle", entry.Name)
			if err != nil {
				t.Fatal(err)
			}
			expected, ok := fixtures[rel]
			if !ok {
				t.Fatalf("unexpected entry %q", entry.Name)
			}
			reader, err := entry.Open()
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(reader)
			reader.Close() // Reading to EOF validates CRC.
			if err != nil {
				t.Fatalf("CRC/decompression failed %q: %v", entry.Name, err)
			}
			if !bytes.Equal(got, expected) {
				t.Fatalf("bytes changed for %s", entry.Name)
			}
			if rel == "video.mkv" && entry.Method != zip.Store {
				t.Fatal("store path not exercised")
			}
			if rel == "nested/deep/text.txt" && entry.Method != zip.Deflate {
				t.Fatal("deflate path not exercised")
			}
			seen[rel] = true
		}
		archive.Close()
		if len(seen) != len(fixtures) {
			t.Fatalf("lost files: %v", seen)
		}
	}
}
func TestZipSingleFileRoundTrip(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "single.bin")
	data := bytes.Repeat([]byte{0, 1, 255}, 100000)
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(root, "single.zip")
	if err := ZipFolder(source, dest, nil, func(int64, int64) {}); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if len(archive.File) != 1 || archive.File[0].Name != "single.bin" {
		t.Fatal("wrong single file layout")
	}
	r, err := archive.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("single-file corruption: %v", err)
	}
}
