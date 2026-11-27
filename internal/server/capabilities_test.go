package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-parcels/internal/config"
	"github.com/abjelosevic88/pocket-homelab-parcels/internal/store"
	"github.com/abjelosevic88/pocket-homelab-parcels/internal/track"
)

// countingCainiao stands in for Cainiao and counts who asked it.
type countingCainiao struct{ calls atomic.Int64 }

func (c *countingCainiao) Get(_ context.Context, _ []string) (map[string]*track.Result, error) {
	c.calls.Add(1)
	return map[string]*track.Result{}, nil
}

// newCapTestApp is newApp with Cainiao (on by default for new people)
// replaced by a counter, so no test asks the real service.
func newCapTestApp(t *testing.T, tweak func(*config.Config)) (*app, *countingCainiao) {
	t.Helper()
	a := newApp(t, tweak)
	cn := &countingCainiao{}
	a.s.cainiao = cn
	return a, cn
}

// fakeCapCore is a Pocket Core that vouches for link tokens
// pcx_<alice|bob>_<search|day|widget|capture|read|write> from Pocket Hub
// and remembers the manifest Parcels registers with.
type fakeCapCore struct {
	*httptest.Server
	mu       sync.Mutex
	manifest map[string]any
}

func newCapApp(t *testing.T) (*app, *fakeCapCore) {
	t.Helper()
	fc := &fakeCapCore{}
	scopes := map[string]string{"search": core.ScopeSearch, "day": core.ScopeDay, "widget": core.ScopeWidget, "capture": core.ScopeCapture,
		"read": "parcels:read", "write": "parcels:write"}
	fc.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/apps/register":
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			fc.mu.Lock()
			fc.manifest = m
			fc.mu.Unlock()
			io.WriteString(w, `{}`)
		case "/api/v1/auth/introspect":
			var in struct{ Token string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			parts := strings.Split(in.Token, "_")
			if len(parts) != 3 || parts[0] != "pcx" || scopes[parts[2]] == "" {
				io.WriteString(w, `{"active":false}`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"active": true, "kind": "delegated", "audience": "parcels", "scope": scopes[parts[2]],
				"user": map[string]any{"id": 1, "sub": parts[1] + "@example.com", "email": parts[1] + "@example.com", "name": parts[1], "role": "member"},
				"app":  map[string]any{"id": 9, "slug": "hub", "name": "Pocket Hub"}})
		default:
			io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(fc.Close)
	a, _ := newCapTestApp(t, func(c *config.Config) { c.CoreURL, c.CoreRegistrationKey = fc.URL, "key" })
	return a, fc
}

// items are the rows of a capability answer.
func items(t *testing.T, m map[string]any) []map[string]any {
	t.Helper()
	l, ok := m["items"].([]any)
	if !ok {
		t.Fatalf("no items list in %v", m)
	}
	out := []map[string]any{}
	for _, v := range l {
		out = append(out, v.(map[string]any))
	}
	return out
}

// item finds the row with that title.
func item(t *testing.T, m map[string]any, title string) map[string]any {
	t.Helper()
	for _, it := range items(t, m) {
		if it["title"] == title {
			return it
		}
	}
	t.Fatalf("no %q in %v", title, m["items"])
	return nil
}

func titles(t *testing.T, m map[string]any) []string {
	t.Helper()
	var out []string
	for _, it := range items(t, m) {
		out = append(out, it["title"].(string))
	}
	return out
}

func TestPocketSearch(t *testing.T) {
	a, _ := newCapTestApp(t, nil)
	// no first check running beside the steps added here
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"track_cainiao": false})
	head := a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"tracking_number": "RR123456785CN", "title": "Headphones", "shop": "AliExpress", "notes": "the blue ones"})
	a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"tracking_number": "JJD0012345678901234", "title": "Winter boots", "shop": "Zalando"})
	book := a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"tracking_number": "RE123456789BA", "title": "Book for Ana", "direction": "out", "contact_name": "Ana"})
	a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"title": "Zalando return label"})
	a.ok(a.bob, "POST", "/api/v1/parcels", map[string]any{"title": "Lamp", "shop": "Zalando", "notes": "the blue ones"})
	a.ok(a.alice, "POST", "/api/v1/parcels/"+id(head)+"/events", map[string]any{"description": "Arrived at the sorting centre", "location": "Sarajevo", "status": "in_transit"})

	res := a.ok(a.alice, "GET", "/api/v1/pocket/search?q=headphones", nil)
	got := items(t, res)
	today := shortDay(time.Now(), time.Now())
	if len(got) != 1 || got[0]["id"] != id(head) || got[0]["kind"] != "delivery" || got[0]["url"] != "/parcels/"+id(head) ||
		got[0]["snippet"] != "On the way · China Post · Arrived at the sorting centre, "+today || got[0]["date"] == "" {
		t.Fatalf("headphones: %v", got)
	}
	// by tracking number (spaces or not), notes, shop, carrier
	for q, want := range map[string]string{"rr123456": "Headphones", "rr+123+456": "Headphones", "blue": "Headphones", "dhl": "Winter boots", "china+post": "Headphones"} {
		if got := titles(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q="+q, nil)); len(got) != 1 || got[0] != want {
			t.Errorf("%q finds %v, want %s", q, got, want)
		}
	}
	if b := item(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q=ana", nil), "Book for Ana"); b["kind"] != "sent" || b["url"] != "/parcels/"+id(book) {
		t.Fatalf("sent: %v", b)
	}
	// a name that has the words comes before a shop that has them
	if got := titles(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q=zalando", nil)); len(got) != 2 || got[0] != "Zalando return label" || got[1] != "Winter boots" {
		t.Fatalf("zalando: %v", got)
	}
	if got := items(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q=zalando&limit=1", nil)); len(got) != 1 {
		t.Fatalf("limit 1: %v", got)
	}
	for _, q := range []string{"", "%3F%3F", "*", "%25"} {
		if got := items(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q="+q, nil)); len(got) != 0 {
			t.Fatalf("%q finds %v", q, got)
		}
	}
	// another person's parcels never appear
	for _, q := range []string{"headphones", "dhl", "rr123", "ana"} {
		if got := items(t, a.ok(a.bob, "GET", "/api/v1/pocket/search?q="+q, nil)); len(got) != 0 {
			t.Fatalf("bob finds alice's %q: %v", q, got)
		}
	}
	if got := titles(t, a.ok(a.bob, "GET", "/api/v1/pocket/search?q=blue", nil)); len(got) != 1 || got[0] != "Lamp" {
		t.Fatalf("bob's own: %v", got)
	}
	a.expect(401, "", "GET", "/api/v1/pocket/search?q=boots", nil)
}

