package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-parcels/internal/config"
	"github.com/abjelosevic88/pocket-homelab-parcels/internal/store"
	"github.com/abjelosevic88/pocket-homelab-parcels/internal/track"
)

type app struct {
	t     *testing.T
	url   string
	s     *Server
	st    *store.Store
	alice string
	bob   string
	ids   map[string]int64
}

func newApp(t *testing.T, tweak func(*config.Config)) *app {
	t.Helper()
	cfg := testConfig(t.TempDir())
	cfg.BaseURL = "http://parcels.test"
	cfg.MaxBodyBytes = 64 << 20
	cfg.NotifyHour = 8
	if tweak != nil {
		tweak(cfg)
	}
	st := openTestStore(t, cfg)
	ctx := context.Background()
	a := &app{t: t, st: st, ids: map[string]int64{}}
	for _, u := range []*store.User{{Email: "alice@example.com", Name: "Alice", Role: "admin"}, {Email: "bob@example.com", Name: "Bob", Role: "member"}} {
		if err := st.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		tok, _, _ := st.CreateAPIToken(ctx, u.ID, "t")
		a.ids[u.Name] = u.ID
		if u.Name == "Alice" {
			a.alice = tok
		} else {
			a.bob = tok
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	a.s = New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err := a.s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(a.s.Router())
	t.Cleanup(ts.Close)
	a.url = ts.URL
	return a
}

func (a *app) call(auth, method, path string, body any) (int, map[string]any, []any) {
	a.t.Helper()
	var rd io.Reader
	if body != nil {
		j, _ := json.Marshal(body)
		rd = bytes.NewReader(j)
	}
	req, _ := http.NewRequest(method, a.url+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	var l []any
	if json.Unmarshal(raw, &m) != nil {
		_ = json.Unmarshal(raw, &l)
	}
	return res.StatusCode, m, l
}

func (a *app) ok(auth, method, path string, body any) map[string]any {
	a.t.Helper()
	code, m, _ := a.call(auth, method, path, body)
	if code >= 300 {
		a.t.Fatalf("%s %s → %d %v", method, path, code, m)
	}
	return m
}

func (a *app) list(auth, path string) []any {
	a.t.Helper()
	code, m, l := a.call(auth, "GET", path, nil)
	if code != 200 {
		a.t.Fatalf("GET %s → %d %v", path, code, m)
	}
	return l
}

func (a *app) expect(want int, auth, method, path string, body any) map[string]any {
	a.t.Helper()
	code, m, _ := a.call(auth, method, path, body)
	if code != want {
		a.t.Fatalf("%s %s → %d, want %d (%v)", method, path, code, want, m)
	}
	return m
}

func num(m map[string]any, k string) float64 { v, _ := m[k].(float64); return v }
func id(m map[string]any) string             { return itoa(int64(num(m, "id"))) }

// fakeCainiao answers like Cainiao with whatever the test put in.
type fakeCainiao struct {
	res   map[string]*track.Result
	calls int
}

func (f *fakeCainiao) Get(_ context.Context, numbers []string) (map[string]*track.Result, error) {
	f.calls++
	out := map[string]*track.Result{}
	for _, n := range numbers {
		if r, ok := f.res[n]; ok {
			out[n] = r
		}
	}
	return out, nil
}

func TestPerPersonIsolation(t *testing.T) {
	a := newApp(t, nil)
	p := a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"tracking_number": "rr 123 456 785 cn", "title": "Lamp", "shop": "AliExpress", "price": 1999})
	pid := id(p)
	if p["tracking_number"] != "RR123456785CN" || p["carrier"] != "post-cn" || p["carrier_name"] != "China Post" {
		t.Fatalf("number/carrier = %v %v %v", p["tracking_number"], p["carrier"], p["carrier_name"])
	}
	a.ok(a.alice, "POST", "/api/v1/parcels/"+pid+"/events", map[string]any{"description": "Picked up from the neighbour", "status": "delivered"})
	a.ok(a.alice, "POST", "/api/v1/attachments", map[string]any{"parcel_id": num(p, "id"), "kind": "url", "url": "https://example.com/invoice.pdf", "title": "Invoice"})

	// Bob sees nothing of Alice's, and cannot touch it
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/v1/parcels/" + pid}, {"PUT", "/api/v1/parcels/" + pid}, {"DELETE", "/api/v1/parcels/" + pid},
		{"GET", "/api/v1/parcels/" + pid + "/events"}, {"POST", "/api/v1/parcels/" + pid + "/events"}, {"POST", "/api/v1/parcels/" + pid + "/check"},
		{"POST", "/api/v1/parcels/" + pid + "/delivered"},
	} {
		body := map[string]any{"title": "Mine now", "description": "x"}
		if code, m, _ := a.call(a.bob, c.method, c.path, body); code != 404 {
			t.Errorf("Bob %s %s → %d %v, want 404", c.method, c.path, code, m)
		}
	}
	a.expect(404, a.bob, "POST", "/api/v1/attachments", map[string]any{"parcel_id": num(p, "id"), "kind": "url", "url": "https://example.com/x"})
	for _, view := range []string{"active", "delivered", "all", "archived"} {
		if l := a.ok(a.bob, "GET", "/api/v1/parcels?view="+view, nil)["parcels"].([]any); len(l) != 0 {
			t.Errorf("Bob's %s list has %d parcels", view, len(l))
		}
	}
	if l := a.ok(a.bob, "GET", "/api/v1/search?q=lamp", nil)["parcels"].([]any); len(l) != 0 {
		t.Error("Bob found Alice's parcel by search")
	}
	// the same number is fine for Bob: it is his own parcel then
	a.expect(201, a.bob, "POST", "/api/v1/parcels", map[string]any{"tracking_number": "RR123456785CN"})
	// …but not twice for Alice
	dup := a.expect(409, a.alice, "POST", "/api/v1/parcels", map[string]any{"tracking_number": "RR123456785CN"})
	if num(dup, "id") != num(p, "id") {
		t.Errorf("409 should name the parcel already followed: %v", dup)
	}
	// Alice's own view: delivered by hand, a timeline, the paper
	got := a.ok(a.alice, "GET", "/api/v1/parcels/"+pid, nil)
	if got["status"] != "delivered" || got["delivered_at"] == "" || len(got["events"].([]any)) != 1 || len(got["attachments"].([]any)) != 1 {
		t.Errorf("Alice's parcel = %v", got)
	}
	// Bob's finds and Alice's finds are their own
	ctx := context.Background()
	f := store.Find{MailMessageID: 7, TrackingNumber: "LP00123456789012", Subject: "Shipped"}
	if _, err := a.st.SaveFind(ctx, a.ids["Alice"], &f); err != nil {
		t.Fatal(err)
	}
	a.expect(404, a.bob, "POST", "/api/v1/finds/"+itoa(f.ID)+"/add", nil)
	a.expect(404, a.bob, "POST", "/api/v1/finds/"+itoa(f.ID)+"/dismiss", nil)
	if l := a.ok(a.bob, "GET", "/api/v1/finds", nil)["finds"].([]any); len(l) != 0 {
		t.Error("Bob sees Alice's finds")
	}
	added := a.expect(201, a.alice, "POST", "/api/v1/finds/"+itoa(f.ID)+"/add", map[string]any{"title": "Phone case"})
	if added["title"] != "Phone case" || added["source"] != "mail" || added["carrier"] != "cainiao" {
		t.Errorf("parcel from find = %v", added)
	}
}

