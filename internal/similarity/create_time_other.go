//go:build !darwin

package similarity

import (
	"os"
	"time"
)

// getFileCreateTime falls back to modification time on non-macOS platforms.
func getFileCreateTime(_ string, info os.FileInfo) time.Time {
	return info.ModTime()
}