func TestPocketDay(t *testing.T) {
	a, _ := newCapTestApp(t, nil)
	today := time.Now().Format("2006-01-02")
	lamp := a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"title": "Lamp", "shop": "IKEA"})
	a.ok(a.alice, "POST", "/api/v1/parcels/"+id(lamp)+"/delivered", nil)
	a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"title": "Grinder", "expected_on": today})
	a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"title": "Hub", "expected_on": store.AddDays(today, -1), "expected_until": store.AddDays(today, 2)})
	boots := a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"title": "Boots"})
	a.ok(a.alice, "POST", "/api/v1/parcels/"+id(boots)+"/events", map[string]any{"description": "Out for delivery", "location": "Sarajevo", "status": "out_for_delivery"})
	a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"title": "Book", "direction": "out", "contact_name": "Ana", "ordered_on": today})
	a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"title": "Later", "expected_on": store.AddDays(today, 3)})

	day := a.ok(a.alice, "GET", "/api/v1/pocket/day/"+today, nil)
	if day["date"] != today || day["summary"] != "1 parcel delivered, 1 sent, 2 expected, 1 with news" || len(items(t, day)) != 5 {
		t.Fatalf("day: %v", day)
	}
	l := item(t, day, "Lamp")
	if l["id"] != "parcel:"+id(lamp) || l["kind"] != "delivered" || l["done"] != true || l["sub"] != "IKEA" || l["url"] != "/parcels/"+id(lamp) || len(l["time"].(string)) != 5 {
		t.Fatalf("delivered: %v", l)
	}
	if g := item(t, day, "Grinder"); g["kind"] != "expected" || g["done"] != nil || g["sub"] != "Waiting for news" {
		t.Fatalf("expected: %v", g)
	}
	if h := item(t, day, "Hub"); h["kind"] != "expected" {
		t.Fatalf("a range: %v", h)
	}
	if b := item(t, day, "Boots"); !strings.HasPrefix(b["id"].(string), "step:") || b["kind"] != "news" || b["sub"] != "Out for delivery · Sarajevo" || b["url"] != "/parcels/"+id(boots) {
		t.Fatalf("news: %v", b)
	}
	if b := item(t, day, "Book"); b["kind"] != "sent" || b["sub"] != "Sent to Ana" {
		t.Fatalf("sent: %v", b)
	}
	if ms := day["metrics"].([]any); len(ms) != 3 {
		t.Fatalf("metrics: %v", ms)
	}
	// a day to come: only what is expected
	if d := a.ok(a.alice, "GET", "/api/v1/pocket/day/"+store.AddDays(today, 3), nil); d["summary"] != "1 parcel expected" || len(items(t, d)) != 1 {
		t.Fatalf("in 3 days: %v", d)
	}
	// a day with nothing, and somebody else's day
	if d := a.ok(a.alice, "GET", "/api/v1/pocket/day/2019-03-04", nil); d["summary"] != nil || len(items(t, d)) != 0 {
		t.Fatalf("an empty day: %v", d)
	}
	if d := a.ok(a.bob, "GET", "/api/v1/pocket/day/"+today+"?tz=Europe/Sarajevo", nil); d["summary"] != nil || len(items(t, d)) != 0 {
		t.Fatalf("bob sees alice's day: %v", d)
	}
	a.expect(400, a.alice, "GET", "/api/v1/pocket/day/yesterday", nil)
	// the old route answers as before
	if old := a.ok(a.alice, "GET", "/api/v1/day/"+today, nil); len(old["delivered"].([]any)) != 1 || len(old["expected"].([]any)) != 2 || len(old["sent"].([]any)) != 1 {
		t.Fatalf("/day changed: %v", old)
	}
}

