package expense

import "errors"

var errNoScope = errors.New("expense: access scope is not set")

// Scope определяет, чьи траты доступны вызывающему.
// Нулевое значение запрещает доступ — область нужно задать явно.
type Scope struct {
	userID int64
	all    bool
}

// UserScope — только траты одного члена семьи (Telegram-бот).
func UserScope(userID int64) Scope { return Scope{userID: userID} }

// FamilyScope — траты всех членов семьи (веб-панель главы семьи).
func FamilyScope() Scope { return Scope{all: true} }

// owner возвращает ID владельца для фильтрации или nil для всей семьи.
func (s Scope) owner() (*int64, error) {
	switch {
	case s.all:
		return nil, nil
	case s.userID != 0:
		id := s.userID
		return &id, nil
	default:
		return nil, errNoScope
	}
}
