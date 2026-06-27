package httpd

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"data-asset-scan-go/internal/repository"
)

// RegisterSubjectsRoutes registers the read-only subject lookup used by scan.
func RegisterSubjectsRoutes(r *gin.RouterGroup) {
	r.GET("", ListSubjects)
}

// ListSubjects GET /subjects?type=&keyword=
func ListSubjects(c *gin.Context) {
	db := repository.GetDB()
	configRepo := repository.NewSystemConfigRepository(db)
	if configRepo.GetValue(repository.KeyManageEndpoint) == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "未配置管理平台地址，无法从 manage 拉取三主体主数据"})
		return
	}
	if _, err := repository.NewSubjectSyncer(db, configRepo).SyncAll(); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	repo := repository.NewSubjectRepository(repository.GetDB())
	filterType := c.Query("type")
	keyword := c.Query("keyword")
	includeSystem := c.Query("include_system") == "true" || c.Query("include_system") == "1"
	list, err := repo.ListWithOptions(filterType, keyword, includeSystem)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": list})
}
