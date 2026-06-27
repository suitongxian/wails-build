package httpd

import (
	"net/http"
	"os"
	"path/filepath"

	"data-asset-scan-go/internal/models"
	"data-asset-scan-go/internal/repository"
	"github.com/gin-gonic/gin"
)

// RegisterUserRoutes registers /user-info routes
func RegisterUserRoutes(r *gin.RouterGroup) {
	r.GET("", GetUserInfo)
	r.POST("", UpdateUserInfo)
	r.GET("/workspace", GetWorkspace)
	r.PUT("/workspace", UpdateWorkspace)
	r.GET("/workspace/exists", WorkspaceExists)
	r.POST("/workspace", CreateWorkspace)
	r.DELETE("/workspace", DeleteWorkspace)
	r.GET("/workspace/.os/*path", GetOSCompatiblePath)
}

// UserInfoResponse represents the user info response
type UserInfoResponse struct {
	ID          int64   `json:"id"`
	CompanyName string  `json:"company_name"`
	UserName    string  `json:"user_name"`
	Department  string  `json:"department"`
	IP          string  `json:"ip"`
	MacAddress  string  `json:"mac_address"`
	WorkAddress *string `json:"work_address"`
	Phone       *string `json:"phone"`
	PasswordMD5 *string `json:"password_md5"`
	IDCard      *string `json:"id_card"`
	CreateTime  string  `json:"create_time"`
	UpdateTime  string  `json:"update_time"`
	Disable     int     `json:"disable"`
}

// GetUserInfo handles GET /user_info
func GetUserInfo(c *gin.Context) {
	userInfoRepo := repository.NewUserInfoRepository(repository.GetDB())

	userInfo, err := userInfoRepo.GetActiveUser()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   "Failed to get user info",
		})
		return
	}

	if userInfo == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data":    nil,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": UserInfoResponse{
			ID:          userInfo.ID,
			CompanyName: userInfo.CompanyName,
			UserName:    userInfo.UserName,
			Department:  userInfo.Department,
			IP:          userInfo.IP,
			MacAddress:  userInfo.MacAddress,
			WorkAddress: userInfo.WorkAddress,
			Phone:       userInfo.Phone,
			PasswordMD5: userInfo.PasswordMD5,
			IDCard:      userInfo.IDCard,
			CreateTime:  userInfo.CreateTime.Format("2006-01-02 15:04:05"),
			UpdateTime:  userInfo.UpdateTime.Format("2006-01-02 15:04:05"),
			Disable:     userInfo.Disable,
		},
	})
}

// UpdateUserInfoRequest represents the request body for updating user info
type UpdateUserInfoRequest struct {
	CompanyName string  `json:"company_name"`
	UserName    string  `json:"user_name"`
	Department  string  `json:"department"`
	Phone       *string `json:"phone"`
	WorkAddress *string `json:"work_address"`
}

// UpdateUserInfo handles PUT /user_info
func UpdateUserInfo(c *gin.Context) {
	var req UpdateUserInfoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request body",
		})
		return
	}

	// Validate required fields
	if req.CompanyName == "" || req.UserName == "" || req.Department == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Missing required fields: company_name, user_name, department",
		})
		return
	}

	userInfoRepo := repository.NewUserInfoRepository(repository.GetDB())

	userInfo, err := userInfoRepo.Save(models.CreateUserInfoParams{
		CompanyName: req.CompanyName,
		UserName:    req.UserName,
		Department:  req.Department,
		Phone:       req.Phone,
		WorkAddress: req.WorkAddress,
	})

	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   "Failed to save user info",
		})
		return
	}

	// V2-1 后续：user_info 写入后立刻同步到 users 表，保证两表始终一致。
	// 失败仅 log（不阻塞主流程，避免影响 V1 行为）。
	usersRepo := repository.NewUserRepository(repository.GetDB())
	if _, syncErr := usersRepo.UpsertFromUserInfo(userInfo); syncErr != nil {
		// 用 c.Error 记录，不打断响应
		_ = c.Error(syncErr)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "用户信息已保存",
		"data": UserInfoResponse{
			ID:          userInfo.ID,
			CompanyName: userInfo.CompanyName,
			UserName:    userInfo.UserName,
			Department:  userInfo.Department,
			IP:          userInfo.IP,
			MacAddress:  userInfo.MacAddress,
			WorkAddress: userInfo.WorkAddress,
			Phone:       userInfo.Phone,
			PasswordMD5: userInfo.PasswordMD5,
			IDCard:      userInfo.IDCard,
			CreateTime:  userInfo.CreateTime.Format("2006-01-02 15:04:05"),
			UpdateTime:  userInfo.UpdateTime.Format("2006-01-02 15:04:05"),
			Disable:     userInfo.Disable,
		},
	})
}

