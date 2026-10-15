package config

import (
	"slices"
	"testing"
)

func TestIDList(t *testing.T) {
	tests := []struct {
		in   string
		want []int64
		ok   bool
	}{
		{"1,2", []int64{1, 2}, true},
		{" 1, 2 ,", []int64{1, 2}, true},
		{"", nil, true},
		{" ", nil, true},
		{"1,abc", nil, false},
		{"-5", nil, false},
	}
	for _, tt := range tests {
		var l IDList
		err := l.UnmarshalText([]byte(tt.in))
		if (err == nil) != tt.ok || (tt.ok && !slices.Equal(l, tt.want)) {
			t.Errorf("%q: got %v, %v", tt.in, l, err)
		}
	}
}

func TestValidateBotRequiresWhitelist(t *testing.T) {
	c := Config{Telegram: Telegram{Token: "t"}, LLM: LLM{BaseURL: "u", Model: "m", MaxSteps: 1}}
	if c.ValidateBot() == nil {
		t.Error("empty whitelist must be rejected")
	}
	c.Telegram.AllowAll = true
	if err := c.ValidateBot(); err != nil {
		t.Errorf("allow all: %v", err)
	}
	c.Telegram.AllowAll = false
	c.Telegram.AllowedIDs = IDList{1}
	if err := c.ValidateBot(); err != nil {
		t.Errorf("whitelist: %v", err)
	}
}