// A step belongs to the day it happened on where the person is, whatever
// zone the carrier wrote it in.
func TestPocketDayZone(t *testing.T) {
	a, _ := newCapTestApp(t, nil)
	p := a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"title": "Late step"})
	a.ok(a.alice, "POST", "/api/v1/parcels/"+id(p)+"/events", map[string]any{"description": "Arrived late at night", "at": "2025-01-10T23:30:00-05:00"})
	ny := a.ok(a.alice, "GET", "/api/v1/pocket/day/2025-01-10?tz=America/New_York", nil)
	if it := item(t, ny, "Late step"); it["time"] != "23:30" || ny["summary"] != "1 parcel with news" {
		t.Fatalf("New York: %v", ny)
	}
	if d := a.ok(a.alice, "GET", "/api/v1/pocket/day/2025-01-11?tz=America/New_York", nil); len(items(t, d)) != 0 {
		t.Fatalf("the next day in New York: %v", d)
	}
	if it := item(t, a.ok(a.alice, "GET", "/api/v1/pocket/day/2025-01-11?tz=UTC", nil), "Late step"); it["time"] != "04:30" {
		t.Fatalf("UTC: %v", it)
	}
}

func TestPocketWidget(t *testing.T) {
	a, cn := newCapTestApp(t, nil)
	// nothing on its way: nothing to show
	if w := a.ok(a.bob, "GET", "/api/v1/pocket/widget", nil); w["line"] != nil || w["metrics"] != nil || len(items(t, w)) != 0 {
		t.Fatalf("without parcels: %v", w)
	}
	tomorrow := store.AddDays(time.Now().Format("2006-01-02"), 1)
	a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"title": "Book", "expected_on": tomorrow})
	a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"title": "Boots", "status": "exception"})
	a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"title": "Lamp", "status": "out_for_delivery"})
	a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"title": "Case", "status": "pickup"})
	a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"title": "Mug", "status": "delivered"})
	a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"title": "Cable"})
	before := cn.calls.Load()
	w := a.ok(a.alice, "GET", "/api/v1/pocket/widget", nil)
	if w["line"] != "1 out for delivery, 1 ready to pick up, 1 with a problem, 2 on the way" || w["url"] != "/" {
		t.Fatalf("widget: %v", w)
	}
	got := items(t, w)
	if len(got) != 5 || got[0]["title"] != "Lamp" || got[0]["detail"] != "Out for delivery · today" || got[1]["title"] != "Case" || got[1]["detail"] != "Ready to pick up" ||
		got[2]["title"] != "Boots" || got[2]["detail"] != "Problem" || got[3]["title"] != "Book" || got[3]["detail"] != "Waiting for news · tomorrow" ||
		got[4]["title"] != "Cable" || got[4]["detail"] != "Waiting for news" || !strings.HasPrefix(got[0]["url"].(string), "/parcels/") {
		t.Fatalf("rows: %v", got)
	}
	if ms := w["metrics"].([]any); len(ms) != 3 || ms[0].(map[string]any)["value"] != "5" || ms[1].(map[string]any)["value"] != "2" || ms[2].(map[string]any)["value"] != "1" {
		t.Fatalf("metrics: %v", ms)
	}
	if cn.calls.Load() != before {
		t.Fatal("the widget asked the tracking service")
	}
	if w := a.ok(a.bob, "GET", "/api/v1/pocket/widget", nil); len(items(t, w)) != 0 {
		t.Fatalf("bob sees alice's parcels: %v", w)
	}
}

