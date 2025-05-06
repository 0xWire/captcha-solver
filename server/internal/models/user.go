package models

import (
	"time"
)

// User представляет пользователя
type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"` // не выводится в JSON
	Role         string    `json:"role"`
	APIKey       string    `json:"api_key,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	Balance      float64   `json:"balance"`
}
