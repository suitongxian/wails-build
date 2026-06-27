package httpd

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"data-asset-scan-go/internal/repository"
	"data-asset-scan-go/internal/similarity"
	"github.com/gin-gonic/gin"
)

// 2026-05-24 三个 URL（upload / manage / archive）合并为单一「服务端地址」，
// 全部转发到 repository.GetEffectiveServerEndpoint()。三个 effective* 函数保留只是
// 为了让老调用方不用一次性改完——读到的都是同一个值。
func effectiveUploadServerURL(repo *repository.SystemConfigRepository) string {
	return repo.GetEffectiveServerEndpoint()
}

func effectiveManageEndpoint(repo *repository.SystemConfigRepository) string {
	return repo.GetEffectiveServerEndpoint()
}

func effectiveArchiveEndpoint(repo *repository.SystemConfigRepository) string {
	return repo.GetEffectiveServerEndpoint()
}

func loadFloatConfig(repo *repository.SystemConfigRepository, key string, fallback float64) float64 {
	v := repo.GetValue(key)
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return f
}

// GetConfig handles GET /config
// Returns combined config from system_config table + computed values
func GetConfig(c *gin.Context) {
	configRepo := repository.NewSystemConfigRepository(repository.GetDB())

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"workspace":                  configRepo.GetWorkspace(),
			"full_inventory_time":        configRepo.GetFullInventoryTime(),
			"daily_scan_interval":        configRepo.GetDailyScanInterval(),
			"last_scan_time":             configRepo.GetLastScanTime(),
			"control_type":               configRepo.GetControlType(),
			"scan_area_path":             configRepo.GetScanAreaPath(),
			"scan_exclude_dir":           configRepo.GetScanExcludeDir(),
			"last_sync_time":             configRepo.GetLastSyncTime(),
			"home_dir":                   os.Getenv("HOME"),
			"similarity_same_content":    loadFloatConfig(configRepo, similarity.CfgKeySameContent, 0.95),
			"similarity_process_version": loadFloatConfig(configRepo, similarity.CfgKeyProcessVersion, 0.75),
			"similarity_derived":         loadFloatConfig(configRepo, similarity.CfgKeyDerived, 0.50),
			"similarity_image":           loadFloatConfig(configRepo, similarity.CfgKeyImage, 0.84),
			"similarity_filename":        loadFloatConfig(configRepo, similarity.CfgKeyFileNameSim, 0.70),
			"similarity_feature":         loadFloatConfig(configRepo, similarity.CfgKeyFeatureSim, 0.60),
			// project_root 已与 workspace 合并：永远返回 EffectiveProjectRoot
			"project_root": configRepo.GetEffectiveProjectRoot(),
			// 三个 URL（upload / manage / archive）合并为单一「服务端地址」server_endpoint。
			// 老字段保留同值回显，兼容旧前端 / 旧调用方。
			"server_endpoint":   configRepo.GetEffectiveServerEndpoint(),
			"manage_endpoint":   configRepo.GetEffectiveServerEndpoint(),
			"upload_server_url": configRepo.GetEffectiveServerEndpoint(),
			"archive_endpoint":  configRepo.GetEffectiveServerEndpoint(),
			// 模版管理平台地址（同步远程模版用），与上报数据/文件的 manage 地址分离
			"template_server_endpoint": configRepo.GetEffectiveTemplateServerEndpoint(),
		},
	})
}

