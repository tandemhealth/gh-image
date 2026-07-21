package upload

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// MaxPNGSize matches GitHub's documented 10 MB limit for images.
const MaxPNGSize int64 = 10_000_000

const (
	maxPNGDimension  = 20_000
	maxPNGPixelCount = 40_000_000
)

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

// Evidence is an immutable snapshot of one validated screenshot. Snapshotting
// before any network request keeps the policy size and uploaded bytes identical
// even if the source path or inode changes later.
type Evidence struct {
	name string
	data []byte
}

func (e *Evidence) Name() string { return e.name }

func (e *Evidence) Size() int64 { return int64(len(e.data)) }

func (e *Evidence) Reader() io.Reader { return bytes.NewReader(e.data) }

// OpenEvidence validates and snapshots one PNG beneath evidenceRoot. Both paths
// must be absolute. Symlinks are rejected at the root, directory, and file
// levels; os.Root also prevents concurrent path traversal outside the root.
func OpenEvidence(evidenceRoot, inputPath string) (*Evidence, error) {
	return openEvidence(evidenceRoot, inputPath, os.OpenRoot)
}

func openEvidence(evidenceRoot, inputPath string, openRoot func(string) (*os.Root, error)) (*Evidence, error) {
	if !filepath.IsAbs(evidenceRoot) {
		return nil, fmt.Errorf("evidence root must be absolute: %q", evidenceRoot)
	}
	if !filepath.IsAbs(inputPath) {
		return nil, fmt.Errorf("PNG path must be absolute: %q", inputPath)
	}

	rootPath := filepath.Clean(evidenceRoot)
	path := filepath.Clean(inputPath)
	if !strings.EqualFold(filepath.Ext(path), ".png") {
		return nil, fmt.Errorf("screenshot must use the .png extension: %q", path)
	}
	name := filepath.Base(path)
	if err := validateEvidenceName(name); err != nil {
		return nil, err
	}

	rootInfo, err := os.Lstat(rootPath)
	if err != nil {
		return nil, fmt.Errorf("evidence root: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("evidence root must not be a symlink: %q", rootPath)
	}
	if !rootInfo.IsDir() {
		return nil, fmt.Errorf("evidence root must be a directory: %q", rootPath)
	}

	rel, err := filepath.Rel(rootPath, path)
	if err != nil {
		return nil, fmt.Errorf("resolving PNG path beneath evidence root: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("PNG path is outside evidence root: %q", path)
	}

	root, err := openRoot(rootPath)
	if err != nil {
		return nil, fmt.Errorf("opening evidence root: %w", err)
	}
	defer func() { _ = root.Close() }()
	openedRootInfo, err := root.Stat(".")
	if err != nil {
		return nil, fmt.Errorf("inspecting opened evidence root: %w", err)
	}
	if !openedRootInfo.IsDir() || !os.SameFile(rootInfo, openedRootInfo) {
		return nil, fmt.Errorf("evidence root changed while it was being opened: %q", rootPath)
	}

	component := ""
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if component == "" {
			component = part
		} else {
			component = filepath.Join(component, part)
		}
		info, err := root.Lstat(component)
		if err != nil {
			return nil, fmt.Errorf("PNG path: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("PNG path must not contain a symlink: %q", filepath.Join(rootPath, component))
		}
	}

	pathInfo, err := root.Lstat(rel)
	if err != nil {
		return nil, fmt.Errorf("PNG path: %w", err)
	}
	if !pathInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("PNG path must be a regular file: %q", path)
	}
	if pathInfo.Size() > MaxPNGSize {
		return nil, fmt.Errorf("PNG size %d exceeds 10 MB limit (%d bytes)", pathInfo.Size(), MaxPNGSize)
	}

	file, err := root.Open(rel)
	if err != nil {
		return nil, fmt.Errorf("opening PNG: %w", err)
	}
	defer func() { _ = file.Close() }()

	openedInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspecting opened PNG: %w", err)
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(pathInfo, openedInfo) {
		return nil, fmt.Errorf("PNG path changed while it was being opened: %q", path)
	}

	data, err := io.ReadAll(io.LimitReader(file, MaxPNGSize+1))
	if err != nil {
		return nil, fmt.Errorf("reading PNG: %w", err)
	}
	if int64(len(data)) > MaxPNGSize {
		return nil, fmt.Errorf("PNG size exceeds 10 MB limit (%d bytes)", MaxPNGSize)
	}
	finalInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("rechecking opened PNG: %w", err)
	}
	if int64(len(data)) != openedInfo.Size() || finalInfo.Size() != openedInfo.Size() {
		return nil, fmt.Errorf("PNG changed while it was being read: %q", path)
	}
	if err := validatePNGChunks(data); err != nil {
		return nil, fmt.Errorf("invalid PNG data %q: %w", path, err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("invalid PNG data %q: %w", path, err)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > maxPNGDimension || config.Height > maxPNGDimension || int64(config.Width)*int64(config.Height) > maxPNGPixelCount {
		return nil, fmt.Errorf("PNG dimensions %dx%d exceed the %d-pixel decode limit", config.Width, config.Height, maxPNGPixelCount)
	}
	image, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("invalid PNG image data %q: %w", path, err)
	}
	var canonical bytes.Buffer
	if err := png.Encode(&canonical, image); err != nil {
		return nil, fmt.Errorf("canonicalizing PNG %q: %w", path, err)
	}
	if int64(canonical.Len()) > MaxPNGSize {
		return nil, fmt.Errorf("canonical PNG size %d exceeds 10 MB limit (%d bytes)", canonical.Len(), MaxPNGSize)
	}

	return &Evidence{name: name, data: canonical.Bytes()}, nil
}

func validatePNGChunks(data []byte) error {
	if !bytes.HasPrefix(data, pngSignature) {
		return fmt.Errorf("invalid PNG signature")
	}
	position := len(pngSignature)
	chunkIndex := 0
	seenIDAT := false
	for position < len(data) {
		if len(data)-position < 12 {
			return fmt.Errorf("truncated chunk at byte %d", position)
		}
		length := int64(binary.BigEndian.Uint32(data[position : position+4]))
		chunkEnd := int64(position) + 12 + length
		if chunkEnd > int64(len(data)) {
			return fmt.Errorf("chunk at byte %d exceeds file length", position)
		}
		typeStart := position + 4
		dataEnd := int64(typeStart+4) + length
		chunkType := string(data[typeStart : typeStart+4])
		wantCRC := binary.BigEndian.Uint32(data[dataEnd : dataEnd+4])
		gotCRC := crc32.ChecksumIEEE(data[typeStart:dataEnd])
		if gotCRC != wantCRC {
			return fmt.Errorf("%s chunk has invalid CRC", chunkType)
		}
		if chunkIndex == 0 && chunkType != "IHDR" {
			return fmt.Errorf("first chunk is %s, want IHDR", chunkType)
		}
		if chunkType == "IDAT" {
			seenIDAT = true
		}
		if chunkType == "IEND" {
			if length != 0 {
				return fmt.Errorf("IEND chunk has non-zero length")
			}
			if !seenIDAT {
				return fmt.Errorf("PNG has no IDAT chunk")
			}
			if chunkEnd != int64(len(data)) {
				return fmt.Errorf("data follows IEND chunk")
			}
			return nil
		}
		position = int(chunkEnd)
		chunkIndex++
	}
	return fmt.Errorf("PNG has no IEND chunk")
}

func validateEvidenceName(name string) error {
	if len(name) > 128 {
		return fmt.Errorf("PNG filename exceeds 128 bytes: %q", name)
	}
	for i, r := range name {
		alphaNumeric := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
		if i == 0 && !alphaNumeric {
			return fmt.Errorf("PNG filename must start with an ASCII letter or digit: %q", name)
		}
		if !alphaNumeric && r != '.' && r != '-' && r != '_' {
			return fmt.Errorf("PNG filename contains an unsupported character: %q", name)
		}
	}
	return nil
}
