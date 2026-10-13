package tsapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeTailscale serves the WIF integration and the Tailscale API from one server.
func fakeTailscale(t *testing.T, seen *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/wif/metadata":
			w.Write([]byte(`{"client_id":"cid-123"}`))
		case "/wif/token":
			w.Write([]byte(`{"token":"oidc-jwt"}`))
		case "/api/oauth/token-exchange":
			r.ParseForm()
			if r.Form.Get("client_id") != "cid-123" || r.Form.Get("jwt") != "oidc-jwt" {
				http.Error(w, `{"message":"bad exchange"}`, 401)
				return
			}
			w.Write([]byte(`{"access_token":"api-tok"}`))
		default:
			if r.Header.Get("Authorization") != "Bearer api-tok" {
				http.Error(w, `{"message":"unauthorized"}`, 401)
				return
			}
			switch {
			case r.URL.Path == "/api/tailnet/-/devices":
				w.Write([]byte(`{"devices":[{"id":"1","name":"muse.tail.ts.net","hostname":"muse","addresses":["100.1.1.1"]},{"id":"2","name":"gizmo-mini.tail.ts.net","hostname":"Gizmo-Mini","addresses":["100.2.2.2"]}]}`))
			case r.URL.Path == "/api/device/1/tags":
				b, _ := io.ReadAll(r.Body)
				var v struct{ Tags []string }
				json.Unmarshal(b, &v)
				if strings.Join(v.Tags, ",") != "tag:muse" {
					http.Error(w, `{"message":"wrong tags"}`, 400)
				}
			case r.URL.Path == "/api/tailnet/-/acl" && r.Method == "GET":
				w.Header().Set("ETag", `"e1"`)
				w.Write([]byte(`{"grants":[]}`))
			case r.URL.Path == "/api/tailnet/-/acl" && r.Method == "POST":
				if r.Header.Get("If-Match") != `"e1"` {
					http.Error(w, `{"message":"precondition failed"}`, 412)
				}
			default:
				http.Error(w, `{"message":"not found"}`, 404)
			}
		}
	}))
}

func newTestClient(srv *httptest.Server, env map[string]string) *Client {
	c := New()
	c.Base = srv.URL + "/api"
	c.Getenv = func(k string) string { return env[k] }
	return c
}

func TestWIFAuthAndDevices(t *testing.T) {
	var seen []string
	srv := fakeTailscale(t, &seen)
	defer srv.Close()
	c := newTestClient(srv, map[string]string{"TSCTL_WIF_URL": srv.URL + "/wif"})

	devs, err := c.Devices()
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 2 || devs[0].ShortName() != "gizmo-mini" {
		t.Fatalf("devices not sorted by name: %+v", devs)
	}
	src, _ := c.AuthSource()
	if !strings.Contains(src, "cid-123") || strings.Contains(src, "api-tok") {
		t.Fatalf("auth source should name the client and never the token: %q", src)
	}
	// The token is cached: one exchange for several calls.
	c.Devices()
	n := 0
	for _, s := range seen {
		if strings.HasSuffix(s, "token-exchange") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("token exchanged %d times, want 1", n)
	}
}

func TestAPIKeyWinsOverWIF(t *testing.T) {
	var seen []string
	srv := fakeTailscale(t, &seen)
	defer srv.Close()
	c := newTestClient(srv, map[string]string{"TS_API_KEY": "api-tok", "TSCTL_WIF_URL": srv.URL + "/wif"})
	if _, err := c.Devices(); err != nil {
		t.Fatal(err)
	}
	for _, s := range seen {
		if strings.Contains(s, "/wif/") {
			t.Fatalf("WIF used although TS_API_KEY is set: %v", seen)
		}
	}
}

func TestReadOnlyRefusesWritesBeforeSending(t *testing.T) {
	var seen []string
	srv := fakeTailscale(t, &seen)
	defer srv.Close()
	c := newTestClient(srv, map[string]string{"TS_API_KEY": "api-tok"})
	c.ReadOnly = true
	if err := c.SetTags("1", []string{"tag:muse"}); err != ErrReadOnly {
		t.Fatalf("want ErrReadOnly, got %v", err)
	}
	if len(seen) != 0 {
		t.Fatalf("read-only write reached the server: %v", seen)
	}
}

func TestSetTagsAndPolicyIfMatch(t *testing.T) {
	var seen []string
	srv := fakeTailscale(t, &seen)
	defer srv.Close()
	c := newTestClient(srv, map[string]string{"TS_API_KEY": "api-tok"})
	if err := c.SetTags("1", []string{"tag:muse"}); err != nil {
		t.Fatal(err)
	}
	p, err := c.GetPolicy()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetPolicy([]byte(`{}`), p.ETag); err != nil {
		t.Fatalf("SetPolicy with the fetched ETag: %v", err)
	}
	if err := c.SetPolicy([]byte(`{}`), `"stale"`); err == nil {
		t.Fatal("SetPolicy with a stale ETag should fail")
	}
}

func TestResolveDevice(t *testing.T) {
	devs := []Device{
		{ID: "1", Name: "muse.tail.ts.net", Hostname: "muse", Addresses: []string{"100.1.1.1"}},
		{ID: "2", Name: "gizmo-mini.tail.ts.net", Hostname: "Gizmo-Mini", Addresses: []string{"100.2.2.2"}},
		{ID: "3", Name: "web.tail.ts.net", Hostname: "dup"},
		{ID: "4", Name: "web-1.tail.ts.net", Hostname: "dup"},
	}
	for ref, want := range map[string]string{
		"muse": "1", "MUSE": "1", "1": "1", "100.2.2.2": "2", "gizmo-mini": "2",
		"gizmo-mini.tail.ts.net.": "2", "web": "3",
	} {
		d, err := ResolveDevice(devs, ref)
		if err != nil || d.ID != want {
			t.Errorf("%q: got %q, %v; want %q", ref, d.ID, err, want)
		}
	}
	if _, err := ResolveDevice(devs, "dup"); err == nil || !strings.Contains(err.Error(), "matches 2") {
		t.Errorf("ambiguous hostname should error, got %v", err)
	}
	if _, err := ResolveDevice(devs, "nope"); err == nil {
		t.Error("unknown device should error")
	}
}
