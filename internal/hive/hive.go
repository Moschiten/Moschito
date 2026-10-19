package hive

import (
	"os"
	"path/filepath"
)

type Hive struct {
	BaseDir  string
	ImageDir string
	BuildDir string
}

func New() *Hive {
	home, _ := os.UserHomeDir()
	base := filepath.Join(home, ".local", "share", "moschito")

	h := &Hive{
		BaseDir:  base,
		ImageDir: filepath.Join(base, "images"),
		BuildDir: filepath.Join(base, "builds"),
	}

	os.MkdirAll(h.ImageDir, 0755)
	os.MkdirAll(h.BuildDir, 0755)

	return h
}
