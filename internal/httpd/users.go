package httpd

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"data-asset-scan-go/internal/repository"
)

// RegisterUsersRoutes registers read-only registered user lookup routes.
func RegisterUsersRoutes(r *gin.RouterGroup) {
	r.GET("", ListUsers)
}

// ListUsers GET /users
func ListUsers(c *gin.Context) {
	_ = syncUsersFromManage(c)
	repo := repository.NewUserRepository(repository.GetDB())
	list, err := repo.List()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": list})
}

type manageAuthUserProfile struct {
	Username       string  `json:"username"`
	DisplayName    string  `json:"display_name"`
	UserUnit       string  `json:"user_unit"`
	UserDepartment string  `json:"user_department"`
	Role           string  `json:"role"`
	Phone          *string `json:"phone"`
	Status         string  `json:"status"`
}

type manageAuthUserListResponse struct {
	Code    int                     `json:"code"`
	Message string                  `json:"message"`
	Data    []manageAuthUserProfile `json:"data"`
}

func syncUsersFromManage(c *gin.Context) error {
	configRepo := repository.NewSystemConfigRepository(repository.GetDB())
	endpoint := strings.TrimRight(strings.TrimSpace(effectiveManageEndpoint(configRepo)), "/")
	if endpoint == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, endpoint+"/api/auth-users/list?status=active", nil)
	if err != nil {
		return err
	}
	// manage_token 已废弃，不再发送 Authorization 头
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("manage users status " + resp.Status)
	}
	var raw manageAuthUserListResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return err
	}
	if raw.Code != 0 {
		return errors.New(raw.Message)
	}
	userRepo := repository.NewUserRepository(repository.GetDB())
	for _, u := range raw.Data {
		if u.Status != "" && u.Status != "active" {
			continue
		}
		_, _ = userRepo.UpsertManagedAuthUser(repository.ManagedAuthUser{
			Username:       u.Username,
			DisplayName:    u.DisplayName,
			UserUnit:       u.UserUnit,
			UserDepartment: u.UserDepartment,
			Role:           u.Role,
			Phone:          u.Phone,
		})
	}
	return nil
}
