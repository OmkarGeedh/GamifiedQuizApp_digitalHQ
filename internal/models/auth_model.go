package models

import (
	"time"
)

type Client struct {
	ID           int       `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Username     string    `gorm:"column:username;size:100;not null;uniqueIndex" json:"username"`
	Email        string    `gorm:"column:email;size:255;not null;uniqueIndex" json:"email"`
	Phone        *string   `gorm:"column:phone;size:20;uniqueIndex" json:"phone,omitempty"`
	Password     string    `gorm:"column:password;size:255;not null" json:"-"`
	Status       string    `gorm:"column:status;size:20;not null;default:'active'" json:"status"` // active, pending, blocked, deleted
	RefreshToken *string   `gorm:"column:refresh_token;size:512" json:"-"`
	CreatedAt    time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

func (Client) TableName() string {
	return "clients"
}
