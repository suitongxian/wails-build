//go:build darwin

package similarity

import (
	"os"
	"reflect"
	"syscall"
	"time"
)

// getFileCreateTime tries to read the real file birth time on macOS.
func getFileCreateTime(path string, info os.FileInfo) time.Time {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err == nil {
		v := reflect.ValueOf(st)
		birth := v.FieldByName("Birthtimespec")
		if birth.IsValid() {
			sec := birth.FieldByName("Sec")
			nsec := birth.FieldByName("Nsec")
			if sec.IsValid() && nsec.IsValid() {
				return time.Unix(sec.Int(), nsec.Int())
			}
		}
	}
	return info.ModTime()
}
