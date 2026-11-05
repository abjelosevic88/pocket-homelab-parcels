package server

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-parcels/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-parcels/internal/store"
	"github.com/abjelosevic88/pocket-homelab-parcels/internal/track"
)

// ---- shipping emails (Pocket Mail) ----
//
// Every half hour Parcels reads the person's new receipts and updates in
// Pocket Mail. A tracking number it does not know becomes a find ("Found
// in your email": Add or Ignore), or a parcel at once when the person
// chose that. An email about a parcel it does know ("delivered", "ready
// for pick-up") becomes a step in that parcel's timeline. Patterns read
// the email first; the person's AI only reads shipping emails the
// patterns found nothing in.

const mailScanEvery = 25 * time.Minute

func (s *Server) scanAllMail(ctx context.Context) {
	ids, err := s.store.SettingsUsers(ctx)
	if err != nil {
		return
	}
	for _, uid := range ids {
		st, err := s.store.GetSettings(ctx, uid)
		if err != nil || !st.MailScan {
			continue
		}
		if mode, _, token, _ := connSettings(st, appMail); s.connMode(mode, token) == modeOff {
			continue
		}
		if _, at, ok := s.store.CacheGet(ctx, fmt.Sprintf("mailscan:%d:ran", uid)); ok {
			if t, err := time.Parse(time.RFC3339Nano, at); err == nil && time.Since(t) < mailScanEvery {
				continue
			}
		}
		cctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
		if _, err := s.scanMail(cctx, uid); err != nil && !isNotConnected(err) {
			s.log.Debug("mail scan", "user", uid, "err", err)
		}
		cancel()
	}
}

// scanResult says what one scan did.
type scanResult struct {
	Read    int `json:"read"`    // emails looked at
	Found   int `json:"found"`   // new parcels waiting for an answer
	Added   int `json:"added"`   // parcels added at once
	Updated int `json:"updated"` // news for parcels already followed
}

// scanMail reads one person's new shipping emails.
func (s *Server) scanMail(ctx context.Context, uid int64) (scanResult, error) {
	var res scanResult
	s.mailMu.Lock()
	defer s.mailMu.Unlock()
	_ = s.store.CachePut(ctx, fmt.Sprintf("mailscan:%d:ran", uid), "1")
	st, err := s.store.GetSettings(ctx, uid)
	if err != nil {
		return res, err
	}
	conn, err := s.clients(ctx, uid).conn(ctx, appMail, scopeMailRead)
	if err != nil {
		return res, err
	}
	sinceKey := fmt.Sprintf("mailscan:%d:since", uid)
	days := st.MailDays
	if days <= 0 {
		days = 30
	}
	since := time.Now().AddDate(0, 0, -days)
	if v, _, ok := s.store.CacheGet(ctx, sinceKey); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			since = t.Add(-6 * time.Hour) // late-arriving mail
		}
	}
	newest := since
	seen := map[int64]bool{}
	var msgs []pocket.Message
	collect := func(category, q string) error {
		for page := 0; page < 4; page++ {
			list, more, err := conn.client.Messages(ctx, category, q, 50, page*50)
			if err != nil {
				return err
			}
			old := false
			for _, m := range list {
				t := track.ParseTime(m.Date)
				if !t.IsZero() && t.Before(since) {
					old = true
					continue
				}
				if !seen[m.ID] {
					seen[m.ID] = true
					msgs = append(msgs, m)
				}
			}
			if !more || old {
				return nil
			}
		}
		return nil
	}
	for _, c := range []string{"receipts", "updates"} {
		if err := collect(c, ""); err != nil {
			return res, err
		}
	}
	for _, q := range []string{"tracking", "shipped", "pošiljka"} {
		_ = collect("", q)
	}
	for _, m := range msgs {
		if ctx.Err() != nil {
			break
		}
		if t := track.ParseTime(m.Date); t.After(newest) {
			newest = t
		}
		doneKey := fmt.Sprintf("mailscan:%d:msg:%d", uid, m.ID)
		if _, _, done := s.store.CacheGet(ctx, doneKey); done {
			continue
		}
		if !track.LooksLikeShipping(m.Subject, m.Snippet) {
			_ = s.store.CachePut(ctx, doneKey, "skip")
			continue
		}
		full, err := conn.client.MessageBody(ctx, m.ID)
		if err != nil {
			continue // try again next time
		}
		res.Read++
		_ = s.store.CachePut(ctx, doneKey, "read")
		s.readEmail(ctx, uid, st, *full, &res)
	}
	if newest.After(since) {
		_ = s.store.CachePut(ctx, sinceKey, newest.UTC().Format(time.RFC3339))
	} else if _, _, ok := s.store.CacheGet(ctx, sinceKey); !ok {
		_ = s.store.CachePut(ctx, sinceKey, time.Now().UTC().Format(time.RFC3339))
	}
	if res.Found > 0 {
		s.publish(ctx, uid, "parcels.found_in_mail", fmt.Sprintf("mail:%d", uid), map[string]any{"count": res.Found})
	}
	return res, nil
}