func TestValidation(t *testing.T) {
	a := newApp(t, nil)
	a.expect(400, a.alice, "POST", "/api/v1/parcels", map[string]any{})
	a.expect(400, a.alice, "POST", "/api/v1/parcels", map[string]any{"tracking_number": "12"})
	a.expect(400, a.alice, "POST", "/api/v1/parcels", map[string]any{"title": "x", "carrier": "pigeon"})
	a.expect(400, a.alice, "POST", "/api/v1/parcels", map[string]any{"title": "x", "expected_on": "next week"})
	a.expect(400, a.alice, "POST", "/api/v1/parcels", map[string]any{"title": "x", "status": "lost-ish"})
	// no number is fine with a name: a local delivery
	p := a.expect(201, a.alice, "POST", "/api/v1/parcels", map[string]any{"title": "Sofa", "expected_on": "2030-01-10", "expected_until": "2030-01-05"})
	if p["expected_on"] != "2030-01-05" || p["expected_until"] != "2030-01-10" || p["expected_manual"] != true || p["can_check"] != false {
		t.Errorf("sofa = %v", p)
	}
	// an outgoing parcel to a contact gets a name
	o := a.expect(201, a.alice, "POST", "/api/v1/parcels", map[string]any{"tracking_number": "RE123456789BA", "direction": "out", "contact_name": "Ana"})
	if o["title"] != "Parcel to Ana" || o["carrier_name"] != "BH Pošta" {
		t.Errorf("outgoing = %v", o)
	}
}

