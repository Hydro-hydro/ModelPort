package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

var (
	ErrPersonalOwnerNotFound  = errors.New("personal owner not found")
	ErrPersonalOwnerAmbiguous = errors.New("personal owner is ambiguous")
	ErrPersonalOwnerDisabled  = errors.New("personal owner is disabled")
	ErrPersonalOwnerMismatch  = errors.New("request is not from the personal owner")
)

// EnsurePersonalOwner validates the single root administrator required by
// personal mode. It is deliberately read-only: a non-empty database that does
// not already contain exactly one enabled root is rejected instead of being
// repaired or promoted as part of a legacy migration.
func EnsurePersonalOwner() error {
	if DB == nil {
		return fmt.Errorf("database is nil")
	}

	err := DB.Transaction(func(tx *gorm.DB) error {
		var administrators []User
		if err := lockForUpdate(tx).Where("role >= ?", common.RoleAdminUser).Order("id ASC").Find(&administrators).Error; err != nil {
			return err
		}

		switch len(administrators) {
		case 0:
			var userCount int64
			if err := tx.Model(&User{}).Count(&userCount).Error; err != nil {
				return err
			}
			if userCount == 0 {
				return nil
			}
			return ErrPersonalOwnerNotFound
		case 1:
			owner := administrators[0]
			if owner.Role != common.RoleRootUser {
				return ErrPersonalOwnerNotFound
			}
			if owner.Status != common.UserStatusEnabled {
				return ErrPersonalOwnerDisabled
			}
			return nil
		default:
			return ErrPersonalOwnerAmbiguous
		}
	})
	if err != nil {
		return err
	}
	return nil
}

// GetPersonalOwner returns the single enabled root account selected by
// EnsurePersonalOwner. It deliberately rejects multiple root/administrator
// records so login never chooses an account nondeterministically.
func GetPersonalOwner() (*User, error) {
	if DB == nil {
		return nil, fmt.Errorf("database is nil")
	}

	var administrators []User
	if err := DB.Where("role >= ?", common.RoleAdminUser).Order("id ASC").Find(&administrators).Error; err != nil {
		return nil, err
	}
	if len(administrators) == 0 {
		return nil, ErrPersonalOwnerNotFound
	}
	if len(administrators) > 1 {
		return nil, ErrPersonalOwnerAmbiguous
	}
	if administrators[0].Role != common.RoleRootUser {
		return nil, ErrPersonalOwnerNotFound
	}
	if administrators[0].Status != common.UserStatusEnabled {
		return nil, ErrPersonalOwnerDisabled
	}
	return &administrators[0], nil
}

// ValidatePersonalOwner verifies that userID is the single account selected as
// the personal edition owner. It is intentionally separate from role checks:
// non-root accounts must not regain dashboard access merely by carrying an
// administrator role.
func ValidatePersonalOwner(userID int) error {
	owner, err := GetPersonalOwner()
	if err != nil {
		return err
	}
	if owner.Id != userID {
		return ErrPersonalOwnerMismatch
	}
	return nil
}
