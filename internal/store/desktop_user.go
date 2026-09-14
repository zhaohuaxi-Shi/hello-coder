package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/hello-coder/hello-coder/internal/auth"
	"github.com/hello-coder/hello-coder/internal/model"
	"gorm.io/gorm"
)

const DesktopUsername = "admin"

// EnsureDesktopUser finds or creates the fixed "admin" account used by
// auth-bypass (desktop EXE) mode. Password is random and unused for login.
func EnsureDesktopUser(db *gorm.DB) (uint, string, error) {
	var u model.User
	err := db.Where("username = ?", DesktopUsername).First(&u).Error
	if err == nil {
		return u.ID, u.Username, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, "", fmt.Errorf("query user: %w", err)
	}

	plain := make([]byte, 32)
	if _, err := rand.Read(plain); err != nil {
		return 0, "", fmt.Errorf("rand: %w", err)
	}
	hash, err := auth.HashPassword(hex.EncodeToString(plain))
	if err != nil {
		return 0, "", fmt.Errorf("hash password: %w", err)
	}
	u = model.User{Username: DesktopUsername, Password: hash}
	if err := db.Create(&u).Error; err != nil {
		return 0, "", fmt.Errorf("create admin user: %w", err)
	}
	return u.ID, u.Username, nil
}