// readEmail takes in one shipping email.
func (s *Server) readEmail(ctx context.Context, uid int64, st store.Settings, m pocket.MessageFull, res *scanResult) {
	text := m.Text
	if strings.TrimSpace(text) == "" {
		text = track.PlainText(m.HTML)
	}
	ships := track.Extract(track.Email{Subject: m.Subject, FromName: m.From.Name, FromAddr: m.From.Addr, Text: text, HTML: m.HTML})
	if len(ships) == 0 && s.aiExtract != nil {
		if got, err := s.aiExtract(ctx, uid, mailText{Subject: m.Subject, From: firstNonEmpty(m.From.Name, m.From.Addr), Text: text}); err == nil {
			shop := track.ShopName(m.From.Name, m.From.Addr)
			for _, g := range got {
				if g.Shop == "" {
					g.Shop = shop
				}
				if g.Title == "" {
					g.Title = defaultTitle(store.Parcel{Shop: g.Shop})
				}
				ships = append(ships, g)
			}
		}
	}
	at := track.ParseTime(m.Date)
	if at.IsZero() {
		at = time.Now()
	}
	for _, sh := range ships {
		f := store.Find{MailMessageID: m.ID, Subject: m.Subject, FromName: m.From.Name, FromAddr: m.From.Addr, ReceivedAt: at.UTC().Format(time.RFC3339),
			TrackingNumber: sh.Number, Carrier: sh.Carrier, Shop: sh.Shop, OrderNumber: sh.Order, Title: sh.Title, StatusHint: sh.Status}
		if p, err := s.store.ParcelByNumber(ctx, uid, sh.Number); err == nil {
			// news about a parcel already followed
			f.State, f.ParcelID = "updated", &p.ID
			if fresh, _ := s.store.SaveFind(ctx, uid, &f); fresh {
				if s.mailNews(ctx, uid, p, sh, m, at) {
					res.Updated++
				}
			}
			continue
		}
		if sh.Status == track.StatusDelivered || sh.Status == track.StatusReturned {
			// an old parcel that already arrived: nothing to follow
			f.State = "dismissed"
			_, _ = s.store.SaveFind(ctx, uid, &f)
			continue
		}
		if st.MailAutoAdd {
			p, err := s.parcelFromFind(ctx, uid, st, f)
			if err == nil {
				f.State, f.ParcelID = "added", &p.ID
				if fresh, _ := s.store.SaveFind(ctx, uid, &f); fresh {
					res.Added++
				}
				continue
			}
		}
		if fresh, _ := s.store.SaveFind(ctx, uid, &f); fresh {
			res.Found++
		}
	}
}

// mailNews adds what an email says to a parcel's timeline; it reports
// whether the parcel changed.
func (s *Server) mailNews(ctx context.Context, uid int64, p *store.Parcel, sh track.Shipment, m pocket.MessageFull, at time.Time) bool {
	if p.MailMessageID == nil {
		p.MailMessageID = &m.ID
	}
	if p.OrderNumber == "" && sh.Order != "" {
		p.OrderNumber = sh.Order
	}
	if sh.Status == "" {
		_ = s.store.UpdateParcel(ctx, uid, p)
		return false
	}
	from := firstNonEmpty(track.ShopName(m.From.Name, m.From.Addr), m.From.Addr)
	ev := track.Event{At: at.Format(time.RFC3339), Status: sh.Status, Description: "Email from " + from + ": " + strings.TrimSpace(m.Subject)}
	fresh, err := s.addEvents(ctx, uid, p, "mail", []track.Event{ev})
	if err != nil || len(fresh) == 0 {
		return false
	}
	before := *p
	changed := s.takeLatest(p, ev)
	_ = s.store.UpdateParcel(ctx, uid, p)
	if p.Status != before.Status {
		s.afterStatus(ctx, uid, before, p, "mail")
	}
	return changed
}

