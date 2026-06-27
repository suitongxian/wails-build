package repository

// 一键归档的纯规则层：按桶定级、按 scope 路由、九宫格目标文件夹名。
// 与磁盘/DB 无关，便于单测；落盘与路由由 quick_archive.go 调用。

// ArchiveRoute 归档去向。
type ArchiveRoute int

const (
	RouteSkip  ArchiveRoute = iota // 跳过（行业级 / 未知）
	RouteLocal                     // 本地复制到个人夹
	RouteCloud                     // 上报云端 manage（部门柜 / 单位室）
)

// sensitivityRank 密级排序：核心 > 重要 > 一般（数字越小越高）。
func sensitivityRank(level string) int {
	switch NormalizeSensitivity(level) {
	case "core":
		return 1
	case "important":
		return 2
	default:
		return 3
	}
}

// maxSensitivity 取两个密级里更高的一个（就高不就低）。
func maxSensitivity(a, b string) string {
	if sensitivityRank(a) <= sensitivityRank(b) {
		na := NormalizeSensitivity(a)
		if na == "" {
			return NormalizeSensitivity(b)
		}
		return na
	}
	return NormalizeSensitivity(b)
}

// BucketArchiveLevel 按桶决定归档级别与是否归档：
//   - input(工作依据)     : 不归档
//   - reference(参考)     : 用导入者声明级（refLevel 为空按一般）
//   - process(过程)       : = 项目敏感级
//   - output(定稿)        : = 项目敏感级，且不低于「重要」
func BucketArchiveLevel(bucket, projectSensitivity, refLevel string) (level string, archive bool) {
	switch bucket {
	case "input":
		return "", false
	case "reference":
		l := NormalizeSensitivity(refLevel)
		if l == "" {
			l = "general"
		}
		return l, true
	case "process":
		l := NormalizeSensitivity(projectSensitivity)
		if l == "" {
			l = "general"
		}
		return l, true
	case "output":
		// 定稿不低于重要
		return maxSensitivity(projectSensitivity, "important"), true
	default:
		return "", false
	}
}

// ScopeRoute 按项目层级(scope)决定去向与容器前/后缀。
//
//	person→本地个人文件夹 / department→云端部门文件柜 / unit→云端单位文件室 / industry+其它→跳过
func ScopeRoute(scope string) (route ArchiveRoute, prefix, suffix string) {
	switch scope {
	case "person":
		return RouteLocal, "个人", "文件夹"
	case "department":
		return RouteCloud, "部门", "文件柜"
	case "unit":
		return RouteCloud, "单位", "文件室"
	default: // industry / 空 / 未知
		return RouteSkip, "", ""
	}
}

// sensitivityLevelLabel 文件级别中文名：核心/重要/一般。
func sensitivityLevelLabel(level string) string {
	switch NormalizeSensitivity(level) {
	case "core":
		return "核心"
	case "important":
		return "重要"
	default:
		return "一般"
	}
}

// NineGridFolder 九宫格目标文件夹名 = 前缀(个人/部门/单位)+级别(核心/重要/一般)+后缀(文件夹/文件柜/文件室)。
// 例：个人+重要+文件夹 = 个人重要文件夹；部门+核心+文件柜 = 部门核心文件柜。
func NineGridFolder(scope, level string) string {
	_, prefix, suffix := ScopeRoute(scope)
	if prefix == "" {
		return ""
	}
	return prefix + sensitivityLevelLabel(level) + suffix
}