func TestPocketCapture(t *testing.T) {
	a, cn := newCapTestApp(t, nil)
	line := map[string]any{"request_id": "capture-0001-abcdef", "text": "RR123456785CN Headphones"}
	c := a.expect(201, a.alice, "POST", "/api/v1/pocket/capture", line)
	if c["title"] != "Headphones" || c["url"] != "/parcels/"+c["id"].(string) {
		t.Fatalf("capture: %v", c)
	}
	p := a.ok(a.alice, "GET", "/api/v1/parcels/"+c["id"].(string), nil)
	if p["tracking_number"] != "RR123456785CN" || p["carrier"] != "post-cn" || p["direction"] != "in" || p["source"] != "manual" || p["status"] != "pending" {
		t.Fatalf("parcel: %v", p)
	}
	// a new parcel is looked up at once, in the background
	for i := 0; cn.calls.Load() == 0 && i < 100; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if cn.calls.Load() == 0 {
		t.Fatal("the new parcel was never checked")
	}
	// the same request again: the same parcel, nothing added
	if again := a.expect(201, a.alice, "POST", "/api/v1/pocket/capture", line); again["id"] != c["id"] {
		t.Fatalf("again: %v", again)
	}
	if n := len(a.ok(a.alice, "GET", "/api/v1/parcels", nil)["parcels"].([]any)); n != 1 {
		t.Fatalf("%d parcels after a retry", n)
	}
	if e := a.expect(400, a.alice, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "capture-0001-abcdef", "text": "Something else"}); !strings.Contains(e["error"].(string), "other words") {
		t.Fatalf("same id, other words: %v", e)
	}
	// a name, and the number on its own line after a tracking word
	b := a.expect(201, a.alice, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "capture-0002-abcdef", "title": "Winter boots",
		"text": "Tracking number: JJD0012345678901234\nsize 43"})
	bp := a.ok(a.alice, "GET", "/api/v1/parcels/"+b["id"].(string), nil)
	if b["title"] != "Winter boots" || bp["tracking_number"] != "JJD0012345678901234" || bp["carrier"] != "dhl-paket" || bp["notes"] != "size 43" {
		t.Fatalf("boots: %v / %v", b, bp)
	}
	// a number only: the app names it
	if n := a.expect(201, a.alice, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "capture-0003-abcdef", "text": "1Z999AA10123456784"}); n["title"] != "UPS parcel" {
		t.Fatalf("number only: %v", n)
	}
	// what cannot be a parcel to follow
	if e := a.expect(400, a.alice, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "capture-0004-abcdef", "text": "Lamp from IKEA on 2026-12-01"}); !strings.Contains(e["error"].(string), "no tracking number") {
		t.Fatalf("no number: %v", e)
	}
	if e := a.expect(400, a.alice, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "capture-0005-abcdef", "text": "rr123456785cn again"}); !strings.Contains(e["error"].(string), "already follow") ||
		!strings.Contains(e["error"].(string), "Headphones") {
		t.Fatalf("a number already followed: %v", e)
	}
	a.expect(400, a.alice, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "capture-0006-abcdef", "kind": "task", "text": "RR123456785CN"})
	a.expect(400, a.alice, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "short", "text": "RR123456785CN"})
	// per person: Bob follows the same number on his own, and sees only his
	bc := a.expect(201, a.bob, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "capture-0001-abcdef", "text": "RR123456785CN Headphones"})
	if bc["id"] == c["id"] {
		t.Fatalf("bob got alice's parcel: %v", bc)
	}
	a.expect(404, a.bob, "GET", "/api/v1/parcels/"+c["id"].(string), nil)
}

