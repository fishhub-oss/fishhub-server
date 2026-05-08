package account

import (
	"log/slog"
	"testing"
)

func TestPosixTZ(t *testing.T) {
	logger := slog.Default()

	tests := []struct {
		iana string
		want string
	}{
		{"America/Sao_Paulo", "<-03>3"},
		{"America/New_York", "EST5EDT"},
		{"America/Los_Angeles", "PST8PDT"},
		{"Europe/London", "GMT0BST"},
		{"Europe/Paris", "CET-1CEST"},
		{"Asia/Tokyo", "JST-9"},
		{"Asia/Kolkata", "IST-5:30"},
		{"UTC", "UTC0"},
		{"Etc/UTC", "UTC0"},
		{"Etc/GMT+3", "<-03>3"},
	}

	for _, tt := range tests {
		t.Run(tt.iana, func(t *testing.T) {
			got := posixTZ(tt.iana, logger)
			if got != tt.want {
				t.Errorf("posixTZ(%q) = %q, want %q", tt.iana, got, tt.want)
			}
		})
	}
}

func TestPosixTZ_UnknownFallback(t *testing.T) {
	got := posixTZ("Not/A/Zone", slog.Default())
	if got != "UTC0" {
		t.Errorf("expected UTC0 fallback, got %q", got)
	}
}

func TestPosixTZ_AllIANANamesHaveEntries(t *testing.T) {
	// Every key in the map must be a non-empty string mapping to a non-empty POSIX rule.
	for iana, posix := range ianaToPostfix {
		if iana == "" {
			t.Error("empty IANA key in map")
		}
		if posix == "" {
			t.Errorf("empty POSIX value for key %q", iana)
		}
	}
}
