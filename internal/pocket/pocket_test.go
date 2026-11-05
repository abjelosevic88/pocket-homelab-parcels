package pocket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Pocket Mail wraps a message: {"message": {…}, "text": "…", "html": "…"}.
func TestMessageBodyReadsMailsWrapper(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/messages/42" || r.Header.Get("Authorization") != "Bearer tok" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"id":42,"subject":"Your order has shipped","from":{"name":"Shop","addr":"noreply@shop.example"},"date":"2026-11-05T08:00:00Z","category":"receipts"},"text":"Tracking number: 1Z999AA10123456784","html":"<p>hi</p>","attachments":[]}`))
	}))
	defer ts.Close()
	m, err := New(ts.URL, "tok", 5*time.Second).MessageBody(context.Background(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != 42 || m.Subject != "Your order has shipped" || m.From.Addr != "noreply@shop.example" || m.From.Name != "Shop" || m.Date == "" ||
		m.Text != "Tracking number: 1Z999AA10123456784" || m.HTML != "<p>hi</p>" {
		t.Fatalf("message = %+v", m)
	}
}
