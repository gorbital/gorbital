package cli

import (
	"net"
	"strings"
	"testing"
)

func TestAppAddr(t *testing.T) {
	tests := []struct {
		env  []string
		want string
	}{
		{nil, defaultAppAddr},
		{[]string{"HOME=/x", "APP_ADDR=127.0.0.1:8090"}, "127.0.0.1:8090"},
		{[]string{"APP_ADDR=127.0.0.1:8090", "APP_ADDR=127.0.0.1:9000"}, "127.0.0.1:9000"},
		{[]string{"APP_ADDR="}, defaultAppAddr},
	}
	for _, tt := range tests {
		if got := appAddr(tt.env); got != tt.want {
			t.Errorf("appAddr(%v) = %q, want %q", tt.env, got, tt.want)
		}
	}
}

func TestCheckPortFree(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	busy := ln.Addr().String()

	err = checkPortFree(busy)
	if err == nil || !strings.Contains(err.Error(), "already in use") || !strings.Contains(err.Error(), "APP_ADDR=127.0.0.1:") {
		t.Errorf("checkPortFree(busy %s) = %v, want in-use error with APP_ADDR suggestion", busy, err)
	}

	ln.Close()
	if err := checkPortFree(busy); err != nil {
		t.Errorf("checkPortFree(free %s) = %v, want nil", busy, err)
	}
	if err := checkPortFree("no-port"); err == nil {
		t.Error("checkPortFree(invalid address) = nil, want error")
	}
}
