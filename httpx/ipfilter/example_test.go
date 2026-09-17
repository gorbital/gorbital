package ipfilter_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"gorbital.dev/httpx"
	"gorbital.dev/httpx/ipfilter"
)

func ExampleNew() {
	allow, err := ipfilter.ParsePrefixes("10.0.0.0/8, 2001:db8::/32")
	if err != nil {
		panic(err)
	}
	onlyOffice, err := ipfilter.New(allow, nil)
	if err != nil {
		panic(err)
	}
	ops := httpx.Chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}), onlyOffice)

	for _, addr := range []string{"10.1.2.3:52000", "203.0.113.9:52000"} {
		req := httptest.NewRequest(http.MethodGet, "/ops/system", nil)
		req.RemoteAddr = addr
		rec := httptest.NewRecorder()
		ops.ServeHTTP(rec, req)
		fmt.Println(addr, rec.Code, rec.Body.String() == "ok" || problemCode(rec.Body.Bytes()) == "ip_not_allowed")
	}
	// Output:
	// 10.1.2.3:52000 200 true
	// 203.0.113.9:52000 403 true
}

func ExampleNew_behindAProxy() {
	// Behind a load balancer, resolve the client address first.
	proxies, _ := httpx.ParseTrustedProxies("10.0.0.0/8")
	deny, _ := ipfilter.ParsePrefixes("198.51.100.0/24")
	filter, err := ipfilter.New(nil, deny)
	if err != nil {
		panic(err)
	}
	h := httpx.Chain(http.NotFoundHandler(), httpx.TrustedProxies(proxies), filter)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.2:443"
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	fmt.Println(rec.Code)
	// Output: 403
}

func ExampleParsePrefixes() {
	p, err := ipfilter.ParsePrefixes("10.0.0.5/8, 192.0.2.10, ::ffff:198.51.100.1")
	fmt.Println(p, err)
	_, err = ipfilter.ParsePrefixes("office-vpn")
	fmt.Println(err)
	// Output:
	// [10.0.0.0/8 192.0.2.10/32 198.51.100.1/32] <nil>
	// ipfilter: "office-vpn" is not a CIDR range or IP address
}

// problemCode returns the code of a problem response body.
func problemCode(body []byte) string {
	var p httpx.Problem
	_ = json.Unmarshal(body, &p)
	return p.Code
}
