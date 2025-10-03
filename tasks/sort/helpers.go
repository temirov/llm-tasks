package sort

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"mime"
	"path/filepath"
	"strings"

	"github.com/temirov/llm-tasks/internal/fsops"
)

const (
	maxArchiveEntries = 10
)

func collectArchiveEntries(fs fsops.FS, info fsops.FileInfo) ([]ArchiveEntry, error) {
	if !isSupportedArchive(info.AbsolutePath) {
		return nil, nil
	}
	data, readErr := fs.ReadFile(info.AbsolutePath)
	if readErr != nil {
		return nil, readErr
	}
	reader := bytes.NewReader(data)
	switch detectArchiveKind(info.AbsolutePath) {
	case archiveZip:
		return inspectZipEntries(reader, int64(len(data)))
	case archiveTar:
		return inspectTarEntries(reader)
	case archiveTarGz:
		gz, err := gzip.NewReader(reader)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		return inspectTarEntries(gz)
	default:
		return nil, nil
	}
}

type archiveKind int

const (
	archiveUnknown archiveKind = iota
	archiveZip
	archiveTar
	archiveTarGz
)

func detectArchiveKind(path string) archiveKind {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return archiveTarGz
	case strings.HasSuffix(lower, ".tar"):
		return archiveTar
	case strings.HasSuffix(lower, ".zip"):
		return archiveZip
	default:
		return archiveUnknown
	}
}

func isSupportedArchive(path string) bool {
	return detectArchiveKind(path) != archiveUnknown
}

func inspectZipEntries(reader io.ReaderAt, size int64) ([]ArchiveEntry, error) {
	zr, err := zip.NewReader(reader, size)
	if err != nil {
		return nil, err
	}
	var entries []ArchiveEntry
	for _, f := range zr.File {
		if len(entries) >= maxArchiveEntries {
			break
		}
		info := f.FileInfo()
		if info.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(f.Name))
		mimeType := mime.TypeByExtension(ext)
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		size := int64(f.UncompressedSize64)
		entries = append(entries, ArchiveEntry{
			Path:      filepath.Clean(f.Name),
			MIMEType:  mimeType,
			SizeBytes: size,
		})
	}
	return entries, nil
}

func inspectTarEntries(r io.Reader) ([]ArchiveEntry, error) {
	tr := tar.NewReader(r)
	var entries []ArchiveEntry
	for len(entries) < maxArchiveEntries {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.FileInfo().IsDir() {
			continue
		}
		size := hdr.FileInfo().Size()
		ext := strings.ToLower(filepath.Ext(hdr.Name))
		mimeType := mime.TypeByExtension(ext)
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		entries = append(entries, ArchiveEntry{
			Path:      filepath.Clean(hdr.Name),
			MIMEType:  mimeType,
			SizeBytes: size,
		})
		if size > 0 {
			if _, skipErr := io.CopyN(io.Discard, tr, size); skipErr != nil && !errors.Is(skipErr, io.EOF) {
				return nil, skipErr
			}
		}
	}
	return entries, nil
}