// A Cainiao parcel is followed for free: steps are kept once, the status
// moves on, the delivered day is remembered and the next check stops.
func TestTrackingApplies(t *testing.T) {
	a := newApp(t, nil)
	fake := &fakeCainiao{res: map[string]*track.Result{}}
	a.s.cainiao = fake
	ctx := context.Background()
	uid := a.ids["Alice"]
	p := a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"tracking_number": "LP00123456789012", "title": "Earbuds"})
	if p["provider"] != "cainiao" || p["can_check"] != true {
		t.Fatalf("provider = %v", p["provider"])
	}
	pid := int64(num(p, "id"))
	t0 := time.Now().Add(-48 * time.Hour).UTC()
	steps := []track.Event{
		{At: t0.Format(time.RFC3339), Status: track.StatusInfoReceived, Description: "Order information received"},
		{At: t0.Add(20 * time.Hour).Format(time.RFC3339), Status: track.StatusInTransit, Description: "Departed from sorting center", Location: "Shenzhen"},
	}
	fake.res["LP00123456789012"] = &track.Result{Found: true, Status: track.StatusInTransit, Text: steps[1].Description, Location: "Shenzhen", At: steps[1].At,
		ExpectedFrom: "2030-02-01", ExpectedUntil: "2030-02-05", Events: steps}
	pp, _ := a.st.GetParcel(ctx, uid, pid)
	if _, err := a.s.checkParcel(ctx, uid, pp); err != nil {
		t.Fatal(err)
	}
	// twice: nothing new
	pp, _ = a.st.GetParcel(ctx, uid, pid)
	if changed, err := a.s.checkParcel(ctx, uid, pp); err != nil || changed {
		t.Fatalf("second check changed=%v err=%v", changed, err)
	}
	got := a.ok(a.alice, "GET", "/api/v1/parcels/"+itoa(pid), nil)
	if got["status"] != "in_transit" || got["location"] != "Shenzhen" || got["expected_on"] != "2030-02-01" || len(got["events"].([]any)) != 2 || got["next_check_at"] == "" {
		t.Fatalf("after check = %v", got)
	}
	// an expected day the person typed is theirs
	a.ok(a.alice, "PUT", "/api/v1/parcels/"+itoa(pid), map[string]any{"expected_on": "2030-03-03"})
	steps = append(steps, track.Event{At: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339), Status: track.StatusDelivered, Description: "Delivered", Location: "Sarajevo"})
	fake.res["LP00123456789012"] = &track.Result{Found: true, Status: track.StatusDelivered, Text: "Delivered", Location: "Sarajevo", At: steps[2].At, ExpectedFrom: "2030-02-02", Events: steps}
	pp, _ = a.st.GetParcel(ctx, uid, pid)
	if _, err := a.s.checkParcel(ctx, uid, pp); err != nil {
		t.Fatal(err)
	}
	got = a.ok(a.alice, "GET", "/api/v1/parcels/"+itoa(pid), nil)
	if got["status"] != "delivered" || got["delivered_at"] != steps[2].At || got["expected_on"] != "2030-03-03" || got["next_check_at"] != "" || got["can_check"] != false {
		t.Fatalf("after delivery = %v", got)
	}
	if l := a.ok(a.alice, "GET", "/api/v1/parcels?view=delivered", nil)["parcels"].([]any); len(l) != 1 {
		t.Errorf("delivered list = %d", len(l))
	}
	// a number nothing can follow says so
	n := a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"tracking_number": "1Z999AA10123456784"})
	if n["provider"] != "" || n["carrier"] != "ups" {
		t.Errorf("UPS without a key = %v", n)
	}
	res := a.ok(a.alice, "POST", "/api/v1/parcels/"+id(n)+"/check", nil)
	if !strings.Contains(res["error"].(string), "17TRACK") {
		t.Errorf("check without a service = %v", res)
	}
}

