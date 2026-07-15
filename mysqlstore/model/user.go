package model

import "time"

// User is the persistence representation of an account.
type User struct {
	ID           uint64    `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`
	Email        string    `gorm:"column:email;type:varchar(320);not null"`
	DisplayName  string    `gorm:"column:display_name;type:varchar(100);not null"`
	PasswordHash string    `gorm:"column:password_hash;type:varchar(255);not null"`
	Status       string    `gorm:"column:status;type:varchar(16);not null"`
	Version      uint64    `gorm:"column:version;type:bigint unsigned;not null"`
	CreatedAt    time.Time `gorm:"column:created_at;type:datetime(6);not null"`
	UpdatedAt    time.Time `gorm:"column:updated_at;type:datetime(6);not null"`
}

func (User) TableName() string { return "users" }

// Role is a stable authorization role.
type Role struct {
	ID          uint64    `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`
	Name        string    `gorm:"column:name;type:varchar(64);not null"`
	Description *string   `gorm:"column:description;type:varchar(255)"`
	CreatedAt   time.Time `gorm:"column:created_at;type:datetime(6);not null"`
	UpdatedAt   time.Time `gorm:"column:updated_at;type:datetime(6);not null"`
}

func (Role) TableName() string { return "roles" }

// Permission is a stable, globally addressable authorization capability.
type Permission struct {
	ID          uint64    `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`
	Name        string    `gorm:"column:name;type:varchar(100);not null"`
	Description *string   `gorm:"column:description;type:varchar(255)"`
	CreatedAt   time.Time `gorm:"column:created_at;type:datetime(6);not null"`
	UpdatedAt   time.Time `gorm:"column:updated_at;type:datetime(6);not null"`
}

func (Permission) TableName() string { return "permissions" }

// UserRole maps an account to a role.
type UserRole struct {
	UserID     uint64    `gorm:"column:user_id;type:bigint unsigned;primaryKey;autoIncrement:false"`
	RoleID     uint64    `gorm:"column:role_id;type:bigint unsigned;primaryKey;autoIncrement:false"`
	AssignedAt time.Time `gorm:"column:assigned_at;type:datetime(6);not null"`
}

func (UserRole) TableName() string { return "user_roles" }

// RolePermission maps a role to a permission.
type RolePermission struct {
	RoleID       uint64    `gorm:"column:role_id;type:bigint unsigned;primaryKey;autoIncrement:false"`
	PermissionID uint64    `gorm:"column:permission_id;type:bigint unsigned;primaryKey;autoIncrement:false"`
	GrantedAt    time.Time `gorm:"column:granted_at;type:datetime(6);not null"`
}

func (RolePermission) TableName() string { return "role_permissions" }

// Session persists the lifecycle of a refresh-token family.
type Session struct {
	ID              string     `gorm:"column:id;type:char(22);primaryKey"`
	UserID          uint64     `gorm:"column:user_id;type:bigint unsigned;not null"`
	Rotation        uint64     `gorm:"column:rotation;type:bigint unsigned;not null"`
	ExpiresAt       time.Time  `gorm:"column:expires_at;type:datetime(6);not null"`
	RevokedAt       *time.Time `gorm:"column:revoked_at;type:datetime(6)"`
	ReuseDetectedAt *time.Time `gorm:"column:reuse_detected_at;type:datetime(6)"`
	CreatedAt       time.Time  `gorm:"column:created_at;type:datetime(6);not null"`
	UpdatedAt       time.Time  `gorm:"column:updated_at;type:datetime(6);not null"`
}

func (Session) TableName() string { return "sessions" }

// RefreshToken retains every issued digest, including consumed tokens, because
// observing any older digest again must revoke the entire token family as reuse.
type RefreshToken struct {
	ID           string     `gorm:"column:id;type:char(22);primaryKey"`
	SessionID    string     `gorm:"column:session_id;type:char(22);not null"`
	Digest       []byte     `gorm:"column:digest;type:binary(32);size:32;not null"`
	ExpiresAt    time.Time  `gorm:"column:expires_at;type:datetime(6);not null"`
	ConsumedAt   *time.Time `gorm:"column:consumed_at;type:datetime(6)"`
	RevokedAt    *time.Time `gorm:"column:revoked_at;type:datetime(6)"`
	ReplacedByID *string    `gorm:"column:replaced_by_id;type:char(22)"`
	CreatedAt    time.Time  `gorm:"column:created_at;type:datetime(6);not null"`
	UpdatedAt    time.Time  `gorm:"column:updated_at;type:datetime(6);not null"`
}

func (RefreshToken) TableName() string { return "refresh_tokens" }
