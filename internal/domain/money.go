package domain

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Money — сумма в копейках. Float для денег не используем.
type Money int64

// MaxMoney — верхняя граница одной траты (100 млн ₽), защита от опечаток и переполнений.
const MaxMoney Money = 100_000_000_00

var (
	errInvalidMoney = errors.New("invalid amount")
	// ErrMoneyTooLarge — сумма корректна по формату, но больше MaxMoney.
	ErrMoneyTooLarge = errors.New("amount is too large")
)

// ParseMoney разбирает десятичную строку в рублях: "350", "350.5", "1200,90".
// Допускаются только цифры и один разделитель; знак не принимается.
func ParseMoney(s string) (Money, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", ".")
	whole, frac, hasFrac := strings.Cut(s, ".")
	if !isDigits(whole) || (hasFrac && (!isDigits(frac) || len(frac) > 2)) {
		return 0, errInvalidMoney
	}
	// Только цифры, поэтому ошибка ParseInt здесь — переполнение.
	rub, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || rub > int64(MaxMoney/100) {
		return 0, ErrMoneyTooLarge
	}
	var kop int64
	if hasFrac {
		if len(frac) == 1 {
			frac += "0"
		}
		kop, _ = strconv.ParseInt(frac, 10, 64)
	}
	if m := Money(rub*100 + kop); m <= MaxMoney {
		return m, nil
	}
	return 0, ErrMoneyTooLarge
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// MoneyFromFloat переводит рубли (например, из аргументов LLM) в копейки с округлением.
func MoneyFromFloat(rub float64) (Money, error) {
	if math.IsNaN(rub) || math.IsInf(rub, 0) || math.Abs(rub) > float64(MaxMoney/100) {
		return 0, errInvalidMoney
	}
	return Money(math.Round(rub * 100)), nil
}

// Decimal возвращает сумму в рублях без лишних нулей: "350", "350.5", "0.05".
func (m Money) Decimal() string {
	sign := ""
	if m < 0 {
		sign, m = "-", -m
	}
	rub, kop := int64(m)/100, int64(m)%100
	if kop == 0 {
		return fmt.Sprintf("%s%d", sign, rub)
	}
	return strings.TrimRight(fmt.Sprintf("%s%d.%02d", sign, rub, kop), "0")
}

// Float — сумма в рублях для JSON-ответов.
func (m Money) Float() float64 {
	return float64(m) / 100
}

// String форматирует сумму для человека: "1 200,50 ₽".
func (m Money) String() string {
	sign := ""
	if m < 0 {
		sign, m = "-", -m
	}
	rub, kop := int64(m)/100, int64(m)%100

	digits := strconv.FormatInt(rub, 10)
	var b strings.Builder
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(d)
	}
	if kop != 0 {
		fmt.Fprintf(&b, ",%02d", kop)
	}
	return sign + b.String() + " ₽"
}
