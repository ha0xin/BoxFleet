package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/haoxin/boxfleet/internal/server/db"
)

func TestObservedClientIP(t *testing.T) {
	for _, test := range []struct{ name, peer, source, cf, cf6, want string }{
		{"trusted IPv4", "172.19.0.1:1234", "cloudflare", "198.51.100.9", "", "198.51.100.9"},
		{"trusted IPv6", "[::ffff:172.19.0.1]:1234", "cloudflare", "2001:db8::9", "", "2001:db8::9"},
		{"untrusted public peer", "198.51.100.8:1234", "cloudflare", "198.51.100.9", "", "198.51.100.8"},
		{"missing header", "172.19.0.1:1234", "cloudflare", "", "", ""},
		{"malformed header", "172.19.0.1:1234", "cloudflare", "bad", "", ""},
		{"bridge spoof", "172.19.0.8:1234", "cloudflare", "198.51.100.9", "", ""},
		{"pseudo IPv4", "172.19.0.1:1234", "cloudflare", "240.1.2.3", "2001:db8::8", "2001:db8::8"},
		{"pseudo without real IPv6", "172.19.0.1:1234", "cloudflare", "240.1.2.3", "", ""},
		{"pseudo with invalid alternative", "172.19.0.1:1234", "cloudflare", "240.1.2.3", "198.51.100.1", ""},
		{"direct ignores headers", "198.51.100.7:1234", "direct", "198.51.100.9", "", "198.51.100.7"},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := Options{ClientIPSource: test.source, TrustedProxyCIDRs: []string{"172.19.0.1/32"}}
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = test.peer
			r.Header.Set("CF-Connecting-IP", test.cf)
			r.Header.Set("CF-Connecting-IPv6", test.cf6)
			var got string
			nodeClientIPMiddleware(options)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if ip := observedRequestIP(r, test.source); ip != nil {
					got = ip.Address
				}
			})).ServeHTTP(httptest.NewRecorder(), r)
			if got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
	if ValidateClientIPOptions("cloudflare", nil) == nil || ValidateClientIPOptions("cloudflare", []string{"bad"}) == nil {
		t.Fatal("accepted unsafe configuration")
	}
}

func TestNetworkAPIAuthenticationAndHeartbeatObservation(t *testing.T) {
	store := openAPITestDB(t)
	ctx := context.Background()
	seedAPITestNode(t, ctx, store)
	issued, err := store.IssueNodeToken(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Options{DB: store, AllowInsecureAdmin: true, ClientIPSource: "cloudflare", TrustedProxyCIDRs: []string{"172.19.0.1/32"}})
	request := func(method, path, body, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.RemoteAddr = "172.19.0.1:1234"
		r.Header.Set("CF-Connecting-IP", "198.51.100.4")
		r.Header.Set("X-BoxFleet-Node", "azus")
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	for _, method := range []string{"GET", "POST"} {
		if w := request(method, "/api/node/network-snapshot", "{}", "bad"); w.Code != 401 {
			t.Fatalf("unauthenticated %s: %d", method, w.Code)
		}
	}
	body := `{"revision":1,"complete":true,"collected_at":"2026-10-18T00:00:00Z","addresses":[{"address":"10.0.0.2","interface":"eth0","interface_index":2,"prefix_length":24}]}`
	if w := request("POST", "/api/node/network-snapshot", body, issued.Token); w.Code != 200 {
		t.Fatalf("snapshot: %d %s", w.Code, w.Body.String())
	}
	if w := request("POST", "/api/node/network-snapshot", strings.Replace(body, "10.0.0.2", "10.0.0.3", 1), issued.Token); w.Code != 409 {
		t.Fatalf("conflict: %d", w.Code)
	}
	if w := request("POST", "/api/node/heartbeat", `{"node_name":"spoofed","status":"ok"}`, issued.Token); w.Code != 200 {
		t.Fatalf("heartbeat: %d %s", w.Code, w.Body.String())
	}
	network, err := store.NodeNetwork(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}
	if len(network.Addresses) != 1 || len(network.ObservedIPs) != 1 || network.ObservedIPs[0].Address != "198.51.100.4" {
		t.Fatalf("network: %+v", network)
	}
	node, err := store.GetNode(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}
	w := request("GET", "/api/admin/nodes/"+node.ID+"/addresses", "", "")
	var detail db.NodeNetwork
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &detail) != nil || len(detail.Addresses) != 1 {
		t.Fatalf("detail: %d %s", w.Code, w.Body.String())
	}
	if err := store.SetNodeStatus(ctx, "azus", "disabled"); err != nil {
		t.Fatal(err)
	}
	if w := request("GET", "/api/node/network-snapshot", "", issued.Token); w.Code != 200 {
		t.Fatalf("pause revoked inventory: %d", w.Code)
	}
	if _, err := store.SoftDeleteNode(ctx, "azus"); err != nil {
		t.Fatal(err)
	}
	if w := request("GET", "/api/node/network-snapshot", "", issued.Token); w.Code != 401 {
		t.Fatalf("deleted node accepted: %d", w.Code)
	}
}
