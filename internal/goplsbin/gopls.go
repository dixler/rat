package goplsbin

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

//go:embed *
var fs embed.FS

func Path() (string, error) {
	binary, err := fs.ReadFile("gopls")
	if err != nil {
		return "", err
	}
	if len(binary) == 0 {
		return "", fmt.Errorf("embedded gopls binary is empty")
	}

	//sum := sha256.Sum256(binary)
	name := fmt.Sprintf("gopls-%s-%s", runtime.GOOS, runtime.GOARCH)
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "rat")
	path := filepath.Join(dir, name)

	if info, err := os.Stat(path); err == nil && info.Mode().Perm()&0100 != 0 && info.Size() == int64(len(binary)) {
		return path, nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	tmp, err := os.CreateTemp(dir, name+"-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(binary); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Chmod(0755); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", err
	}
	return path, nil
}