// parcelFromFind adds the parcel an email named.
func (s *Server) parcelFromFind(ctx context.Context, uid int64, st store.Settings, f store.Find) (*store.Parcel, error) {
	p := store.Parcel{Title: f.Title, TrackingNumber: f.TrackingNumber, Carrier: f.Carrier, Direction: "in", Shop: f.Shop, OrderNumber: f.OrderNumber,
		Currency: st.Currency, Status: track.StatusPending, Source: "mail", MailMessageID: &f.MailMessageID, OrderedOn: localDate(f.ReceivedAt)}
	if p.Carrier == "" {
		p.Carrier = track.Detect(p.TrackingNumber)
	}
	if f.StatusHint != "" && !track.Final(f.StatusHint) {
		p.Status, p.StatusText, p.StatusAt = f.StatusHint, "Email from "+firstNonEmpty(f.Shop, f.FromAddr)+": "+f.Subject, f.ReceivedAt
	}
	created, err := s.createParcel(ctx, uid, &p, st)
	if err != nil {
		return nil, err
	}
	ev := track.Event{At: f.ReceivedAt, Status: firstNonEmpty(f.StatusHint, track.StatusInfoReceived), Description: "Email from " + firstNonEmpty(f.Shop, f.FromAddr) + ": " + f.Subject}
	_, _ = s.addEvents(ctx, uid, created, "mail", []track.Event{ev})
	return created, nil
}

// ---------------- the finds API ----------------

func (s *Server) handleListFinds(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	fs, err := s.store.ListFinds(ctx, uid, r.URL.Query().Get("state"), queryInt(r, "limit"))
	if err != nil {
		s.fail(w, err, "finds")
		return
	}
	type findView struct {
		store.Find
		CarrierName string `json:"carrier_name"`
		MailURL     string `json:"mail_url"`
	}
	out := make([]findView, 0, len(fs))
	mailBase := s.siblingLink(ctx, uid, appMail, "")
	for _, f := range fs {
		v := findView{Find: f, CarrierName: track.Name(f.Carrier)}
		if mailBase != "" {
			v.MailURL = mailBase + "/message/" + strconv.FormatInt(f.MailMessageID, 10)
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"finds": out})
}

// handleAddFind adds the parcel a find names (title and shop may be changed first).
func (s *Server) handleAddFind(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	f, err := s.store.GetFind(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "find")
		return
	}
	var in struct {
		Title string `json:"title"`
		Shop  string `json:"shop"`
	}
	_ = decode(r, &in)
	if t := strings.TrimSpace(in.Title); t != "" {
		f.Title = t
	}
	if sh := strings.TrimSpace(in.Shop); sh != "" {
		f.Shop = sh
	}
	if old, err := s.store.ParcelByNumber(ctx, uid, f.TrackingNumber); err == nil {
		_ = s.store.SetFindState(ctx, uid, f.ID, "added", &old.ID)
		writeJSON(w, http.StatusOK, s.detail(ctx, uid, old))
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	p, err := s.parcelFromFind(ctx, uid, st, *f)
	if err != nil {
		s.fail(w, err, "add parcel")
		return
	}
	_ = s.store.SetFindState(ctx, uid, f.ID, "added", &p.ID)
	writeJSON(w, http.StatusCreated, s.detail(ctx, uid, p))
}

func (s *Server) handleDismissFind(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.store.SetFindState(ctx, actorID(ctx), idParam(r, "id"), "dismissed", nil); err != nil {
		s.fail(w, err, "find")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleScanMail looks through the email now (Settings, the Email page).
func (s *Server) handleScanMail(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	res, err := s.scanMail(ctx, actorID(ctx))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "result": res})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": res})
}
