package api

import "os"

// removeSocket deletes path if it exists and looks like a socket. Returns nil
// when the file is absent so callers can chain it before bind.
func removeSocket(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		// Refuse to touch a non-socket file.
		return nil
	}
	return os.Remove(path)
}
