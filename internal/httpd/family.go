package httpd

import (
	"net/http"
	"strconv"

	"data-asset-scan-go/internal/models"
	"data-asset-scan-go/internal/repository"

	"github.com/gin-gonic/gin"
)

func RegisterFamilyRoutes(r *gin.RouterGroup) {
	r.GET("/needs-arbitration", GetFamilyNeedsArbitrationCount)
	r.POST("/batch-members", GetBatchFamilyMembers)
	r.GET("/:id", GetFamily)
	r.GET("/:id/members", GetFamilyMembers)
	r.POST("/:id/authoritative", SetFamilyAuthoritative)
}

// GetFamily returns the family row for the given id.
func GetFamily(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	repo := repository.NewFamilyRepository(repository.GetDB())
	row, err := repo.GetFamilyByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "family not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": row})
}

// FamilyMembersResponse groups members by relation for the UI.
type FamilyMembersResponse struct {
	FamilyID        int64                                      `json:"family_id"`
	PrimaryResource *models.DataResources                      `json:"primary_resource"`
	Groups          map[string][]repository.FamilyMemberDetail `json:"groups"`
	TotalMembers    int                                        `json:"total_members"`
}

// GetFamilyMembers returns the data_resources rows in the family, grouped by
// relation (primary / same_content / process_version / derived).
func GetFamilyMembers(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	db := repository.GetDB()
	famRepo := repository.NewFamilyRepository(db)

	members, err := famRepo.ListFamilyMembers(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	groups := map[string][]repository.FamilyMemberDetail{
		"primary":         {},
		"same_content":    {},
		"process_version": {},
		"derived":         {},
	}
	for _, m := range members {
		key := "derived"
		if m.FamilyRelation != nil {
			key = *m.FamilyRelation
		}
		if _, ok := groups[key]; !ok {
			groups[key] = []repository.FamilyMemberDetail{}
		}
		groups[key] = append(groups[key], m)
	}

	// Look up the primary's full DataResources row for richer display.
	var primary *models.DataResources
	if len(groups["primary"]) > 0 {
		var p models.DataResources
		if err := db.Get(&p, `SELECT * FROM data_resources WHERE data_resources_id = ? AND disable = 0`,
			groups["primary"][0].DataResourcesID); err == nil {
			primary = &p
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": FamilyMembersResponse{
			FamilyID:        id,
			PrimaryResource: primary,
			Groups:          groups,
			TotalMembers:    len(members),
		},
	})
}

// GetFamilyNeedsArbitrationCount GET /family/needs-arbitration
//
// 返回当前还需要人工裁定权威源的"重要级 family"数量。
// 重要级 = importance_level=2，且家族至少 2 成员且未确权且未驳回。
func GetFamilyNeedsArbitrationCount(c *gin.Context) {
	var count int
	err := repository.GetDB().Get(&count, `
		SELECT COUNT(DISTINCT f.family_id)
		  FROM data_resource_family f
		  JOIN data_resources dr ON dr.family_id = f.family_id
		 WHERE f.authoritative_resource_id IS NULL
		   AND f.member_count >= 2
		   AND f.disable = 0
		   AND dr.disable = 0
		   AND dr.importance_level = 2
		   AND dr.claim_status = 2
		   AND dr.ai_classify_rejected_at IS NULL
	`)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"count": count}})
}

// BatchFamilyMembersRequest is the request body for POST /family/batch-members.
type BatchFamilyMembersRequest struct {
	ContentSigns []string `json:"content_signs"`
}

// GetBatchFamilyMembers POST /family/batch-members
// 给前端批量场景一次性拉取多个 content_sign 对应的 family 成员，避免 N+1。
// 无 family 的 content_sign 不会出现在返回 map 里。
func GetBatchFamilyMembers(c *gin.Context) {
	var req BatchFamilyMembersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	repo := repository.NewFamilyRepository(repository.GetDB())
	result, err := repo.BatchListFamilyMembersByContentSigns(req.ContentSigns)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// SetFamilyAuthoritative POST /family/:id/authoritative body {"resource_id": int64}
func SetFamilyAuthoritative(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid family id"})
		return
	}
	var body struct {
		ResourceID int64 `json:"resource_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.ResourceID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "resource_id 必填"})
		return
	}
	if err := repository.SetAuthoritativeResource(repository.GetDB(), id, body.ResourceID); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"family_id":                 id,
		"authoritative_resource_id": body.ResourceID,
	}})
}
