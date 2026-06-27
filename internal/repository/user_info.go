package repository

import (
	"net"
	"strings"
	"time"

	"data-asset-scan-go/internal/models"
	"github.com/jmoiron/sqlx"
)

// UserInfoRepository handles database operations for user_info table
type UserInfoRepository struct {
	DB *sqlx.DB
}

type ManagedAuthUser struct {
	Username       string
	DisplayName    string
	UserUnit       string
	UserDepartment string
	Role           string
	Phone          *string
}

// NewUserInfoRepository creates a new UserInfoRepository instance
func NewUserInfoRepository(db *sqlx.DB) *UserInfoRepository {
	return &UserInfoRepository{DB: db}
}

// GetActiveUser retrieves the current active user (only one active record)
func (r *UserInfoRepository) GetActiveUser() (*models.UserInfo, error) {
	var user models.UserInfo
	query := `SELECT * FROM user_info WHERE disable = 0 ORDER BY id DESC LIMIT 1`
	err := r.DB.Get(&user, query)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

// GetByID retrieves a user by ID
func (r *UserInfoRepository) GetByID(id int64) (*models.UserInfo, error) {
	var user models.UserInfo
	query := `SELECT * FROM user_info WHERE id = ? AND disable = 0`
	err := r.DB.Get(&user, query, id)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

// Create creates a new user record
func (r *UserInfoRepository) Create(params models.CreateUserInfoParams) (*models.UserInfo, error) {
	now := time.Now()
	ip := getLocalIP()
	mac := getLocalMAC()

	query := `
		INSERT INTO user_info (company_name, user_name, department, ip, mac_address, work_address, phone, create_time, update_time, disable)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
	`
	result, err := r.DB.Exec(query,
		params.CompanyName,
		params.UserName,
		params.Department,
		ip,
		mac,
		params.WorkAddress,
		params.Phone,
		now,
		now,
	)
	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	return r.GetByID(id)
}

// Update updates an existing user record
func (r *UserInfoRepository) Update(id int64, params models.UpdateUserInfoParams) (*models.UserInfo, error) {
	existing, err := r.GetByID(id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, nil
	}

	now := time.Now()
	ip := getLocalIP()
	mac := getLocalMAC()

	// Apply defaults for nil values
	companyName := existing.CompanyName
	if params.CompanyName != nil {
		companyName = *params.CompanyName
	}
	userName := existing.UserName
	if params.UserName != nil {
		userName = *params.UserName
	}
	department := existing.Department
	if params.Department != nil {
		department = *params.Department
	}
	phone := existing.Phone
	if params.Phone != nil {
		phone = params.Phone
	}
	workAddress := existing.WorkAddress
	if params.WorkAddress != nil {
		workAddress = params.WorkAddress
	}

	query := `
		UPDATE user_info
		SET company_name = ?, user_name = ?, department = ?, ip = ?, mac_address = ?,
		    work_address = ?, phone = ?, update_time = ?
		WHERE id = ? AND disable = 0
	`
	_, err = r.DB.Exec(query,
		companyName,
		userName,
		department,
		ip,
		mac,
		workAddress,
		phone,
		now,
		id,
	)
	if err != nil {
		return nil, err
	}

	return r.GetByID(id)
}

// Save saves user info (creates or updates)
func (r *UserInfoRepository) Save(params models.CreateUserInfoParams) (*models.UserInfo, error) {
	existing, err := r.GetActiveUser()
	if err != nil {
		return nil, err
	}
	if existing != nil {
		updateParams := models.UpdateUserInfoParams{
			CompanyName: &params.CompanyName,
			UserName:    &params.UserName,
			Department:  &params.Department,
			Phone:       params.Phone,
			WorkAddress: params.WorkAddress,
		}
		return r.Update(existing.ID, updateParams)
	}
	return r.Create(params)
}

// MirrorManagedAuthUser mirrors the authoritative manage account into local
// compatibility tables. user_info keeps the visible owner name, while users
// keeps the stable login username from manage.
func (r *UserInfoRepository) MirrorManagedAuthUser(user ManagedAuthUser) error {
	user.Username = strings.TrimSpace(user.Username)
	user.DisplayName = strings.TrimSpace(user.DisplayName)
	if user.Username == "" {
		return nil
	}
	if user.DisplayName == "" {
		user.DisplayName = user.Username
	}

	now := time.Now()
	ip := getLocalIP()
	if ip == "" {
		ip = "127.0.0.1"
	}
	mac := getLocalMAC()
	if mac == "" {
		mac = "00:00:00:00:00:00"
	}

	existing, err := r.GetActiveUser()
	if err != nil {
		return err
	}
	if existing != nil {
		if _, err := r.DB.Exec(`UPDATE user_info
			SET company_name = ?, user_name = ?, department = ?, ip = ?, mac_address = ?,
			    phone = ?, update_time = ?
			WHERE id = ? AND disable = 0`,
			user.UserUnit, user.DisplayName, user.UserDepartment, ip, mac, user.Phone, now, existing.ID); err != nil {
			return err
		}
	} else {
		if _, err := r.DB.Exec(`INSERT INTO user_info (
			company_name, user_name, department, ip, mac_address, phone, create_time, update_time, disable
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			user.UserUnit, user.DisplayName, user.UserDepartment, ip, mac, user.Phone, now, now); err != nil {
			return err
		}
	}

	usersRepo := NewUserRepository(r.DB)
	_, err = usersRepo.UpsertManagedAuthUser(user)
	return err
}

// HasActiveUser checks if there is an active user
func (r *UserInfoRepository) HasActiveUser() (bool, error) {
	user, err := r.GetActiveUser()
	if err != nil {
		return false, err
	}
	return user != nil, nil
}

// GetLocalIP returns the local IP address (exported for cross-package use).
func GetLocalIP() string { return getLocalIP() }

// GetLocalMAC returns the local MAC address (exported for cross-package use).
func GetLocalMAC() string { return getLocalMAC() }

// getLocalIP returns the local IP address
func getLocalIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagLoopback == 0 && len(iface.HardwareAddr) > 0 {
			addrs, _ := iface.Addrs()
			for _, addr := range addrs {
				if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
					return ipNet.IP.String()
				}
			}
		}
	}
	return ""
}

// getLocalMAC returns the local MAC address
func getLocalMAC() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagLoopback == 0 && len(iface.HardwareAddr) > 0 {
			return iface.HardwareAddr.String()
		}
	}
	return ""
}
