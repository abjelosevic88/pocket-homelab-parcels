package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-parcels/internal/store"
)

// Adding a parcel and a new status queue parcels.added, parcels.status_changed
// and parcels.delivered on the Core outbox, with the owner's email.
func TestParcelEvents(t *testing.T) {
	cfg := testConfig(t.TempDir())
	cfg.CoreURL = "http://core.invalid"
	st := openTestStore(t, cfg)
	s := New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	ctx := context.Background()
	// Init creates the outbox; no Start, so nothing drains it.
	if err := s.core.Init(ctx); err != nil {
		t.Fatal(err)
	}
	u := &store.User{Email: "ana@example.com", Name: "Ana", Role: "admin"}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	tok, _, _ := st.CreateAPIToken(ctx, u.ID, "t")
	ts := httptest.NewServer(s.Router())
	t.Cleanup(ts.Close)
	a := &app{t: t, url: ts.URL, s: s, st: st}

	p := a.ok(tok, "POST", "/api/v1/parcels", map[string]any{"title": "Kettle", "shop": "IKEA"})
	pid := int64(num(p, "id"))
	a.ok(tok, "POST", "/api/v1/parcels/"+itoa(pid)+"/delivered", nil)

	rows, err := st.DB.QueryContext(ctx, `SELECT body FROM pocket_outbox ORDER BY created_at, rowid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	topics := map[string]map[string]any{}
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			t.Fatal(err)
		}
		var ev struct {
			Topic, Subject string
			Payload        map[string]any
		}
		if err := json.Unmarshal(b, &ev); err != nil {
			t.Fatal(err)
		}
		if ev.Subject != "parcel:"+itoa(pid) {
			t.Errorf("%s subject = %q", ev.Topic, ev.Subject)
		}
		topics[ev.Topic] = ev.Payload
	}
	for _, want := range []string{"parcels.added", "parcels.status_changed", "parcels.delivered"} {
		pl, ok := topics[want]
		if !ok {
			t.Errorf("no %s event (got %v)", want, topics)
			continue
		}
		if pl["user_email"] != "ana@example.com" || pl["title"] != "Kettle" || pl["shop"] != "IKEA" || int64(pl["parcel_id"].(float64)) != pid {
			t.Errorf("%s payload = %v", want, pl)
		}
	}
	if topics["parcels.status_changed"]["previous_status"] != "pending" || topics["parcels.delivered"]["status"] != "delivered" {
		t.Errorf("status payloads = %v", topics)
	}
}