func TestLinkScopes(t *testing.T) {
	req := func(method, path string) *http.Request { return httptest.NewRequest(method, path, nil) }
	read, write := []string{"parcels:read"}, []string{"parcels:write"}
	cases := []struct {
		scopes []string
		method string
		path   string
		want   bool
	}{
		{nil, "GET", "/api/v1/auth/me", true},
		{read, "GET", "/api/v1/parcels", true},
		{read, "GET", "/api/v1/parcels/12", true},
		{read, "GET", "/api/v1/parcels/12/events", true},
		{read, "GET", "/api/v1/summary", true},
		{read, "GET", "/api/v1/day/2026-11-05", true},
		{read, "POST", "/api/v1/parcels", false},
		{read, "GET", "/api/v1/settings", false},
		{read, "GET", "/api/v1/finds", false},
		{write, "POST", "/api/v1/parcels", true},
		{write, "PUT", "/api/v1/parcels/3", true},
		{write, "DELETE", "/api/v1/parcels/3", true},
		{write, "POST", "/api/v1/parcels/3/events", true},
		{write, "GET", "/api/v1/parcels", false},
		{write, "POST", "/api/v1/parcels/3/money", false},
		{write, "POST", "/api/v1/parcels/3/inventory", false},
		{append(read, write...), "GET", "/api/v1/tokens", false},
		{append(read, write...), "GET", "/api/v1/export.json", false},
	}
	for _, c := range cases {
		if got := linkAllows(c.scopes, req(c.method, c.path)); got != c.want {
			t.Errorf("%v %s %s = %v, want %v", c.scopes, c.method, c.path, got, c.want)
		}
	}
}

func TestExportImportIdempotent(t *testing.T) {
	a := newApp(t, nil)
	p := a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"tracking_number": "JJD0012345678901234", "title": "Kettle", "price": 3999, "return_by": "2030-01-01"})
	a.ok(a.alice, "POST", "/api/v1/parcels/"+id(p)+"/events", map[string]any{"description": "Arrived at the parcel center", "location": "Leipzig"})
	a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"title": "Sofa"})
	a.ok(a.alice, "POST", "/api/v1/attachments", map[string]any{"parcel_id": num(p, "id"), "kind": "url", "url": "https://example.com/r.pdf"})
	doc := a.ok(a.alice, "GET", "/api/v1/export.json", nil)
	if doc["app"] != "pocket-parcels" || len(doc["parcels"].([]any)) != 2 || len(doc["events"].([]any)) != 1 {
		t.Fatalf("export = %v", doc)
	}
	first := a.ok(a.bob, "POST", "/api/v1/import", doc)
	if num(first, "parcels") != 2 || num(first, "events") != 1 || num(first, "attachments") != 1 {
		t.Fatalf("first import = %v", first)
	}
	again := a.ok(a.bob, "POST", "/api/v1/import", doc)
	if num(again, "parcels") != 0 || num(again, "events") != 0 || num(again, "attachments") != 0 {
		t.Fatalf("second import added %v", again)
	}
	if l := a.ok(a.bob, "GET", "/api/v1/parcels?view=all", nil)["parcels"].([]any); len(l) != 2 {
		t.Errorf("Bob has %d parcels", len(l))
	}
	a.expect(400, a.bob, "POST", "/api/v1/import", map[string]any{"app": "pocket-home"})
}

func TestSettingsPerPerson(t *testing.T) {
	a := newApp(t, nil)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"notify_out": false, "track17_key": "abcdefghijklmnop", "stale_days": 5, "title": "Our parcels"})
	al := a.ok(a.alice, "GET", "/api/v1/settings", nil)
	bo := a.ok(a.bob, "GET", "/api/v1/settings", nil)
	if al["notify_out"] != false || bo["notify_out"] != true {
		t.Errorf("notify_out alice=%v bob=%v", al["notify_out"], bo["notify_out"])
	}
	if al["track17_key"] != "abcd••••mnop" || bo["track17_key"] != "" || al["track17_configured"] != true {
		t.Errorf("17TRACK key alice=%v bob=%v", al["track17_key"], bo["track17_key"])
	}
	if al["title"] != "Our parcels" || bo["title"] != "Our parcels" {
		t.Errorf("box title = %v / %v", al["title"], bo["title"])
	}
	// a member cannot rename the box
	a.ok(a.bob, "PUT", "/api/v1/settings", map[string]any{"title": "Bob's"})
	if a.ok(a.alice, "GET", "/api/v1/settings", nil)["title"] != "Our parcels" {
		t.Error("member renamed the box")
	}
	a.expect(400, a.alice, "PUT", "/api/v1/settings", map[string]any{"stale_days": 500})
	// the masked key sent back does not overwrite the real one
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"track17_key": "abcd••••mnop"})
	st, _ := a.st.GetSettings(context.Background(), a.ids["Alice"])
	if st.Track17Key != "abcdefghijklmnop" {
		t.Errorf("key = %q", st.Track17Key)
	}
	// with a 17TRACK key every number is followed
	p := a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"tracking_number": "1Z999AA10123456784"})
	if p["provider"] != "17track" {
		t.Errorf("provider with a key = %v", p["provider"])
	}
}

