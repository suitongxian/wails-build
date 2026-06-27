package httpd

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"data-asset-scan-go/internal/repository"
)

func RegisterMemorandumRoutes(r *gin.RouterGroup) {
	r.GET("/pending", ListMemorandumPending)
	r.GET("/registered", ListMemorandumRegistered)
	r.POST("/register", RegisterMemorandum)
}

type memorandumItem struct {
	LedgerID        int64   `db:"id" json:"ledger_id"`
	AssetName       string  `db:"asset_name" json:"asset_name"`
	FileVersionCode string  `db:"file_version_code" json:"file_version_code"`
	CreateTime      string  `db:"create_time" json:"create_time"`
	Topic           *string `db:"memorandum_topic" json:"topic"`
	Classification  *string `db:"memorandum_classification" json:"classification"`
	RegisteredAt    *string `db:"memorandum_registered_at" json:"registered_at"`
	RegisteredBy    *int64  `db:"memorandum_registered_by" json:"registered_by"`
}

// ListMemorandumPending GET /memorandum/pending?page=&page_size=
func ListMemorandumPending(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	listMemorandum(c, page, pageSize, false)
}

// ListMemorandumRegistered GET /memorandum/registered?page=&page_size=
func ListMemorandumRegistered(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	listMemorandum(c, page, pageSize, true)
}

func listMemorandum(c *gin.Context, page, pageSize int, registered bool) {
	db := repository.GetDB()
	// 过滤掉项目初始化时建的 'planned' 占位 ledger（模板的每个 file_rule 都会建
	// 一条 placeholder，没有真实文件挂账），它们不应出现在核心登记待登记列表里。
	where := `project_code = 'SYS-PERSONAL-CORE' AND disable = 0
		  AND lifecycle_status != 'planned'`
	if registered {
		where += ` AND memorandum_registered_at IS NOT NULL`
	} else {
		where += ` AND memorandum_registered_at IS NULL`
	}

	var total int
	if err := db.Get(&total, `SELECT COUNT(*) FROM asset_ledgers WHERE `+where); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	var items []memorandumItem
	if err := db.Select(&items,
		`SELECT id, asset_name, file_version_code, create_time,
		        memorandum_topic, memorandum_classification, memorandum_registered_at, memorandum_registered_by
		 FROM asset_ledgers WHERE `+where+`
		 ORDER BY id DESC LIMIT ? OFFSET ?`, pageSize, (page-1)*pageSize); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}})
}

type registerRequest struct {
	LedgerID       int64  `json:"ledger_id"`
	Topic          string `json:"topic"`
	Classification string `json:"classification"`
	Note           string `json:"note"`
	Password       string `json:"password"`
}

// RegisterMemorandum POST /memorandum/register
func RegisterMemorandum(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	if req.LedgerID <= 0 || req.Topic == "" || req.Classification == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "ledger_id / topic / classification / password 全部必填"})
		return
	}
	db := repository.GetDB()

	var info struct {
		ProjectCode string  `db:"project_code"`
		Registered  *string `db:"memorandum_registered_at"`
	}
	if err := db.Get(&info, `SELECT project_code, memorandum_registered_at FROM asset_ledgers WHERE id = ? AND disable = 0`, req.LedgerID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "ledger 不存在"})
		return
	}
	if info.ProjectCode != repository.PersonalCoreProjectCode {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "非核心级 ledger 不可进行核心登记"})
		return
	}
	if info.Registered != nil && *info.Registered != "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "该 ledger 已登记，不可重复"})
		return
	}

	operator := currentOperator(c)
	var stored *string
	_ = db.Get(&stored, `SELECT password_md5 FROM user_info WHERE user_name = ? AND disable = 0 ORDER BY id DESC LIMIT 1`, operator)
	if stored == nil || *stored == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "用户未设密码，无法签字"})
		return
	}
	if md5Hex(req.Password) != *stored {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "密码错误"})
		return
	}

	now := time.Now()
	userID := currentUserID(c)
	sigPayload := strconv.FormatInt(userID, 10) + ":" + now.Format(time.RFC3339Nano) + ":" + *stored
	sigHash := sha256Hex(sigPayload)

	if _, err := db.Exec(`UPDATE asset_ledgers SET
		memorandum_topic = ?, memorandum_classification = ?, memorandum_registered_at = ?,
		memorandum_registered_by = ?, memorandum_signature_hash = ?, update_time = ?
		WHERE id = ?`,
		req.Topic, req.Classification, now, userID, sigHash, now, req.LedgerID); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	auditRepo := repository.NewAuditLogRepository(db)
	_, _ = auditRepo.Append(repository.AppendAuditInput{
		ActorID:     operator,
		ActorUserID: userID,
		Action:      "core_memorandum_register",
		TargetType:  "asset_ledger",
		TargetID:    req.LedgerID,
		After:       gin.H{"topic": req.Topic, "classification": req.Classification, "note": req.Note, "registered_at": now},
		IPAddress:   c.ClientIP(),
		Message:     "核心级资料登记: " + req.Topic,
	})

	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"ledger_id": req.LedgerID, "registered_at": now}})
}

func md5Hex(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
