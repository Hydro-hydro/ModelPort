package model

import "errors"

var (
	ErrSetupRecordMissing  = errors.New("setup record missing for a non-empty database")
	ErrSetupSchemaMismatch = errors.New("setup record does not match the current schema")
)

const (
	SetupEditionModelPort = "modelport-personal"
	CurrentSchemaVersion  = 1
)

type Setup struct {
	ID            uint   `json:"id" gorm:"primaryKey"`
	Version       string `json:"version" gorm:"type:varchar(50);not null"`
	InitializedAt int64  `json:"initialized_at" gorm:"type:bigint;not null"`
	Edition       string `json:"edition" gorm:"type:varchar(50);not null"`
	SchemaVersion int    `json:"schema_version" gorm:"not null"`
}

func (s *Setup) IsCurrentSchema() bool {
	return s != nil &&
		s.Edition == SetupEditionModelPort &&
		s.SchemaVersion == CurrentSchemaVersion
}

func GetSetup() *Setup {
	var setup Setup
	err := DB.First(&setup).Error
	if err != nil {
		return nil
	}
	return &setup
}
