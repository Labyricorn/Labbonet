package server

import (
	"fmt"
	"os"
	"path/filepath"
)

// LockFileName is the name of the exclusive lock file inside the data
// directory.
const LockFileName = "labbonet.lock"

// DataDirLock holds the open lock file for the lifetime of the process. A
// second instance pointed at the same data directory fails to acquire it
// (A-B3).
type DataDirLock struct {
	f *os.File
}

// AcquireDataDirLock creates (or reuses) the lock file in dataDir and takes
// an exclusive, process-lifetime hold on it. It returns an error if another
// process already holds it.
func AcquireDataDirLock(dataDir string) (*DataDirLock, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}

	path := filepath.Join(dataDir, LockFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("data directory %q is already in use by another Labbonet instance (remove %q if that is not the case)", dataDir, path)
		}
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	return &DataDirLock{f: f}, nil
}

// Release closes and removes the lock file.
func (l *DataDirLock) Release() error {
	path := l.f.Name()
	if err := l.f.Close(); err != nil {
		return err
	}
	return os.Remove(path)
}
