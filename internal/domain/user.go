package domain

import (
	"strconv"
	"strings"
	"time"
)

// User — член семьи, идентифицируется Telegram ID.
type User struct {
	TelegramID int64
	Username   string
	FirstName  string
	LastName   string
	CreatedAt  time.Time
}

// DisplayName — имя для панели: «Имя Фамилия», @username или ID.
func (u User) DisplayName() string {
	if name := strings.TrimSpace(u.FirstName + " " + u.LastName); name != "" {
		return name
	}
	if u.Username != "" {
		return "@" + u.Username
	}
	return strconv.FormatInt(u.TelegramID, 10)
}