// SaveConfigRequest represents the POST /config request body
type SaveConfigRequest struct {
	Workspace                *string  `json:"workspace"`
	DailyScanInterval        *int     `json:"daily_scan_interval"`
	ControlType              *string  `json:"control_type"`
	ScanAreaPath             *string  `json:"scan_area_path"`
	ScanExcludeDir           *string  `json:"scan_exclude_dir"`
	UploadServerURL          *string  `json:"upload_server_url"`
	SimilaritySameContent    *float64 `json:"similarity_same_content"`
	SimilarityProcessVersion *float64 `json:"similarity_process_version"`
	SimilarityDerived        *float64 `json:"similarity_derived"`
	SimilarityImage          *float64 `json:"similarity_image"`
	SimilarityFilename       *float64 `json:"similarity_filename"`
	SimilarityFeature        *float64 `json:"similarity_feature"`
	// 数据业务模版 V1 配置
	ProjectRoot     *string `json:"project_root"`
	ManageEndpoint  *string `json:"manage_endpoint"`
	ManageToken     *string `json:"manage_token"` // 已废弃，仅接收以兼容老前端，不再生效
	ArchiveEndpoint *string `json:"archive_endpoint"`
	// 三个 URL 合并后的单一入参，前端只填这一个
	ServerEndpoint *string `json:"server_endpoint"`
	// 模版管理平台地址（template-manage，同步远程模版用），独立于上报数据/文件的 manage 地址
	TemplateServerEndpoint *string `json:"template_server_endpoint"`
}

// SaveConfig handles POST /config
// Updates individual config fields
func SaveConfig(c *gin.Context) {
	var req SaveConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	configRepo := repository.NewSystemConfigRepository(repository.GetDB())

	if req.Workspace != nil {
		configRepo.SetWorkspace(*req.Workspace)
	}
	if req.DailyScanInterval != nil {
		configRepo.SetDailyScanInterval(*req.DailyScanInterval)
	}
	if req.ControlType != nil {
		configRepo.SetControlType(*req.ControlType)
	}
	if req.ScanAreaPath != nil {
		configRepo.SetScanAreaPath(*req.ScanAreaPath)
	}
	if req.ScanExcludeDir != nil {
		configRepo.SetScanExcludeDir(*req.ScanExcludeDir)
	}
	// 三个 URL 合并：server_endpoint 优先；老字段（upload/manage/archive）传了就覆盖各自 key
	// 任何一个写入都同步到全部三个 key，让 read 路径无论查哪个 key 都得到一致值
	syncServerEndpoint := func(v string) {
		v = strings.TrimSpace(v)
		configRepo.SetValue(repository.KeyUploadServerURL, v)
		configRepo.SetValue(repository.KeyManageEndpoint, v)
		configRepo.SetValue(repository.KeyArchiveEndpoint, v)
	}
	if req.ServerEndpoint != nil {
		syncServerEndpoint(*req.ServerEndpoint)
	} else if req.ManageEndpoint != nil {
		syncServerEndpoint(*req.ManageEndpoint)
	} else if req.UploadServerURL != nil {
		syncServerEndpoint(*req.UploadServerURL)
	} else if req.ArchiveEndpoint != nil {
		syncServerEndpoint(*req.ArchiveEndpoint)
	}
	setF := func(key string, v *float64) {
		if v == nil {
			return
		}
		if *v <= 0 || *v > 1.0 {
			return
		}
		configRepo.SetValue(key, strconv.FormatFloat(*v, 'f', -1, 64))
	}
	setF(similarity.CfgKeySameContent, req.SimilaritySameContent)
	setF(similarity.CfgKeyProcessVersion, req.SimilarityProcessVersion)
	setF(similarity.CfgKeyDerived, req.SimilarityDerived)
	setF(similarity.CfgKeyImage, req.SimilarityImage)
	setF(similarity.CfgKeyFileNameSim, req.SimilarityFilename)
	setF(similarity.CfgKeyFeatureSim, req.SimilarityFeature)

	// 数据业务模版 V1 配置（trim 后写入；空字符串也允许，用来清空）
	setS := func(key string, v *string) {
		if v == nil {
			return
		}
		configRepo.SetValue(key, strings.TrimSpace(*v))
	}
	setS(repository.KeyProjectRoot, req.ProjectRoot)
	setS(repository.KeyTemplateServerEndpoint, req.TemplateServerEndpoint) // 模版服务器地址独立存储
	// manage_endpoint / archive_endpoint 已由 syncServerEndpoint 处理；manage_token 已废弃，不再写库

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Configuration saved",
	})
}
