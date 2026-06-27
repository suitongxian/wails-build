package scanner

import (
	"path/filepath"
	"strings"
)

// IsSuspectNonPersonal 判断一个扫到的文件是否"疑似非个人文件"，
// 用于扫描器在写入 data_distributing 时同步打标。
//
// 设计原则：**保守**。宁可漏标也不要误标——误标真实个人文件会让用户
// 一键忽略时丢东西；漏标只是用户得手动归类。
//
// 三类规则任意命中即返回 true：
//  1. 路径模式（系统/缓存/版本控制目录等）
//  2. 后缀模式（明显二进制/系统/构建产物）
//  3. 复合启发式（小文件 + 非文档后缀）
//
// 注意：path 应是绝对路径或相对路径都行，会规范化分隔符做匹配。
func IsSuspectNonPersonal(path string, sizeBytes int64) bool {
	// 规范化为 / 分隔，跨平台一致匹配。注意：filepath.ToSlash 是平台相关的，
	// 在 Linux 上不会把 \ 替换成 /，所以必须用 strings.ReplaceAll 显式替换。
	normalized := strings.ReplaceAll(path, "\\", "/")
	lower := strings.ToLower(normalized)

	if pathLooksLikeSystemDir(lower) {
		return true
	}
	ext := strings.ToLower(filepath.Ext(path))
	if extLooksLikeBinaryArtifact(ext) {
		return true
	}
	if sizeBytes < 1024 && !extIsDocLike(ext) {
		return true
	}
	return false
}

// suspectPathSubstrings 命中即视为系统/缓存类目录。
// 用 substring 匹配（前后 / 包围）。
//
// 取舍原则：宁可漏标也不要误标。Linux 顶层目录用**两级以上**路径匹配，
// 不收 /dev/ /proc/ /sys/ /usr/ /var/ /tmp/ /run/ 这种单层（容易撞用户
// 路径：/home/dev/ 中的 dev 用户名、项目里 proc/sys/run 等常见子目录）。
var suspectPathSubstrings = []string{
	// macOS 系统
	"/library/", "/system/", "/.trash/",

	// Windows 系统
	"/windows/", "/program files/", "/program files (x86)/",
	"/appdata/", "/$recycle.bin/", "/programdata/",

	// Linux 系统（两级路径绕开误匹配）
	"/etc/", // 单层但用户路径含 /etc/ 子串概率低（区别于 /var/etc/, /usr/etc/ 一般不存在）
	"/usr/lib/", "/usr/lib64/", "/usr/share/",
	"/usr/local/lib/", "/usr/local/share/",
	"/var/cache/", "/var/log/", "/var/lib/", "/var/tmp/", "/var/spool/",
	"/snap/", "/lost+found/", "/boot/",

	// 跨平台用户级隐藏 / 缓存（多数在 walker 默认 exclude 里，这里冗余防御）
	"/.cache/", "/.git/", "/.svn/", "/.hg/",
	"/node_modules/", "/__pycache__/", "/.npm/", "/.yarn/",
	"/.gradle/", "/.m2/", "/.cargo/", "/.go/pkg/",
	"/.idea/", "/.vscode/", "/.vs/",
	"/.docker/", "/.android/", "/.tox/", "/.pytest_cache/",
	"/.gem/", "/.bundle/",
}

func pathLooksLikeSystemDir(lowerNormalizedPath string) bool {
	for _, sub := range suspectPathSubstrings {
		if strings.Contains(lowerNormalizedPath, sub) {
			return true
		}
	}
	return false
}

// suspectExts 命中即视为非个人文件（系统库/可执行/字体/构建产物等）。
var suspectExts = map[string]bool{
	// 动态/静态库 + 可执行
	".dll": true, ".sys": true, ".exe": true, ".com": true, ".bat": true, ".cmd": true,
	".so": true, ".dylib": true, ".a": true, ".o": true, ".obj": true,
	// 字体
	".ttf": true, ".otf": true, ".woff": true, ".woff2": true, ".eot": true,
	// 临时 / 锁 / 备份 / 日志
	".tmp": true, ".temp": true, ".lock": true, ".swap": true, ".swp": true,
	".bak": true, ".old": true, ".log": true, ".lnk": true,
	// 编译/构建产物
	".pyc": true, ".pyo": true, ".class": true, ".jar": true,
	// 系统元数据
	".ds_store": true,
}

func extLooksLikeBinaryArtifact(ext string) bool {
	return suspectExts[ext]
}

// docLikeExts 常见个人文档类后缀，用于"小文件不一定是 suspect"判断。
var docLikeExts = map[string]bool{
	".txt": true, ".md": true, ".rtf": true,
	".doc": true, ".docx": true, ".dot": true, ".dotx": true,
	".xls": true, ".xlsx": true, ".xlsm": true, ".csv": true,
	".ppt": true, ".pptx": true,
	".pdf": true,
	".html": true, ".htm": true, ".xml": true,
	".json": true, ".yaml": true, ".yml": true, ".toml": true,
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
	".mp4": true, ".mov": true, ".mp3": true, ".wav": true,
}

func extIsDocLike(ext string) bool {
	return docLikeExts[ext]
}
