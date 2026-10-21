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

	// Try to read it as a gzip file first (standard for OCI layers)
	gzr, gzErr := gzip.NewReader(f)
	var tr *tar.Reader
	
	if gzErr == nil {
		defer gzr.Close()
		tr = tar.NewReader(gzr)
	} else {
		// If gzip fails, rewind the file and try reading it as a raw tar
		if _, err := f.Seek(0, 0); err != nil {
			return fmt.Errorf("failed to seek blob: %w", err)
		}
		tr = tar.NewReader(f)
	}

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