func TestCapabilityLinkScopes(t *testing.T) {
	a, _ := newCapApp(t)
	head := a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"title": "Headphones"})
	a.ok(a.bob, "POST", "/api/v1/parcels", map[string]any{"title": "Bob's headphones"})
	today := time.Now().Format("2006-01-02")
	cases := []struct {
		tok, method, path string
		want              int
	}{
		{"pcx_alice_search", "GET", "/api/v1/pocket/search?q=headphones", 200},
		{"pcx_alice_search", "GET", "/api/v1/auth/me", 200},
		{"pcx_alice_search", "GET", "/api/v1/pocket/widget", 403},
		{"pcx_alice_search", "GET", "/api/v1/search?q=headphones", 403},
		{"pcx_alice_search", "GET", "/api/v1/parcels/" + id(head), 403},
		{"pcx_alice_day", "GET", "/api/v1/pocket/day/" + today, 200},
		{"pcx_alice_day", "GET", "/api/v1/day/" + today, 403},
		{"pcx_alice_day", "GET", "/api/v1/pocket/search?q=x", 403},
		{"pcx_alice_widget", "GET", "/api/v1/pocket/widget", 200},
		{"pcx_alice_widget", "GET", "/api/v1/overview", 403},
		{"pcx_alice_widget", "POST", "/api/v1/pocket/capture", 403},
		{"pcx_alice_capture", "GET", "/api/v1/pocket/widget", 403},
		{"pcx_alice_capture", "POST", "/api/v1/parcels", 403},
		{"pcx_alice_capture", "GET", "/api/v1/pocket/capture", 405}, // no such route to open
		{"pcx_alice_read", "GET", "/api/v1/pocket/search?q=x", 403},
		{"pcx_alice_read", "GET", "/api/v1/pocket/day/" + today, 403},
		{"pcx_alice_read", "GET", "/api/v1/pocket/widget", 403},
		{"pcx_alice_read", "GET", "/api/v1/search?q=x", 200},
		{"pcx_alice_write", "POST", "/api/v1/pocket/capture", 403},
	}
	for _, c := range cases {
		var body any
		if c.method == "POST" {
			body = map[string]any{"request_id": "link-capture-000001", "text": "RR123456785CN"}
		}
		if code, m, _ := a.call(c.tok, c.method, c.path, body); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%v)", c.tok, c.method, c.path, code, c.want, m)
		}
	}
	// through the link, as the person: their own parcels only
	if got := titles(t, a.ok("pcx_alice_search", "GET", "/api/v1/pocket/search?q=headphones", nil)); len(got) != 1 || got[0] != "Headphones" {
		t.Fatalf("alice through the link: %v", got)
	}
	if got := titles(t, a.ok("pcx_bob_widget", "GET", "/api/v1/pocket/widget", nil)); len(got) != 1 || got[0] != "Bob's headphones" {
		t.Fatalf("bob through the link: %v", got)
	}
	// a capture through the link is the person's own parcel, not Pocket Hub's
	c := a.expect(201, "pcx_alice_capture", "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "link-capture-000002", "text": "RR123456785CN Headphones"})
	if p := a.ok(a.alice, "GET", "/api/v1/parcels/"+c["id"].(string), nil); p["source"] != "manual" {
		t.Fatalf("captured through the link: %v", p)
	}
}

func TestManifestCapabilities(t *testing.T) {
	_, fc := newCapApp(t)
	var m map[string]any
	for i := 0; i < 200; i++ {
		fc.mu.Lock()
		m = fc.manifest
		fc.mu.Unlock()
		if m != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if m == nil {
		t.Fatal("Parcels never registered")
	}
	if m["category"] != "Home" {
		t.Errorf("category: %v", m["category"])
	}
	// the capabilities sit at the top of the manifest
	search, _ := m["search"].(map[string]any)
	capture, _ := m["capture"].(map[string]any)
	if search == nil || m["day"] == nil || m["widget"] == nil || capture == nil {
		t.Fatalf("capabilities: %v", m)
	}
	if b, _ := json.Marshal(search["bangs"]); string(b) != `["parcels","parcel","track"]` {
		t.Errorf("bangs: %s", b)
	}
	if b, _ := json.Marshal(capture["kinds"]); !strings.Contains(string(b), `"kind":"parcel"`) || !strings.Contains(string(b), `"fields":["title"]`) {
		t.Errorf("capture kinds: %s", b)
	}
	offers := map[string]bool{}
	for _, o := range m["offers"].([]any) {
		offers[o.(map[string]any)["scope"].(string)] = true
	}
	for _, sc := range []string{core.ScopeSearch, core.ScopeDay, core.ScopeWidget, core.ScopeCapture, "parcels:read", "parcels:write"} {
		if !offers[sc] {
			t.Errorf("no %s offer in %v", sc, offers)
		}
	}
}