// WorkspaceInfo represents workspace info
type WorkspaceInfo struct {
	Path         string `json:"path"`
	Exists       bool   `json:"exists"`
	LastScanTime string `json:"last_scan_time"`
}

// GetWorkspace handles GET /user_info/workspace
func GetWorkspace(c *gin.Context) {
	configRepo := repository.NewSystemConfigRepository(repository.GetDB())

	workspacePath := configRepo.GetWorkspace()
	lastScanTime := configRepo.GetLastScanTime()

	exists := false
	if workspacePath != "" {
		if info, err := os.Stat(workspacePath); err == nil {
			exists = info.IsDir()
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": WorkspaceInfo{
			Path:         workspacePath,
			Exists:       exists,
			LastScanTime: lastScanTime,
		},
	})
}

// UpdateWorkspaceRequest represents the request body for updating workspace
type UpdateWorkspaceRequest struct {
	Path string `json:"path"`
}

// UpdateWorkspace handles PUT /user_info/workspace
func UpdateWorkspace(c *gin.Context) {
	var req UpdateWorkspaceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request body",
		})
		return
	}

	if req.Path == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Missing required field: path",
		})
		return
	}

	// Verify the directory exists
	if info, err := os.Stat(req.Path); err != nil || !info.IsDir() {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid directory path",
		})
		return
	}

	configRepo := repository.NewSystemConfigRepository(repository.GetDB())
	configRepo.SetWorkspace(req.Path)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Workspace updated",
	})
}

// WorkspaceExists handles GET /user_info/workspace/exists
func WorkspaceExists(c *gin.Context) {
	configRepo := repository.NewSystemConfigRepository(repository.GetDB())

	workspacePath := configRepo.GetWorkspace()
	exists := false
	if workspacePath != "" {
		if info, err := os.Stat(workspacePath); err == nil {
			exists = info.IsDir()
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"exists": exists,
		},
	})
}

// CreateWorkspaceRequest represents the request body for creating workspace
type CreateWorkspaceRequest struct {
	Path string `json:"path"`
}

// CreateWorkspace handles POST /user_info/workspace
func CreateWorkspace(c *gin.Context) {
	var req CreateWorkspaceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request body",
		})
		return
	}

	if req.Path == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Missing required field: path",
		})
		return
	}

	// Create the directory if it doesn't exist
	if err := os.MkdirAll(req.Path, 0755); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   "Failed to create workspace directory",
		})
		return
	}

	configRepo := repository.NewSystemConfigRepository(repository.GetDB())
	configRepo.SetWorkspace(req.Path)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Workspace created",
	})
}

// DeleteWorkspace handles DELETE /user_info/workspace
func DeleteWorkspace(c *gin.Context) {
	configRepo := repository.NewSystemConfigRepository(repository.GetDB())
	workspacePath := configRepo.GetWorkspace()

	if workspacePath == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   "Workspace path not set",
		})
		return
	}

	// Check if directory exists
	if _, err := os.Stat(workspacePath); os.IsNotExist(err) {
		// Directory doesn't exist, just clear the config
		configRepo.SetWorkspace("")
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "Workspace deleted",
		})
		return
	}

	// Remove the directory
	if err := os.RemoveAll(workspacePath); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   "Failed to delete workspace directory",
		})
		return
	}

	configRepo.SetWorkspace("")

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Workspace deleted",
	})
}

// GetOSCompatiblePath handles GET /user_info/workspace/.os/*path
func GetOSCompatiblePath(c *gin.Context) {
	path := c.Param("path")

	// Convert path to OS-compatible format
	osPath := filepath.FromSlash(path)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"path": osPath,
		},
	})
}
