//go:build !windows

package scanner

import (
	"os"

	"golang.org/x/sys/unix"
)

// getInode returns the inode number of a file on Unix-like platforms.
func getInode(info os.FileInfo) (uint64, bool) {
	stat, ok := info.Sys().(*unix.Stat_t)
	if !ok {
		return 0, false
	}
	return stat.Ino, true
}
