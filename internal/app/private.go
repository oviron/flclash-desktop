package app

import (
	"os"
	"path/filepath"
)

// writePrivate writes a file only its owner may read, also in place of one
// others could: the data goes to a new file made private from the start,
// which then takes the old one's name.
func writePrivate(path string, data []byte) error {
	f, err := createPrivate(filepath.Dir(path))
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}
