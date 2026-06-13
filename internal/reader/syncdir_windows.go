//go:build windows

package reader

func syncDir(string) error {
	// Windows does not support the Unix directory fsync pattern used after
	// rename. The cursor file itself is still fsynced before the rename.
	return nil
}
