package httpd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"data-asset-scan-go/internal/repository"
)

const defaultManageEndpoint = "http://127.0.0.1:3002"

type authUser struct {
	ID             int64   `json:"id"`
	Username       string  `json:"username"`
	DisplayName    string  `json:"display_name"`
	UserUnit       string  `json:"user_unit"`
	UserDepartment string  `json:"user_department"`
	Phone          *string `json:"phone"`
	Role           string  `json:"role"`
	Status         string  `json:"status"`
	LastLoginTime  *string `json:"last_login_time,omitempty"`
}

type authSession struct {
	Token string   `json:"token"`
	User  authUser `json:"user"`
}

type authRequest struct {
	ManageEndpoint  string  `json:"manage_endpoint"`
	Username        string  `json:"username"`
	Password        string  `json:"password"`
	DisplayName     string  `json:"display_name,omitempty"`
	UserUnit        string  `json:"user_unit,omitempty"`
	UserDepartment  string  `json:"user_department,omitempty"`
	Phone           *string `json:"phone,omitempty"`
	ComputerIP      string  `json:"computer_ip,omitempty"`
	ComputerMAC     string  `json:"computer_mac,omitempty"`
	TerminalVersion string  `json:"terminal_app_version,omitempty"`
}

type manageAuthResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    authSession `json:"data"`
}

// sessionTTL 一次登录的有效期：1 天。过期后（无论是否重启）都需要重新登录。
const sessionTTL = 24 * time.Hour

var currentAuthSession = struct {
	sync.RWMutex
	session   *authSession
	expiresAt time.Time // 零值表示无会话；非零为过期时刻
}{}

// persistedSession 落库格式：在会话基础上多带一个过期时刻（unix 秒）。
type persistedSession struct {
	Token     string   `json:"token"`
	User      authUser `json:"user"`
	ExpiresAt int64    `json:"expires_at"`
}

// activeSession 返回当前有效登录会话；过期则清理并返回 nil；
// 进程内为空时尝试从本地库恢复（关闭终端重开后仍保持登录，只要没过 1 天）。
func activeSession() *authSession {
	now := time.Now()
	currentAuthSession.RLock()
	s := currentAuthSession.session
	exp := currentAuthSession.expiresAt
	currentAuthSession.RUnlock()

	if s != nil {
		if !exp.IsZero() && now.After(exp) {
			expireSession() // 超过 1 天：清内存 + 清库
			return nil
		}
		return s
	}

	// 内存为空：尝试从本地库恢复（loadPersistedSession 内部已校验过期）。
	restored, rexp := loadPersistedSession()
	if restored == nil {
		return nil
	}
	currentAuthSession.Lock()
	if currentAuthSession.session == nil { // 双检：并发下可能已被别的请求恢复
		currentAuthSession.session = restored
		currentAuthSession.expiresAt = rexp
	}
	s = currentAuthSession.session
	currentAuthSession.Unlock()
	return s
}

// setSession 设置当前会话并落库（登录/注册成功后调用），有效期为 now+sessionTTL。
func setSession(s *authSession) {
	exp := time.Now().Add(sessionTTL)
	currentAuthSession.Lock()
	currentAuthSession.session = s
	currentAuthSession.expiresAt = exp
	currentAuthSession.Unlock()
	persistSession(s, exp)
}

// expireSession 清空内存会话与本地库（过期或登出）。
func expireSession() {
	currentAuthSession.Lock()
	currentAuthSession.session = nil
	currentAuthSession.expiresAt = time.Time{}
	currentAuthSession.Unlock()
	clearPersistedSession()
}

// persistSession 把会话+过期时刻落到本地库，供下次启动恢复。失败仅 log，不阻塞登录。
func persistSession(s *authSession, exp time.Time) {
	db := repository.GetDB()
	if db == nil || s == nil {
		return
	}
	b, err := json.Marshal(persistedSession{Token: s.Token, User: s.User, ExpiresAt: exp.Unix()})
	if err != nil {
		log.Printf("[auth] marshal session failed: %v", err)
		return
	}
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyAuthSession, string(b))
}

// loadPersistedSession 从本地库读出上次登录会话与过期时刻；无/损坏/已过期则返回 (nil, zero)。
// 已过期会顺手清库，避免反复尝试。
func loadPersistedSession() (*authSession, time.Time) {
	db := repository.GetDB()
	if db == nil {
		return nil, time.Time{}
	}
	raw := strings.TrimSpace(repository.NewSystemConfigRepository(db).GetValue(repository.KeyAuthSession))
	if raw == "" {
		return nil, time.Time{}
	}
	var p persistedSession
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		log.Printf("[auth] unmarshal persisted session failed: %v", err)
		return nil, time.Time{}
	}
	if p.Token == "" || p.User.Username == "" {
		return nil, time.Time{}
	}
	if p.ExpiresAt > 0 && time.Now().Unix() > p.ExpiresAt {
		clearPersistedSession() // 已过 1 天：清掉，需重新登录
		return nil, time.Time{}
	}
	exp := time.Time{}
	if p.ExpiresAt > 0 {
		exp = time.Unix(p.ExpiresAt, 0)
	}
	return &authSession{Token: p.Token, User: p.User}, exp
}

// clearPersistedSession 清掉本地库里的会话，避免下次启动又恢复。
func clearPersistedSession() {
	if db := repository.GetDB(); db != nil {
		repository.NewSystemConfigRepository(db).SetValue(repository.KeyAuthSession, "")
	}
}

