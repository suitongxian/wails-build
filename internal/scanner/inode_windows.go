//go:build windows

package scanner

import "os"

// getInode is unavailable on Windows. The scanner treats ok=false as "skip
// inode-based symlink dedupe" and continues scanning normally.
func getInode(_ os.FileInfo) (uint64, bool) {
	return 0, false
}
