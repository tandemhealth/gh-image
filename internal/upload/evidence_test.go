package upload

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func validTestPNGWithColor(pixel color.NRGBA) []byte {
	var data bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, pixel)
	if err := png.Encode(&data, img); err != nil {
		panic(err)
	}
	return data.Bytes()
}

func validTestPNG() []byte {
	return validTestPNGWithColor(color.NRGBA{R: 12, G: 34, B: 56, A: 255})
}

var testPNG = validTestPNG()

func writeTestPNG(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, testPNG, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOpenEvidence(t *testing.T) {
	t.Run("opens one canonical PNG and retains the descriptor", func(t *testing.T) {
		root := t.TempDir()
		path := writeTestPNG(t, root, "shot.png")

		evidence, err := OpenEvidence(root, path)
		if err != nil {
			t.Fatalf("OpenEvidence() error = %v", err)
		}
		if evidence.Name() != "shot.png" {
			t.Fatalf("Name() = %q, want shot.png", evidence.Name())
		}
		if evidence.Size() != int64(len(testPNG)) {
			t.Fatalf("Size() = %d, want %d", evidence.Size(), len(testPNG))
		}
		snapshotBefore, err := io.ReadAll(evidence.Reader())
		if err != nil {
			t.Fatal(err)
		}

		moved := filepath.Join(root, "moved.png")
		if err := os.Rename(path, moved); err != nil {
			t.Fatal(err)
		}
		replacement := validTestPNGWithColor(color.NRGBA{R: 200, G: 100, B: 50, A: 255})
		if err := os.WriteFile(path, replacement, 0o600); err != nil {
			t.Fatal(err)
		}

		got, err := io.ReadAll(evidence.Reader())
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, snapshotBefore) || bytes.Equal(got, replacement) {
			t.Fatalf("opened descriptor changed after path replacement: got %q", got)
		}
	})

	t.Run("accepts an uppercase PNG extension", func(t *testing.T) {
		root := t.TempDir()
		path := writeTestPNG(t, root, "shot.PNG")
		evidence, err := OpenEvidence(root, path)
		if err != nil {
			t.Fatalf("OpenEvidence() error = %v", err)
		}
		if evidence.Name() != "shot.PNG" {
			t.Fatalf("Name() = %q, want shot.PNG", evidence.Name())
		}
	})

	tests := []struct {
		name string
		make func(t *testing.T) (root, path string)
		want string
	}{
		{
			name: "relative evidence root",
			make: func(t *testing.T) (string, string) {
				root := t.TempDir()
				return ".", writeTestPNG(t, root, "shot.png")
			},
			want: "evidence root must be absolute",
		},
		{
			name: "relative PNG path",
			make: func(t *testing.T) (string, string) {
				return t.TempDir(), "shot.png"
			},
			want: "PNG path must be absolute",
		},
		{
			name: "path outside evidence root",
			make: func(t *testing.T) (string, string) {
				return t.TempDir(), writeTestPNG(t, t.TempDir(), "shot.png")
			},
			want: "outside evidence root",
		},
		{
			name: "non-PNG extension",
			make: func(t *testing.T) (string, string) {
				root := t.TempDir()
				return root, writeTestPNG(t, root, "shot.jpg")
			},
			want: "must use the .png extension",
		},
		{
			name: "Markdown control character in filename",
			make: func(t *testing.T) (string, string) {
				root := t.TempDir()
				return root, writeTestPNG(t, root, "shot]oops.png")
			},
			want: "unsupported character",
		},
		{
			name: "forged PNG extension",
			make: func(t *testing.T) (string, string) {
				root := t.TempDir()
				path := filepath.Join(root, "shot.png")
				if err := os.WriteFile(path, []byte("not a png"), 0o600); err != nil {
					t.Fatal(err)
				}
				return root, path
			},
			want: "invalid PNG signature",
		},
		{
			name: "signature without valid PNG structure",
			make: func(t *testing.T) (string, string) {
				root := t.TempDir()
				path := filepath.Join(root, "shot.png")
				if err := os.WriteFile(path, append([]byte{}, pngSignature...), 0o600); err != nil {
					t.Fatal(err)
				}
				return root, path
			},
			want: "invalid PNG data",
		},
		{
			name: "payload after IEND",
			make: func(t *testing.T) (string, string) {
				root := t.TempDir()
				path := filepath.Join(root, "shot.png")
				if err := os.WriteFile(path, append(append([]byte{}, testPNG...), []byte("hidden payload")...), 0o600); err != nil {
					t.Fatal(err)
				}
				return root, path
			},
			want: "data follows IEND chunk",
		},
		{
			name: "directory",
			make: func(t *testing.T) (string, string) {
				root := t.TempDir()
				path := filepath.Join(root, "directory.png")
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
				return root, path
			},
			want: "regular file",
		},
		{
			name: "file exceeds limit",
			make: func(t *testing.T) (string, string) {
				root := t.TempDir()
				path := writeTestPNG(t, root, "large.png")
				if err := os.Truncate(path, MaxPNGSize+1); err != nil {
					t.Fatal(err)
				}
				return root, path
			},
			want: "exceeds 10 MB limit",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root, path := tc.make(t)
			_, err := OpenEvidence(root, path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("OpenEvidence() error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestOpenEvidenceSizeBoundary(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "exact-limit.png")
	if err := os.WriteFile(path, pngWithAncillarySize(t, testPNG, MaxPNGSize), 0o600); err != nil {
		t.Fatal(err)
	}
	evidence, err := OpenEvidence(root, path)
	if err != nil {
		t.Fatalf("OpenEvidence() at exact limit error = %v", err)
	}
	if evidence.Size() >= MaxPNGSize {
		t.Fatalf("canonical Size() = %d, want metadata stripped below %d", evidence.Size(), MaxPNGSize)
	}
}

func pngWithAncillarySize(t *testing.T, base []byte, targetSize int64) []byte {
	t.Helper()
	const chunkOverhead = 12
	if len(base) < chunkOverhead || string(base[len(base)-8:len(base)-4]) != "IEND" {
		t.Fatal("base PNG does not end in IEND")
	}
	payloadSize := targetSize - int64(len(base)) - chunkOverhead
	if payloadSize < 0 || payloadSize > int64(^uint32(0)) {
		t.Fatalf("invalid ancillary payload size %d", payloadSize)
	}
	iendStart := len(base) - chunkOverhead
	result := make([]byte, 0, targetSize)
	result = append(result, base[:iendStart]...)
	header := make([]byte, 8)
	binary.BigEndian.PutUint32(header[:4], uint32(payloadSize))
	copy(header[4:], "raNd")
	result = append(result, header...)
	result = append(result, make([]byte, payloadSize)...)
	crc := crc32.NewIEEE()
	_, _ = crc.Write(header[4:])
	_, _ = crc.Write(result[len(result)-int(payloadSize):])
	crcBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(crcBytes, crc.Sum32())
	result = append(result, crcBytes...)
	result = append(result, base[iendStart:]...)
	if int64(len(result)) != targetSize {
		t.Fatalf("PNG size = %d, want %d", len(result), targetSize)
	}
	return result
}

func TestOpenEvidenceRejectsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires additional privileges on Windows")
	}

	t.Run("file symlink", func(t *testing.T) {
		root := t.TempDir()
		target := writeTestPNG(t, root, "target.png")
		link := filepath.Join(root, "link.png")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		_, err := OpenEvidence(root, link)
		if err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("OpenEvidence() error = %v, want symlink rejection", err)
		}
	})

	t.Run("evidence root symlink", func(t *testing.T) {
		parent := t.TempDir()
		realRoot := filepath.Join(parent, "real")
		if err := os.Mkdir(realRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		path := writeTestPNG(t, realRoot, "shot.png")
		linkedRoot := filepath.Join(parent, "linked")
		if err := os.Symlink(realRoot, linkedRoot); err != nil {
			t.Fatal(err)
		}
		_, err := OpenEvidence(linkedRoot, filepath.Join(linkedRoot, filepath.Base(path)))
		if err == nil || !strings.Contains(err.Error(), "evidence root must not be a symlink") {
			t.Fatalf("OpenEvidence() error = %v, want root symlink rejection", err)
		}
	})

	t.Run("evidence root replacement", func(t *testing.T) {
		parent := t.TempDir()
		root := filepath.Join(parent, "evidence")
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
		path := writeTestPNG(t, root, "shot.png")
		openReplacement := func(name string) (*os.Root, error) {
			if err := os.Rename(root, filepath.Join(parent, "original")); err != nil {
				return nil, err
			}
			if err := os.Mkdir(root, 0o700); err != nil {
				return nil, err
			}
			if err := os.WriteFile(filepath.Join(root, "shot.png"), testPNG, 0o600); err != nil {
				return nil, err
			}
			return os.OpenRoot(name)
		}
		_, err := openEvidence(root, path, openReplacement)
		if err == nil || !strings.Contains(err.Error(), "evidence root changed") {
			t.Fatalf("openEvidence() error = %v, want root replacement rejection", err)
		}
	})

	t.Run("parent directory symlink", func(t *testing.T) {
		root := t.TempDir()
		realDir := filepath.Join(root, "real")
		if err := os.Mkdir(realDir, 0o700); err != nil {
			t.Fatal(err)
		}
		_ = writeTestPNG(t, realDir, "shot.png")
		linkDir := filepath.Join(root, "linked")
		if err := os.Symlink(realDir, linkDir); err != nil {
			t.Fatal(err)
		}
		_, err := OpenEvidence(root, filepath.Join(linkDir, "shot.png"))
		if err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("OpenEvidence() error = %v, want parent symlink rejection", err)
		}
	})
}
