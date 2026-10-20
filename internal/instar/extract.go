package instar

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func ExtractLayer(blobPath string, destDir string) error {
	f, err := os.Open(blobPath)
	if err != nil {
		return fmt.Errorf("failed to open blob: %w", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read error: %w", err)
		}

		target := filepath.Join(destDir, header.Name)
		os.MkdirAll(filepath.Dir(target), 0755)

		switch header.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, os.FileMode(header.Mode))

		case tar.TypeReg:
			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}
			io.Copy(outFile, tr)
			outFile.Close()

		case tar.TypeSymlink:
			os.RemoveAll(target)
			if err := os.Symlink(header.Linkname, target); err != nil {
				return fmt.Errorf("failed to create symlink: %w", err)
			}

		case tar.TypeLink:
			linkTarget := filepath.Join(destDir, header.Linkname)
			os.RemoveAll(target)
			if err := os.Link(linkTarget, target); err != nil {
				// Fallback to copy if hardlink fails
				if err := copyFile(linkTarget, target); err != nil {
					return fmt.Errorf("failed to create hardlink/copy: %w", err)
				}
			}
		}

		baseName := filepath.Base(header.Name)
		if strings.HasPrefix(baseName, ".wh.") {
			realFileName := strings.TrimPrefix(baseName, ".wh.")
			if realFileName == ".wh..opq" {
				continue
			}
			os.RemoveAll(filepath.Join(filepath.Dir(target), realFileName))
		}
	}
	return nil
}
