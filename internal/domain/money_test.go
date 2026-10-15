package domain

import (
	"errors"
	"testing"
)

func TestParseMoney(t *testing.T) {
	tests := []struct {
		in   string
		want Money
		ok   bool
	}{
		{"350", 35000, true},
		{"350.5", 35050, true},
		{"1200,90", 120090, true},
		{" 0.05 ", 5, true},
		{"1.", 0, false},
		{".5", 0, false},
		{"1.234", 0, false},
		{"abc", 0, false},
		{"", 0, false},
		{"1e3", 0, false},
		{"-5", 0, false},
		{"--5", 0, false},
		{"+5", 0, false},
		{"5.+5", 0, false},
		{"5.-0", 0, false},
		{"100000000", 10_000_000_000, true},
		{"100000000.01", 0, false},
		{"99999999999999999999999", 0, false},
	}
	for _, tt := range tests {
		got, err := ParseMoney(tt.in)
		if (err == nil) != tt.ok || got != tt.want {
			t.Errorf("ParseMoney(%q) = %v, %v; want %v, ok=%v", tt.in, got, err, tt.want, tt.ok)
		}
	}
}

func TestMoneyFromFloat(t *testing.T) {
	got, err := MoneyFromFloat(0.1 + 0.2)
	if err != nil || got != 30 {
		t.Fatalf("MoneyFromFloat(0.3) = %v, %v", got, err)
	}
	if _, err := MoneyFromFloat(1e15); err == nil {
		t.Fatal("expected error for huge amount")
	}
}

func TestMoneyFormat(t *testing.T) {
	tests := []struct {
		m       Money
		decimal string
		human   string
	}{
		{35000, "350", "350 ₽"},
		{35050, "350.5", "350,50 ₽"},
		{123456789, "1234567.89", "1 234 567,89 ₽"},
		{5, "0.05", "0,05 ₽"},
	}
	for _, tt := range tests {
		if got := tt.m.Decimal(); got != tt.decimal {
			t.Errorf("%d.Decimal() = %q, want %q", tt.m, got, tt.decimal)
		}
		if got := tt.m.String(); got != tt.human {
			t.Errorf("%d.String() = %q, want %q", tt.m, got, tt.human)
		}
	}
}

func TestParseMoneyTooLarge(t *testing.T) {
	for _, in := range []string{"100000000.01", "1000000000", "99999999999999999999999"} {
		if _, err := ParseMoney(in); !errors.Is(err, ErrMoneyTooLarge) {
			t.Errorf("ParseMoney(%q) err = %v, want ErrMoneyTooLarge", in, err)
		}
	}
	if _, err := ParseMoney("1.2.3"); err == nil || errors.Is(err, ErrMoneyTooLarge) {
		t.Errorf("format error expected, got %v", err)
	}
}
