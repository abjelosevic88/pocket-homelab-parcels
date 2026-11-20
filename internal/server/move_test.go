package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-parcels/internal/config"
)

// newMoveApp is newApp behind a Pocket Core that vouches for Pocket Move's
// link tokens: pcx_<alice|bob>_<in|out|read> (move:import / move:export / another scope).
func newMoveApp(t *testing.T) *app {
	t.Helper()
	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/auth/introspect" {
			io.WriteString(w, `{}`)
			return
		}
		var in struct{ Token string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		parts := strings.Split(in.Token, "_")
		if len(parts) != 3 || parts[0] != "pcx" {
			io.WriteString(w, `{"active":false}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"active": true, "kind": "delegated", "audience": "parcels",
			"scope": map[string]string{"in": core.ScopeMoveImport, "out": core.ScopeMoveExport, "read": "parcels:read"}[parts[2]],
			"user":  map[string]any{"id": 1, "sub": parts[1] + "@example.com", "email": parts[1] + "@example.com", "name": parts[1], "role": "member"},
			"app":   map[string]any{"id": 9, "slug": "move", "name": "Pocket Move"}})
	}))
	t.Cleanup(coreSrv.Close)
	return newApp(t, func(c *config.Config) { c.CoreURL = coreSrv.URL })
}

func (a *app) moveRaw(auth, method, path, ctype string, body []byte) (int, []byte) {
	a.t.Helper()
	req, _ := http.NewRequest(method, a.url+path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+auth)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

// moveImport posts a file to an import as Pocket Move does and returns the answer.
func (a *app) moveImport(auth, path, ctype string, body []byte) map[string]any {
	a.t.Helper()
	code, out := a.moveRaw(auth, "POST", path, ctype, body)
	if code != 200 {
		a.t.Fatalf("POST %s → %d %s", path, code, out)
	}
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	return m
}

type moveCase struct {
	tok, method, path string
	want              int
}

// checkMoveScopes: move:export reaches the description and the exports,
// move:import the description and the imports, another scope neither.
func checkMoveScopes(t *testing.T, a *app, exports, imports int, cases []moveCase) {
	t.Helper()
	for _, c := range append([]moveCase{
		{"pcx_bob_out", "GET", core.PortablePath, 200},
		{"pcx_bob_in", "GET", core.PortablePath, 200},
		{"pcx_bob_read", "GET", core.PortablePath, 403},
		{"pcx_bob_out", "GET", "/api/v1/settings", 403},
		{"pcx_bob_in", "PUT", "/api/v1/settings", 403},
		{"pcx_bob_out", "GET", "/api/v1/backup", 403},
	}, cases...) {
		if code, body := a.moveRaw(c.tok, c.method, c.path, "application/json", []byte("{}")); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%s)", c.tok, c.method, c.path, code, c.want, body)
		}
	}
	_, body := a.moveRaw("pcx_bob_out", "GET", core.PortablePath, "", nil)
	var p core.Portable
	if err := json.Unmarshal(body, &p); err != nil || len(p.Exports) != exports || len(p.Imports) != imports {
		t.Fatalf("portable: %s", body)
	}
	kinds := 0
	for _, e := range p.Exports {
		if e.Kind == core.KindPocket {
			kinds++
		}
	}
	if kinds != 1 {
		t.Fatalf("exactly one pocket export: %s", body)
	}
}

func (a *app) moveExport(auth, path string) []byte {
	a.t.Helper()
	code, b := a.moveRaw(auth, "GET", path, "", nil)
	if code != 200 {
		a.t.Fatalf("GET %s → %d %s", path, code, b)
	}
	return b
}

func TestMoveLinkScopes(t *testing.T) {
	a := newMoveApp(t)
	checkMoveScopes(t, a, 2, 1, []moveCase{
		{"pcx_bob_out", "GET", "/api/v1/export.json", 200},
		{"pcx_bob_out", "GET", "/api/v1/export.csv", 200},
		{"pcx_bob_out", "POST", "/api/v1/import", 403},
		{"pcx_bob_out", "GET", "/api/v1/parcels", 403},
		{"pcx_bob_in", "GET", "/api/v1/export.json", 403},
		{"pcx_bob_in", "POST", "/api/v1/parcels", 403},
		{"pcx_bob_read", "GET", "/api/v1/export.json", 403},
	})
}

// Move brings Alice's parcels to Bob: the import adds once, and nothing of
// one person shows up for the other.
func TestMoveImportsAddOnce(t *testing.T) {
	a := newMoveApp(t)
	a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"tracking_number": "RR123456785CN", "title": "Lamp", "shop": "AliExpress", "price": 1999})
	a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"title": "Grandma's jam", "notes": "no number"})

	dump := a.moveExport("pcx_alice_out", "/api/v1/export.json")
	if !strings.Contains(string(dump), "Grandma's jam") {
		t.Fatalf("alice's export: %s", dump)
	}
	if b := a.moveExport("pcx_bob_out", "/api/v1/export.json"); strings.Contains(string(b), "Lamp") {
		t.Fatalf("bob's export holds alice's parcels: %s", b)
	}
	if c := a.moveExport("pcx_alice_out", "/api/v1/export.csv"); !strings.Contains(string(c), "Lamp") {
		t.Fatalf("alice's csv: %s", c)
	}
	first := a.moveImport("pcx_bob_in", "/api/v1/import", "application/json", dump)
	second := a.moveImport("pcx_bob_in", "/api/v1/import", "application/json", dump)
	if num(first, "parcels") != 2 || num(second, "parcels")+num(second, "events")+num(second, "attachments") != 0 {
		t.Fatalf("imports: %v then %v", first, second)
	}
	_, bob, bl := a.call(a.bob, "GET", "/api/v1/parcels?all=1", nil)
	_, alice, al := a.call(a.alice, "GET", "/api/v1/parcels?all=1", nil)
	if count(bob, bl) != 2 || count(alice, al) != 2 {
		t.Fatalf("parcels: bob %v %v, alice %v %v", bob, bl, alice, al)
	}
}

// count is the number of items in a list answer ([] or {"items": []}).
func count(m map[string]any, l []any) int {
	if l != nil {
		return len(l)
	}
	for _, k := range []string{"items", "parcels"} {
		if v, ok := m[k].([]any); ok {
			return len(v)
		}
	}
	return -1
}