func TestOverviewSummaryDay(t *testing.T) {
	a := newApp(t, nil)
	today := time.Now().Format("2006-01-02")
	a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"title": "Grinder", "status": "out_for_delivery"})
	a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"title": "Case", "status": "pickup"})
	a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"title": "Hub", "expected_on": store.AddDays(today, 5)})
	d := a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"title": "Lamp"})
	a.ok(a.alice, "POST", "/api/v1/parcels/"+id(d)+"/delivered", nil)
	a.ok(a.alice, "POST", "/api/v1/parcels", map[string]any{"title": "Jacket", "return_by": store.AddDays(today, 2), "return_state": "returning"})
	ov := a.ok(a.alice, "GET", "/api/v1/overview", nil)
	c := ov["counts"].(map[string]any)
	if num(c, "needs_you") != 2 || num(c, "on_the_way") != 2 || num(c, "returns") != 1 || len(ov["recent"].([]any)) != 1 {
		t.Errorf("overview counts = %v recent=%d", c, len(ov["recent"].([]any)))
	}
	sm := a.ok(a.alice, "GET", "/api/v1/summary", nil)
	sc := sm["counts"].(map[string]any)
	if num(sc, "today") != 1 || num(sc, "pickup") != 1 || num(sc, "returns_due") != 1 || num(sc, "delivered_today") != 1 {
		t.Errorf("summary counts = %v", sc)
	}
	day := a.ok(a.alice, "GET", "/api/v1/day/"+today, nil)
	if len(day["delivered"].([]any)) != 1 || len(day["steps"].([]any)) != 1 || len(day["added"].([]any)) != 5 {
		t.Errorf("day = delivered %d steps %d added %d", len(day["delivered"].([]any)), len(day["steps"].([]any)), len(day["added"].([]any)))
	}
	if l := a.ok(a.bob, "GET", "/api/v1/summary", nil)["counts"].(map[string]any); num(l, "today")+num(l, "pickup") != 0 {
		t.Error("Bob's summary shows Alice's parcels")
	}
}

// Demo data seeds and every screen's endpoint answers on it.
func TestDemoData(t *testing.T) {
	a := newApp(t, func(c *config.Config) { c.DemoData = true; c.AdminEmail = ""; c.AuthMode = "local" })
	if err := a.s.seedDemo(context.Background(), a.ids["Alice"]); err != nil {
		t.Fatal(err)
	}
	all := a.ok(a.alice, "GET", "/api/v1/parcels?view=all", nil)
	if num(all, "total") < 40 {
		t.Fatalf("demo parcels = %v", all["total"])
	}
	ov := a.ok(a.alice, "GET", "/api/v1/overview", nil)
	if len(ov["needs_you"].([]any)) < 2 || len(ov["on_the_way"].([]any)) < 2 || len(ov["problems"].([]any)) < 1 || num(ov, "finds") != 3 {
		t.Errorf("demo overview = needs %d, way %d, problems %d, finds %v", len(ov["needs_you"].([]any)), len(ov["on_the_way"].([]any)), len(ov["problems"].([]any)), ov["finds"])
	}
	st := a.ok(a.alice, "GET", "/api/v1/stats", nil)
	if num(st, "delivered") < 30 || len(st["shops"].([]any)) < 5 {
		t.Errorf("demo stats = %v", st)
	}
	// seeding again adds nothing
	if err := a.s.seedDemo(context.Background(), a.ids["Alice"]); err != nil {
		t.Fatal(err)
	}
	if again := a.ok(a.alice, "GET", "/api/v1/parcels?view=all", nil); num(again, "total") != num(all, "total") {
		t.Error("second seed added parcels")
	}
}

var _ = bytes.NewReader
var _ = json.Marshal
var _ = io.Discard
var _ = slog.New
