// verification/main.go
// Partial Hash 交叉验证工具
// 用法: go run main.go <目录路径>
// 输出: 对比该目录下所有文件的 partial hash

package main

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	threshold int64 = 5 * 1024 * 1024 // 5MB
	sampleSize       = 4096
)

// PartialMD5 Go 版本的 partial hash 实现
func PartialMD5(path string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", err
	}

	if fi.Size() <= threshold {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		sum := md5.Sum(data)
		return strings.ToUpper(hex.EncodeToString(sum[:])), nil
	}

	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	head := make([]byte, sampleSize)
	tail := make([]byte, sampleSize)

	f.ReadAt(head, 0)
	f.ReadAt(tail, fi.Size()-sampleSize)

	combined := append(head, tail...)
	sum := md5.Sum(combined)
	return strings.ToUpper(hex.EncodeToString(sum[:])), nil
}

type CompareResult struct {
	Path        string  `json:"path"`
	FileSize    int64   `json:"fileSize"`
	GoHash      string  `json:"goHash"`
	Category    string  `json:"category"` // "tiny"(<4KB) / "small"(<5MB) / "large"(>=5MB)
	OsHash      string  `json:"osHash,omitempty"`
	Match       bool    `json:"match"`
	Error       string  `json:"error,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("用法: go run main.go <目录路径> [--ts-output <文件>]")
		fmt.Println("  --ts-output: 指定原版 TypeScript 的 JSON 输出文件路径（可选）")
		os.Exit(1)
	}

	rootDir := os.Args[1]
	var tsOutputFile string
	for i := 2; i < len(os.Args); i++ {
		if os.Args[i] == "--ts-output" && i+1 < len(os.Args) {
			tsOutputFile = os.Args[i+1]
			i++
		}
	}

	// 读取 TypeScript 输出（如果提供了）
	var tsHashes map[string]string
	if tsOutputFile != "" {
		data, err := os.ReadFile(tsOutputFile)
		if err != nil {
			fmt.Printf("警告: 无法读取 TS 输出文件: %v\n", err)
		} else {
			json.Unmarshal(data, &tsHashes)
		}
	}

	var results []CompareResult
	matchCount := 0
	mismatchCount := 0
	errorCount := 0

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // 跳过权限拒绝等错误
		}
		if info.IsDir() {
			return nil
		}

		result := CompareResult{
			Path:      path,
			FileSize:  info.Size(),
			GoHash:    "",
			Category:  categorize(info.Size()),
		}

		hash, err := PartialMD5(path)
		if err != nil {
			result.Error = err.Error()
			errorCount++
			results = append(results, result)
			return nil
		}
		result.GoHash = hash

		// 如果有 TS 输出，进行对比
		if tsHashes != nil {
			if tsHash, ok := tsHashes[path]; ok {
				result.OsHash = tsHash
				result.Match = (hash == tsHash)
				if result.Match {
					matchCount++
				} else {
					mismatchCount++
					fmt.Printf("❌ 不匹配: %s\n  Go:     %s\n  TS:     %s\n", path, hash, tsHash)
				}
			}
		}

		results = append(results, result)
		return nil
	})

	if err != nil {
		fmt.Printf("遍历目录出错: %v\n", err)
		os.Exit(1)
	}

	// 输出统计
	fmt.Printf("\n========== 验证结果 ==========\n")
	fmt.Printf("总文件数: %d\n", len(results))
	if tsHashes != nil {
		fmt.Printf("匹配: %d | 不匹配: %d | 错误: %d\n", matchCount, mismatchCount, errorCount)
	}

	// 按大小分类统计
	tiny := 0
	small := 0
	large := 0
	for _, r := range results {
		switch r.Category {
		case "tiny":
			tiny++
		case "small":
			small++
		case "large":
			large++
		}
	}
	fmt.Printf("tiny(<4KB): %d | small(<5MB): %d | large(>=5MB): %d\n", tiny, small, large)

	// 输出 JSON 报告
	report, _ := json.MarshalIndent(results, "", "  ")
	os.WriteFile("hash_compare_report.json", report, 0644)
	fmt.Printf("\n详细报告已写入: hash_compare_report.json\n")

	if mismatchCount > 0 {
		fmt.Printf("\n⚠️  存在 %d 个不匹配，请检查!\n", mismatchCount)
		os.Exit(1)
	} else if matchCount > 0 {
		fmt.Printf("\n✅ 全部匹配!\n")
	}
}

func categorize(size int64) string {
	if size < 4*1024 {
		return "tiny"
	} else if size < 5*1024*1024 {
		return "small"
	}
	return "large"
}