func RegisterAuthRoutes(r *gin.RouterGroup) {
	r.POST("/login", AuthLogin)
	r.POST("/register", AuthRegister)
	r.GET("/session", AuthSession)
	r.POST("/logout", AuthLogout)
	r.GET("/login-history", AuthLoginHistory)
	r.POST("/login-history/delete", AuthLoginHistoryDelete)
}

func AuthLogin(c *gin.Context) {
	forwardAuth(c, "/api/auth/login")
}

func AuthRegister(c *gin.Context) {
	forwardAuth(c, "/api/auth/register")
}

func AuthSession(c *gin.Context) {
	session := activeSession()

	if session == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": gin.H{
				"authenticated": false,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"authenticated": true,
			"token":         session.Token,
			"user":          session.User,
		},
	})
}

func AuthLogout(c *gin.Context) {
	expireSession()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"authenticated": false,
		},
	})
}

func forwardAuth(c *gin.Context, path string) {
	var req authRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid request body"})
		return
	}

	endpoint := resolveManageEndpoint(req.ManageEndpoint)
	enrichTerminalMetadata(&req)

	session, err := callManageAuth(endpoint, path, req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": err.Error()})
		return
	}
	if err := mirrorAuthUser(session.User); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "Failed to mirror authenticated user"})
		return
	}

	// 自动设置 / 创建工作空间目录 + 扫描区域默认值。失败不阻塞登录，仅 log。
	if db := repository.GetDB(); db != nil {
		cfg := repository.NewSystemConfigRepository(db)
		if err := ensureDefaultWorkspaceForUser(cfg, session.User.Username); err != nil {
			log.Printf("[auth] ensureDefaultWorkspaceForUser(%s) failed: %v", session.User.Username, err)
		}
		if err := ensureDefaultScanAreaPath(cfg); err != nil {
			log.Printf("[auth] ensureDefaultScanAreaPath failed: %v", err)
		}
		if err := ensureDefaultControlType(cfg); err != nil {
			log.Printf("[auth] ensureDefaultControlType failed: %v", err)
		}
	}

	setSession(session) // 落内存+本地库，有效期 1 天；关闭终端重开后仍保持登录

	// 记录本机登录历史（含密码，仅存本机），供登录页"快速登录"自动填充。失败不阻塞登录。
	if db := repository.GetDB(); db != nil && strings.TrimSpace(req.Password) != "" {
		_ = repository.NewLoginHistoryRepository(db).Upsert(repository.LoginHistoryEntry{
			Username:       session.User.Username,
			Password:       req.Password,
			DisplayName:    session.User.DisplayName,
			UserUnit:       session.User.UserUnit,
			UserDepartment: session.User.UserDepartment,
			ManageEndpoint: endpoint,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    session,
	})
}

// AuthLoginHistory GET /auth/login-history —— 本机登录过的账号(含密码)，供快速登录。
func AuthLoginHistory(c *gin.Context) {
	db := repository.GetDB()
	if db == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": []any{}})
		return
	}
	list, err := repository.NewLoginHistoryRepository(db).List()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": list})
}

// AuthLoginHistoryDelete POST /auth/login-history/delete {username} —— 移除某条快速登录项。
func AuthLoginHistoryDelete(c *gin.Context) {
	var body struct {
		Username string `json:"username"`
	}
	_ = c.ShouldBindJSON(&body)
	if strings.TrimSpace(body.Username) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 username"})
		return
	}
	if db := repository.GetDB(); db != nil {
		_ = repository.NewLoginHistoryRepository(db).Delete(body.Username)
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func resolveManageEndpoint(override string) string {
	if endpoint := strings.TrimSpace(override); endpoint != "" {
		return strings.TrimRight(endpoint, "/")
	}
	if db := repository.GetDB(); db != nil {
		configRepo := repository.NewSystemConfigRepository(db)
		if endpoint := strings.TrimSpace(configRepo.GetValue(repository.KeyManageEndpoint)); endpoint != "" {
			return strings.TrimRight(endpoint, "/")
		}
	}
	return defaultManageEndpoint
}

func enrichTerminalMetadata(req *authRequest) {
	if strings.TrimSpace(req.ComputerIP) == "" {
		req.ComputerIP = repository.GetLocalIP()
	}
	if strings.TrimSpace(req.ComputerIP) == "" {
		req.ComputerIP = "127.0.0.1"
	}
	if strings.TrimSpace(req.ComputerMAC) == "" {
		req.ComputerMAC = repository.GetLocalMAC()
	}
	if strings.TrimSpace(req.ComputerMAC) == "" {
		req.ComputerMAC = "00:00:00:00:00:00"
	}
	if strings.TrimSpace(req.TerminalVersion) == "" {
		req.TerminalVersion = "data-asset-scan-go"
	}
}

func callManageAuth(endpoint, path string, req authRequest) (*authSession, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Post(endpoint+path, "application/json", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var decoded manageAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices || decoded.Code != 0 {
		if decoded.Message == "" {
			decoded.Message = fmt.Sprintf("manage auth failed: status %d", resp.StatusCode)
		}
		return nil, errors.New(decoded.Message)
	}
	if decoded.Data.Token == "" || decoded.Data.User.Username == "" {
		return nil, fmt.Errorf("manage auth response missing token or user")
	}
	return &decoded.Data, nil
}

func mirrorAuthUser(user authUser) error {
	repo := repository.NewUserInfoRepository(repository.GetDB())
	return repo.MirrorManagedAuthUser(repository.ManagedAuthUser{
		Username:       user.Username,
		DisplayName:    user.DisplayName,
		UserUnit:       user.UserUnit,
		UserDepartment: user.UserDepartment,
		Phone:          user.Phone,
	})
}
