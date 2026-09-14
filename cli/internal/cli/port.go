package cli

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// defaultAppAddr matches the generated app's default APP_ADDR.
const defaultAppAddr = "127.0.0.1:8080"

// appAddr returns the last APP_ADDR in env, or the app's default.
func appAddr(env []string) string {
	addr := defaultAppAddr
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "APP_ADDR="); ok && v != "" {
			addr = v
		}
	}
	return addr
}

// checkPortFree reports a clear error when another process already listens
// on addr, instead of letting the app fail with "address already in use".
func checkPortFree(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		host, port, splitErr := net.SplitHostPort(addr)
		if splitErr != nil {
			return fmt.Errorf("APP_ADDR %q is not host:port", addr)
		}
		suggestion := "8090"
		if n, convErr := strconv.Atoi(port); convErr == nil {
			suggestion = strconv.Itoa(n + 10)
		}
		return fmt.Errorf("port %s is already in use by another program\n"+
			"  run on another port by adding this line to .env:\n"+
			"    APP_ADDR=%s", port, net.JoinHostPort(host, suggestion))
	}
	return ln.Close()
}
