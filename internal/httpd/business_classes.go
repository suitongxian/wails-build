package httpd

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"data-asset-scan-go/internal/repository"
)

// RegisterBusinessClassesRoutes 注册 /business-classes 路由（行业分类本地创作 CRUD）。
//
// 2026-05-31 模版创作迁到 scan：行业分类可在 scan 本地增删改查，编码全自动生成。
func RegisterBusinessClassesRoutes(r *gin.RouterGroup) {
	r.GET("", ListBusinessClasses)
	r.POST("", CreateBusinessClass)
	r.PUT("/:id", UpdateBusinessClass)
	r.DELETE("/:id", DeleteBusinessClass)
}

type businessClassBody struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ListBusinessClasses GET /business-classes
func ListBusinessClasses(c *gin.Context) {
	repo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	list, err := repo.ListBusinessClasses()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": list})
}

// CreateBusinessClass POST /business-classes
func CreateBusinessClass(c *gin.Context) {
	var body businessClassBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid body"})
		return
	}
	repo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	bc, err := repo.CreateBusinessClass(body.Name, body.Description)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": bc})
}

// UpdateBusinessClass PUT /business-classes/:id
func UpdateBusinessClass(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	var body businessClassBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid body"})
		return
	}
	repo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	if err := repo.UpdateBusinessClass(id, body.Name, body.Description); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// DeleteBusinessClass DELETE /business-classes/:id
func DeleteBusinessClass(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	repo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	if err := repo.DeleteBusinessClass(id); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
