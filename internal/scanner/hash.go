package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"
)

// FileHashResult represents the result of a file hash operation.
// Hash is uppercase hex of the file's SHA-256 digest, computed over the
// entire file contents (no partial sampling).
type FileHashResult struct {
	Hash string // SHA-256 hex (uppercase)
	Size int64  // File size in bytes
}

// CalculateFileHash streams the entire file through SHA-256 and returns the
// uppercase hex digest plus file size. Streaming avoids loading large files
// into memory while still hashing every byte, so two files only collide if
// they are byte-identical — unlike the old partial-MD5 sampling which could
// false-positive on big office documents that share identical zip
// headers/central-directory trailers but differ in the middle.
func CalculateFileHash(filePath string) (FileHashResult, error) {
	fi, err := os.Stat(filePath)
	if err != nil {
		return FileHashResult{}, err
	}

	f, err := os.Open(filePath)
	if err != nil {
		return FileHashResult{}, err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return FileHashResult{}, err
	}
	return FileHashResult{
		Hash: strings.ToUpper(hex.EncodeToString(h.Sum(nil))),
		Size: fi.Size(),
	}, nil
}

// ReadFileMagic reads the first 8 bytes of a file and returns them as
// uppercase hex. Used by the scanner to record file_magic alongside the hash.
func ReadFileMagic(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	buffer := make([]byte, 8)
	n, err := f.Read(buffer)
	if err != nil && err != io.EOF {
		return "", err
	}

	if n > 0 {
		return strings.ToUpper(hex.EncodeToString(buffer[:n])), nil
	}
	return "", nil
}
