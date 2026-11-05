package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-parcels/internal/store"
	"github.com/abjelosevic88/pocket-homelab-parcels/internal/track"
)

// parcelView is a parcel with what the screens and other apps need to show
// it: names, labels and the addresses of the pages it links to.
type parcelView struct {
	store.Parcel
	CarrierName string `json:"carrier_name"`
	StatusLabel string `json:"status_label"`
	TrackingURL string `json:"tracking_url"`
	URL         string `json:"url"` // the parcel in Pocket Parcels
	Stale       bool   `json:"stale"`
	ArrivesIn   *int   `json:"arrives_in"` // days until expected_on (negative = late)

	// pages in the other Pocket apps (detail only; "" when not known)
	MailURL    string `json:"mail_url,omitempty"`
	WishURL    string `json:"wish_url,omitempty"`
	MoneyURL   string `json:"money_url,omitempty"`
	ExtraURL   string `json:"extra_url,omitempty"`
	ItemURL    string `json:"item_url,omitempty"`
	TaskURL    string `json:"task_url,omitempty"`
	ReturnURL  string `json:"return_task_url,omitempty"`
	ContactURL string `json:"contact_url,omitempty"`
	CanCheck   bool   `json:"can_check"` // a tracking service follows it
	CheckWith  string `json:"check_with,omitempty"`
}

func (s *Server) view(p store.Parcel, st store.Settings, now time.Time) parcelView {
	v := parcelView{Parcel: p, CarrierName: track.Name(p.Carrier), StatusLabel: track.StatusLabel(p.Status),
		TrackingURL: track.TrackingURL(p.Carrier, p.TrackingNumber), URL: s.parcelURL(p.ID)}
	if v.CarrierName == "" && p.Carrier != "" {
		v.CarrierName = p.Carrier
	}
	if p.Active() && st.StaleDays > 0 {
		last := track.ParseTime(firstNonEmpty(p.StatusAt, p.CreatedAt))
		if last.IsZero() {
			last, _ = time.Parse(time.RFC3339Nano, p.CreatedAt)
		}
		v.Stale = !last.IsZero() && now.Sub(last) > time.Duration(st.StaleDays)*24*time.Hour
	}
	if p.ExpectedOn != "" && p.Active() {
		if d, err := time.ParseInLocation("2006-01-02", p.ExpectedOn, time.Local); err == nil {
			today, _ := time.ParseInLocation("2006-01-02", now.Format("2006-01-02"), time.Local)
			n := int(d.Sub(today).Hours() / 24)
			if p.ExpectedUntil != "" && n < 0 {
				if u, err := time.ParseInLocation("2006-01-02", p.ExpectedUntil, time.Local); err == nil && !u.Before(today) {
					n = 0
				}
			}
			v.ArrivesIn = &n
		}
	}
	v.CheckWith = s.providerFor(st, p)
	v.CanCheck = v.CheckWith != ""
	return v
}

func (s *Server) views(ctx context.Context, uid int64, ps []store.Parcel) []parcelView {
	st, _ := s.store.GetSettings(ctx, uid)
	now := time.Now()
	out := make([]parcelView, 0, len(ps))
	for _, p := range ps {
		out = append(out, s.view(p, st, now))
	}
	return out
}

// detail is one parcel with its timeline, papers and the pages it links to.
func (s *Server) detail(ctx context.Context, uid int64, p *store.Parcel) parcelView {
	st, _ := s.store.GetSettings(ctx, uid)
	p.Events, _ = s.store.ListEvents(ctx, uid, p.ID)
	p.Attachments, _ = s.store.ListAttachments(ctx, uid, p.ID)
	v := s.view(*p, st, time.Now())
	link := func(a pocketApp, id *int64, path string) string {
		if id == nil {
			return ""
		}
		return s.siblingLink(ctx, uid, a, fmt.Sprintf(path, *id))
	}
	v.MailURL = link(appMail, p.MailMessageID, "/message/%d")
	v.WishURL = link(appWishlist, p.WishID, "/wishes/%d")
	v.MoneyURL = link(appMoney, p.MoneyTxID, "/transactions/%d")
	v.ExtraURL = link(appMoney, p.ExtraTxID, "/transactions/%d")
	v.ItemURL = link(appInventory, p.InventoryItemID, "/items/%d")
	v.TaskURL = link(appTasks, p.TaskID, "/?task=%d")
	v.ReturnURL = link(appTasks, p.ReturnTaskID, "/?task=%d")
	v.ContactURL = link(appContacts, p.ContactID, "/?contact=%d")
	return v
}

func (s *Server) parcelURL(id int64) string {
	return strings.TrimRight(s.cfg.BaseURL, "/") + fmt.Sprintf("/parcels/%d", id)
}

// ---------------- list, read ----------------

func (s *Server) handleListParcels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	q := r.URL.Query()
	f := store.ParcelFilter{View: q.Get("view"), Q: q.Get("q"), Shop: q.Get("shop"), Carrier: q.Get("carrier"), Direction: q.Get("direction"),
		Status: q.Get("status"), Limit: queryInt(r, "limit"), Offset: queryInt(r, "offset")}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 200
	}
	ps, total, err := s.store.ListParcels(ctx, uid, f)
	if err != nil {
		s.fail(w, err, "parcels")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"parcels": s.views(ctx, uid, ps), "total": total})
}

func (s *Server) handleGetParcel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	p, err := s.store.GetParcel(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "parcel")
		return
	}
	writeJSON(w, http.StatusOK, s.detail(ctx, uid, p))
}

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	id := idParam(r, "id")
	if _, err := s.store.GetParcel(ctx, uid, id); err != nil {
		s.fail(w, err, "parcel")
		return
	}
	ev, err := s.store.ListEvents(ctx, uid, id)
	if err != nil {
		s.fail(w, err, "events")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": ev})
}

// ---------------- create, change ----------------

// parcelInput is what a person (or another app) sends to add or change a
// parcel. Pointers: only what is sent changes.
type parcelInput struct {
	Title          *string `json:"title"`
	TrackingNumber *string `json:"tracking_number"`
	Carrier        *string `json:"carrier"`
	Direction      *string `json:"direction"`
	Shop           *string `json:"shop"`
	OrderNumber    *string `json:"order_number"`
	OrderedOn      *string `json:"ordered_on"`
	Price          *int64  `json:"price"`
	ExtraCost      *int64  `json:"extra_cost"`
	Currency       *string `json:"currency"`
	Status         *string `json:"status"`
	ExpectedOn     *string `json:"expected_on"`
	ExpectedUntil  *string `json:"expected_until"`
	ReturnBy       *string `json:"return_by"`
	ReturnState    *string `json:"return_state"`
	Notes          *string `json:"notes"`
	Archived       *bool   `json:"archived"`
	ContactID      *int64  `json:"contact_id"`
	ContactName    *string `json:"contact_name"`
	WishID         *int64  `json:"wish_id"`
	WishTitle      *string `json:"wish_title"`
	MoneyTxID      *int64  `json:"money_tx_id"`
	MoneyTxLabel   *string `json:"money_tx_label"`
	MailMessageID  *int64  `json:"mail_message_id"`
	ExternalID     *string `json:"external_id"`
}

// apply checks an input and copies it onto p; keys says which were sent.
func (in parcelInput) apply(p *store.Parcel, keys map[string]bool) error {
	str := func(k string, dst *string, v *string, max int) {
		if keys[k] && v != nil {
			t := strings.TrimSpace(*v)
			if len(t) > max {
				t = t[:max]
			}
			*dst = t
		}
	}
	date := func(k string, dst *string, v *string) error {
		if !keys[k] || v == nil {
			return nil
		}
		t := strings.TrimSpace(*v)
		if t != "" && !validDate(t) {
			return fmt.Errorf("%s is a date like 2026-11-20", strings.ReplaceAll(k, "_", " "))
		}
		*dst = t
		return nil
	}
	id := func(k string, dst **int64, v *int64) {
		if keys[k] {
			if v == nil || *v <= 0 {
				*dst = nil
			} else {
				x := *v
				*dst = &x
			}
		}
	}
	if keys["tracking_number"] && in.TrackingNumber != nil {
		n := track.Normalize(*in.TrackingNumber)
		if n != "" && !track.ValidNumber(n) {
			return errors.New("that does not look like a tracking number")
		}
		if n != p.TrackingNumber {
			p.TrackingNumber = n
			p.ProviderRef, p.CheckError, p.NextCheckAt = "", "", ""
			if !keys["carrier"] {
				p.Carrier = track.Detect(n)
			}
		}
	}
	if keys["carrier"] && in.Carrier != nil {
		c := strings.ToLower(strings.TrimSpace(*in.Carrier))
		if c != "" {
			if _, ok := track.Lookup(c); !ok {
				return errors.New("unknown carrier")
			}
		}
		if c == "" {
			c = track.Detect(p.TrackingNumber)
		}
		p.Carrier = c
	}
	str("title", &p.Title, in.Title, 200)
	if keys["direction"] && in.Direction != nil {
		switch *in.Direction {
		case "in", "out":
			p.Direction = *in.Direction
		default:
			return errors.New("direction is in or out")
		}
	}
	str("shop", &p.Shop, in.Shop, 120)
	str("order_number", &p.OrderNumber, in.OrderNumber, 80)
	if err := date("ordered_on", &p.OrderedOn, in.OrderedOn); err != nil {
		return err
	}
	if keys["price"] {
		if in.Price != nil && *in.Price < 0 {
			return errors.New("the price cannot be negative")
		}
		p.Price = in.Price
	}
	if keys["extra_cost"] {
		if in.ExtraCost != nil && *in.ExtraCost < 0 {
			return errors.New("the extra cost cannot be negative")
		}
		p.ExtraCost = in.ExtraCost
	}
	if keys["currency"] && in.Currency != nil {
		c := cleanCurrency(*in.Currency)
		if c == "" {
			return errors.New("currency is a three-letter code like EUR")
		}
		p.Currency = c
	}
	if keys["status"] && in.Status != nil {
		if !track.ValidStatus(*in.Status) {
			return errors.New("unknown status")
		}
		p.Status = *in.Status
	}
	if keys["expected_on"] || keys["expected_until"] {
		if err := date("expected_on", &p.ExpectedOn, in.ExpectedOn); err != nil {
			return err
		}
		if err := date("expected_until", &p.ExpectedUntil, in.ExpectedUntil); err != nil {
			return err
		}
		if !keys["expected_until"] {
			p.ExpectedUntil = "" // one day typed: no range
		}
		if p.ExpectedUntil != "" && p.ExpectedOn == "" {
			p.ExpectedOn, p.ExpectedUntil = p.ExpectedUntil, ""
		}
		if p.ExpectedUntil != "" && p.ExpectedUntil < p.ExpectedOn {
			p.ExpectedOn, p.ExpectedUntil = p.ExpectedUntil, p.ExpectedOn
		}
		if p.ExpectedUntil == p.ExpectedOn {
			p.ExpectedUntil = ""
		}
		p.ExpectedManual = p.ExpectedOn != ""
	}
	if err := date("return_by", &p.ReturnBy, in.ReturnBy); err != nil {
		return err
	}
	if keys["return_state"] && in.ReturnState != nil {
		switch *in.ReturnState {
		case "", "keeping", "returning", "returned":
			p.ReturnState = *in.ReturnState
		default:
			return errors.New("return state is keeping, returning or returned")
		}
	}
	str("notes", &p.Notes, in.Notes, 4000)
	if keys["archived"] && in.Archived != nil {
		p.Archived = *in.Archived
	}
	id("contact_id", &p.ContactID, in.ContactID)
	str("contact_name", &p.ContactName, in.ContactName, 120)
	id("wish_id", &p.WishID, in.WishID)
	str("wish_title", &p.WishTitle, in.WishTitle, 200)
	if keys["wish_id"] && p.WishID == nil {
		p.WishTitle = ""
	}
	id("money_tx_id", &p.MoneyTxID, in.MoneyTxID)
	str("money_tx_label", &p.MoneyTxLabel, in.MoneyTxLabel, 200)
	if keys["money_tx_id"] && p.MoneyTxID == nil {
		p.MoneyTxLabel = ""
	}
	id("mail_message_id", &p.MailMessageID, in.MailMessageID)
	str("external_id", &p.ExternalID, in.ExternalID, 120)
	return nil
}

// defaultTitle names a parcel nobody named.
func defaultTitle(p store.Parcel) string {
	switch {
	case p.Title != "":
		return p.Title
	case p.Direction == "out" && p.ContactName != "":
		return "Parcel to " + p.ContactName
	case p.Shop != "":
		return "Order from " + p.Shop
	case p.Carrier != "":
		return track.Name(p.Carrier) + " parcel"
	}
	return "Parcel"
}

func (s *Server) handleCreateParcel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in parcelInput
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	p := store.Parcel{Direction: "in", Currency: st.Currency, Status: track.StatusPending, Source: "manual"}
	if app := linkApp(ctx); app != "" {
		p.Source = app
	}
	if err := in.apply(&p, keys); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if p.TrackingNumber == "" && p.Title == "" {
		writeErr(w, http.StatusBadRequest, "add a tracking number, or at least say what is coming")
		return
	}
	// another app sending the same thing again gets the first one back
	if p.ExternalID != "" {
		if old, err := s.store.ParcelByExternal(ctx, uid, p.Source, p.ExternalID); err == nil {
			writeJSON(w, http.StatusOK, s.detail(ctx, uid, old))
			return
		}
	}
	created, err := s.createParcel(ctx, uid, &p, st)
	if errors.Is(err, store.ErrConflict) {
		old, _ := s.store.ParcelByNumber(ctx, uid, p.TrackingNumber)
		body := map[string]any{"error": "you already follow this tracking number"}
		if old != nil {
			body["id"] = old.ID
		}
		writeJSON(w, http.StatusConflict, body)
		return
	}
	if err != nil {
		s.fail(w, err, "create parcel")
		return
	}
	writeJSON(w, http.StatusCreated, s.detail(ctx, uid, created))
}

// createParcel saves a new parcel, then in the background marks its wish
// bought, checks it once and puts it on the calendar.
func (s *Server) createParcel(ctx context.Context, uid int64, p *store.Parcel, st store.Settings) (*store.Parcel, error) {
	p.Title = defaultTitle(*p)
	if p.Status == "" {
		p.Status = track.StatusPending
	}
	if p.Status == track.StatusDelivered && p.DeliveredAt == "" {
		p.DeliveredAt = time.Now().UTC().Format(time.RFC3339)
	}
	p.Provider = s.providerFor(st, *p)
	if err := s.store.CreateParcel(ctx, uid, p); err != nil {
		return nil, err
	}
	s.publish(ctx, uid, "parcels.added", parcelSubject(p.ID), s.eventPayload(*p))
	// the parcel is the wish: tell Wishlist it was bought
	if p.WishID != nil && st.WishlistBought && p.Direction == "in" {
		s.markWishBoughtSoon(uid, p.ID)
	}
	if p.Provider != "" {
		s.checkSoon(uid, p.ID)
	}
	s.syncCalendarSoon(uid)
	return s.store.GetParcel(ctx, uid, p.ID)
}

func parcelSubject(id int64) string { return fmt.Sprintf("parcel:%d", id) }

func (s *Server) handleUpdateParcel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	p, err := s.store.GetParcel(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "parcel")
		return
	}
	if app := linkApp(ctx); app != "" && p.Source != app {
		writeErr(w, http.StatusForbidden, "this app may change only the parcels it added")
		return
	}
	var in parcelInput
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	before := *p
	if err := in.apply(p, keys); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if p.TrackingNumber == "" && p.Title == "" {
		writeErr(w, http.StatusBadRequest, "a parcel needs a tracking number or a name")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	if keys["status"] && p.Status != before.Status {
		s.statusByHand(p, before.Status)
	}
	p.Provider = s.providerFor(st, *p)
	if err := s.store.UpdateParcel(ctx, uid, p); err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeErr(w, http.StatusConflict, "you already follow this tracking number in another parcel")
			return
		}
		s.fail(w, err, "update parcel")
		return
	}
	if p.Status != before.Status {
		s.afterStatus(ctx, uid, before, p, "manual")
	}
	if keys["return_state"] && p.ReturnState == "returning" && before.ReturnState != "returning" && st.TasksReturns && p.ReturnTaskID == nil {
		s.returnTaskSoon(uid, p.ID)
	}
	if keys["wish_id"] && p.WishID != nil && (before.WishID == nil || *before.WishID != *p.WishID) && st.WishlistBought && p.Direction == "in" {
		s.markWishBoughtSoon(uid, p.ID)
	}
	if p.TrackingNumber != before.TrackingNumber && p.Provider != "" {
		s.checkSoon(uid, p.ID)
	}
	s.syncCalendarSoon(uid)
	p, _ = s.store.GetParcel(ctx, uid, p.ID)
	writeJSON(w, http.StatusOK, s.detail(ctx, uid, p))
}

// statusByHand fills in what a status chosen by the person implies.
func (s *Server) statusByHand(p *store.Parcel, was string) {
	now := time.Now().UTC().Format(time.RFC3339)
	p.StatusAt = now
	p.StatusText = track.StatusLabel(p.Status) + " (set by hand)"
	if p.Status == track.StatusDelivered && p.DeliveredAt == "" {
		p.DeliveredAt = now
	}
	if p.Status != track.StatusDelivered && was == track.StatusDelivered {
		p.DeliveredAt = ""
	}
}

func (s *Server) handleDeleteParcel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	p, err := s.store.GetParcel(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "parcel")
		return
	}
	if app := linkApp(ctx); app != "" && p.Source != app {
		writeErr(w, http.StatusForbidden, "this app may remove only the parcels it added")
		return
	}
	if err := s.store.DeleteParcel(ctx, uid, p.ID); err != nil {
		s.fail(w, err, "delete parcel")
		return
	}
	s.dropCalendarEvents(uid, []store.Parcel{*p})
	s.stopTrackingSoon(uid, *p)
	w.WriteHeader(http.StatusNoContent)
}

// handleDelivered marks a parcel delivered by hand ("It's here").
func (s *Server) handleDelivered(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	p, err := s.store.GetParcel(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "parcel")
		return
	}
	var in struct {
		At string `json:"at"`
	}
	_ = decode(r, &in)
	at := time.Now()
	if t := track.ParseTime(in.At); !t.IsZero() {
		at = t
	} else if validDate(in.At) && in.At != "" {
		at, _ = time.ParseInLocation("2006-01-02 15:04", in.At+" 12:00", time.Local)
	}
	if p.Status != track.StatusDelivered {
		before := *p
		ev := track.Event{At: at.Format(time.RFC3339), Status: track.StatusDelivered, Description: "Delivered (marked by you)"}
		_, _ = s.addEvents(ctx, uid, p, "manual", []track.Event{ev})
		p.Status, p.StatusText, p.StatusAt, p.DeliveredAt, p.Location = track.StatusDelivered, ev.Description, ev.At, ev.At, ""
		if err := s.store.UpdateParcel(ctx, uid, p); err != nil {
			s.fail(w, err, "delivered")
			return
		}
		s.afterStatus(ctx, uid, before, p, "manual")
		s.syncCalendarSoon(uid)
	}
	p, _ = s.store.GetParcel(ctx, uid, p.ID)
	writeJSON(w, http.StatusOK, s.detail(ctx, uid, p))
}

// handleAddEvent adds a step by hand (or from another app): "Picked up from
// the neighbour", "Customs charged 12 KM".
func (s *Server) handleAddEvent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	p, err := s.store.GetParcel(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "parcel")
		return
	}
	source := "manual"
	if app := linkApp(ctx); app != "" {
		if p.Source != app {
			writeErr(w, http.StatusForbidden, "this app may add news only to the parcels it added")
			return
		}
		source = app
	}
	var in struct {
		At          string `json:"at"`
		Status      string `json:"status"`
		Description string `json:"description"`
		Location    string `json:"location"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	in.Description = strings.TrimSpace(in.Description)
	if in.Description == "" {
		writeErr(w, http.StatusBadRequest, "say what happened")
		return
	}
	if in.Status == "" {
		in.Status = firstNonEmpty(track.Classify(in.Description), track.StatusInTransit)
	}
	if !track.ValidStatus(in.Status) {
		writeErr(w, http.StatusBadRequest, "unknown status")
		return
	}
	at := time.Now()
	if t := track.ParseTime(in.At); !t.IsZero() {
		at = t
	} else if in.At != "" {
		writeErr(w, http.StatusBadRequest, "at is a time like 2026-11-05T14:30:00+01:00")
		return
	}
	before := *p
	ev := track.Event{At: at.Format(time.RFC3339), Status: in.Status, Description: in.Description, Location: strings.TrimSpace(in.Location)}
	if _, err := s.addEvents(ctx, uid, p, source, []track.Event{ev}); err != nil {
		s.fail(w, err, "event")
		return
	}
	if s.takeLatest(p, ev) {
		if err := s.store.UpdateParcel(ctx, uid, p); err != nil {
			s.fail(w, err, "event")
			return
		}
		if p.Status != before.Status {
			s.afterStatus(ctx, uid, before, p, source)
		}
	}
	p, _ = s.store.GetParcel(ctx, uid, p.ID)
	writeJSON(w, http.StatusCreated, s.detail(ctx, uid, p))
}

func (s *Server) handleDeleteEvent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	e, err := s.store.GetEvent(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "event")
		return
	}
	if e.Source != "manual" {
		writeErr(w, http.StatusConflict, "only steps you added yourself can be removed")
		return
	}
	if err := s.store.DeleteEvent(ctx, uid, e.ID); err != nil {
		s.fail(w, err, "event")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleCheck asks the tracking service now.
func (s *Server) handleCheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	p, err := s.store.GetParcel(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "parcel")
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	changed, err := s.checkParcel(cctx, uid, p)
	p, _ = s.store.GetParcel(ctx, uid, p.ID)
	out := map[string]any{"parcel": s.detail(ctx, uid, p), "changed": changed}
	if err != nil {
		out["error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, out)
}

// handleDetect says what Parcels makes of a number while it is typed.
func (s *Server) handleDetect(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Number string `json:"number"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ctx := r.Context()
	uid := actorID(ctx)
	n := track.Normalize(in.Number)
	c := track.Detect(n)
	st, _ := s.store.GetSettings(ctx, uid)
	out := map[string]any{"number": n, "valid": track.ValidNumber(n), "carrier": c, "carrier_name": track.Name(c), "tracking_url": track.TrackingURL(c, n),
		"check_with": s.providerFor(st, store.Parcel{TrackingNumber: n, Carrier: c, Status: track.StatusPending})}
	if old, err := s.store.ParcelByNumber(ctx, uid, n); err == nil {
		out["existing_id"], out["existing_title"] = old.ID, old.Title
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCarriers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"carriers": track.List()})
}

func (s *Server) handleShops(w http.ResponseWriter, r *http.Request) {
	shops, err := s.store.Shops(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "shops")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"shops": shops})
}

// ---------------- papers ----------------

func (s *Server) handleCreateAttachment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		ParcelID int64  `json:"parcel_id"`
		Kind     string `json:"kind"`
		Ref      *int64 `json:"ref"`
		URL      string `json:"url"`
		Title    string `json:"title"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a := store.Attachment{ParcelID: in.ParcelID, Kind: in.Kind, Ref: in.Ref, Title: strings.TrimSpace(in.Title)}
	switch in.Kind {
	case "document":
		if in.Ref == nil || *in.Ref <= 0 {
			writeErr(w, http.StatusBadRequest, "choose a document")
			return
		}
		if docs := s.siblingLink(ctx, uid, appDocuments, fmt.Sprintf("/documents/%d", *in.Ref)); docs != "" {
			a.URL = docs
		}
	case "url":
		u, ok := cleanURL(in.URL)
		if !ok {
			writeErr(w, http.StatusBadRequest, "the link starts with http:// or https://")
			return
		}
		a.URL = u
	default:
		writeErr(w, http.StatusBadRequest, "kind is document or url")
		return
	}
	if a.Title == "" {
		a.Title = a.URL
	}
	if err := s.store.CreateAttachment(ctx, uid, &a); err != nil {
		s.fail(w, err, "attachment")
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) handleDeleteAttachment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.store.DeleteAttachment(ctx, actorID(ctx), idParam(r, "id")); err != nil {
		s.fail(w, err, "attachment")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------- overview, summary, day, search, stats ----------------

// handleOverview is the home screen: what needs you today, what is on the
// way, problems, returns, and what arrived lately.
func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	active, _, err := s.store.ListParcels(ctx, uid, store.ParcelFilter{View: "active"})
	if err != nil {
		s.fail(w, err, "overview")
		return
	}
	now := time.Now()
	today := now.Format("2006-01-02")
	var needs, coming, problems []parcelView
	for _, p := range active {
		v := s.view(p, st, now)
		switch {
		case p.Status == track.StatusOutForDelivery || p.Status == track.StatusPickup || (v.ArrivesIn != nil && *v.ArrivesIn == 0):
			needs = append(needs, v)
		case p.Status == track.StatusException || v.Stale:
			problems = append(problems, v)
		default:
			coming = append(coming, v)
		}
	}
	done, _, _ := s.store.ListParcels(ctx, uid, store.ParcelFilter{View: "delivered", Limit: 8})
	var recent []parcelView
	for _, p := range done {
		if t := track.ParseTime(firstNonEmpty(p.DeliveredAt, p.StatusAt)); !t.IsZero() && now.Sub(t) < 10*24*time.Hour {
			recent = append(recent, s.view(p, st, now))
		}
	}
	returns, _, _ := s.store.ListParcels(ctx, uid, store.ParcelFilter{View: "returns", Limit: 10})
	month := now.Format("2006-01")
	all, _ := s.store.AllParcels(ctx, uid)
	deliveredMonth := 0
	for _, p := range all {
		if p.Status == track.StatusDelivered && strings.HasPrefix(localDate(p.DeliveredAt), month) {
			deliveredMonth++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"date": today, "needs_you": nonNil(needs), "on_the_way": nonNil(coming), "problems": nonNil(problems), "recent": nonNil(recent),
		"returns": s.views(ctx, uid, returns), "finds": s.store.CountFinds(ctx, uid),
		"counts":   map[string]int{"active": len(active), "needs_you": len(needs), "on_the_way": len(coming), "problems": len(problems), "returns": len(returns), "delivered_month": deliveredMonth},
		"tracking": map[string]any{"cainiao": st.TrackCainiao, "seventeen": st.Track17Key != ""},
	})
}

func nonNil[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

// localDate is the local day of an RFC 3339 time, "" when it is not one.
func localDate(at string) string {
	t := track.ParseTime(at)
	if t.IsZero() {
		return ""
	}
	return t.In(time.Local).Format("2006-01-02")
}

// handleSummary is the short version for Pocket AI and dashboards: what
// arrives today, waits for pick-up, has a problem, and the returns due.
func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	active, _, err := s.store.ListParcels(ctx, uid, store.ParcelFilter{View: "active"})
	if err != nil {
		s.fail(w, err, "summary")
		return
	}
	now := time.Now()
	type line struct {
		ID         int64  `json:"id"`
		Title      string `json:"title"`
		Shop       string `json:"shop,omitempty"`
		Carrier    string `json:"carrier,omitempty"`
		Status     string `json:"status"`
		Label      string `json:"status_label"`
		Text       string `json:"latest,omitempty"`
		Location   string `json:"location,omitempty"`
		ExpectedOn string `json:"expected_on,omitempty"`
		ReturnBy   string `json:"return_by,omitempty"`
		URL        string `json:"url"`
	}
	mk := func(v parcelView) line {
		return line{v.ID, v.Title, v.Shop, v.CarrierName, v.Status, v.StatusLabel, v.StatusText, v.Location, v.ExpectedOn, v.ReturnBy, v.URL}
	}
	out := map[string][]line{"today": {}, "pickup": {}, "problems": {}, "on_the_way": {}, "returns_due": {}, "delivered_today": {}}
	for _, p := range active {
		v := s.view(p, st, now)
		switch {
		case p.Status == track.StatusPickup:
			out["pickup"] = append(out["pickup"], mk(v))
		case p.Status == track.StatusOutForDelivery || (v.ArrivesIn != nil && *v.ArrivesIn == 0):
			out["today"] = append(out["today"], mk(v))
		case p.Status == track.StatusException || v.Stale:
			out["problems"] = append(out["problems"], mk(v))
		default:
			out["on_the_way"] = append(out["on_the_way"], mk(v))
		}
	}
	returns, _, _ := s.store.ListParcels(ctx, uid, store.ParcelFilter{View: "returns"})
	for _, p := range returns {
		if p.ReturnBy <= store.AddDays(now.Format("2006-01-02"), 7) {
			out["returns_due"] = append(out["returns_due"], mk(s.view(p, st, now)))
		}
	}
	done, _, _ := s.store.ListParcels(ctx, uid, store.ParcelFilter{View: "delivered", Limit: 20})
	for _, p := range done {
		if localDate(p.DeliveredAt) == now.Format("2006-01-02") {
			out["delivered_today"] = append(out["delivered_today"], mk(s.view(p, st, now)))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"date": now.Format("2006-01-02"), "counts": map[string]int{
		"today": len(out["today"]), "pickup": len(out["pickup"]), "problems": len(out["problems"]), "on_the_way": len(out["on_the_way"]),
		"returns_due": len(out["returns_due"]), "delivered_today": len(out["delivered_today"]), "emails_to_check": s.store.CountFinds(ctx, uid)},
		"parcels": out})
}

// handleDay is one day for a journal (Pocket Day): what was delivered,
// what was sent, what waited for pick-up and what was expected.
func (s *Server) handleDay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	date := chi.URLParam(r, "date")
	if !validDate(date) || date == "" {
		writeErr(w, http.StatusBadRequest, "the date is YYYY-MM-DD")
		return
	}
	day, _ := time.ParseInLocation("2006-01-02", date, time.Local)
	from, to := day.UTC().Format(time.RFC3339), day.AddDate(0, 0, 1).UTC().Format(time.RFC3339)
	all, err := s.store.AllParcels(ctx, uid)
	if err != nil {
		s.fail(w, err, "day")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	now := time.Now()
	byID := map[int64]store.Parcel{}
	delivered, sent, expected, added := []parcelView{}, []parcelView{}, []parcelView{}, []parcelView{}
	for _, p := range all {
		byID[p.ID] = p
		switch {
		case p.Status == track.StatusDelivered && localDate(p.DeliveredAt) == date:
			delivered = append(delivered, s.view(p, st, now))
		case p.Active() && p.ExpectedOn != "" && p.ExpectedOn <= date && (p.ExpectedUntil == "" && p.ExpectedOn == date || p.ExpectedUntil >= date):
			expected = append(expected, s.view(p, st, now))
		}
		if p.Direction == "out" && (p.OrderedOn == date || p.OrderedOn == "" && localDate(p.CreatedAt) == date) {
			sent = append(sent, s.view(p, st, now))
		} else if p.Direction == "in" && localDate(p.CreatedAt) == date {
			added = append(added, s.view(p, st, now))
		}
	}
	// the day's steps, with the parcel's name
	evs, _ := s.store.EventsBetween(ctx, uid, from, to)
	steps := []map[string]any{}
	for _, e := range evs {
		p, ok := byID[e.ParcelID]
		if !ok {
			continue
		}
		steps = append(steps, map[string]any{"parcel_id": p.ID, "title": p.Title, "at": e.At, "status": e.Status, "status_label": track.StatusLabel(e.Status),
			"description": e.Description, "location": e.Location, "url": s.parcelURL(p.ID)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"date": date, "delivered": delivered, "sent": sent, "added": added, "expected": expected, "steps": steps})
}

// handleSearch finds parcels by name, number, shop, order or the person
// it goes to.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"parcels": []any{}})
		return
	}
	limit := queryInt(r, "limit")
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	ps, _, err := s.store.ListParcels(ctx, uid, store.ParcelFilter{View: "all", Q: q, Limit: limit})
	if err != nil {
		s.fail(w, err, "search")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"parcels": s.views(ctx, uid, ps)})
}

// handleStats: parcels per month, how long they took, per shop and carrier,
// and what was spent (orders and extra costs, in each currency).
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	months := queryInt(r, "months")
	if months <= 0 || months > 60 {
		months = 12
	}
	all, err := s.store.AllParcels(ctx, uid)
	if err != nil {
		s.fail(w, err, "stats")
		return
	}
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local).AddDate(0, -(months - 1), 0)
	type month struct {
		Month     string  `json:"month"`
		Parcels   int     `json:"parcels"`
		Delivered int     `json:"delivered"`
		AvgDays   float64 `json:"avg_days"`
		days      int
	}
	ms := make([]*month, months)
	idx := map[string]int{}
	for i := range ms {
		m := start.AddDate(0, i, 0).Format("2006-01")
		ms[i] = &month{Month: m}
		idx[m] = i
	}
	type tally struct {
		Name      string  `json:"name"`
		Parcels   int     `json:"parcels"`
		Delivered int     `json:"delivered"`
		AvgDays   float64 `json:"avg_days"`
		days      int
	}
	shops, carriers := map[string]*tally{}, map[string]*tally{}
	spent := map[string]int64{}
	extra := map[string]int64{}
	totalDays, delivered := 0, 0
	for _, p := range all {
		when := firstNonEmpty(p.OrderedOn, localDate(p.CreatedAt))
		if len(when) < 7 {
			continue
		}
		i, in := idx[when[:7]]
		if !in {
			continue
		}
		ms[i].Parcels++
		took := -1
		if p.Status == track.StatusDelivered && p.Days != nil {
			took = *p.Days
			ms[i].Delivered++
			ms[i].days += took
			totalDays += took
			delivered++
		}
		add := func(m map[string]*tally, key, name string) {
			if key == "" {
				return
			}
			t := m[strings.ToLower(key)]
			if t == nil {
				t = &tally{Name: name}
				m[strings.ToLower(key)] = t
			}
			t.Parcels++
			if took >= 0 {
				t.Delivered++
				t.days += took
			}
		}
		add(shops, p.Shop, p.Shop)
		add(carriers, p.Carrier, firstNonEmpty(track.Name(p.Carrier), p.Carrier))
		if p.Price != nil {
			spent[p.Currency] += *p.Price
		}
		if p.ExtraCost != nil {
			extra[p.Currency] += *p.ExtraCost
		}
	}
	for _, m := range ms {
		if m.Delivered > 0 {
			m.AvgDays = float64(m.days) / float64(m.Delivered)
		}
	}
	list := func(m map[string]*tally) []*tally {
		out := make([]*tally, 0, len(m))
		for _, t := range m {
			if t.Delivered > 0 {
				t.AvgDays = float64(t.days) / float64(t.Delivered)
			}
			out = append(out, t)
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Parcels != out[j].Parcels {
				return out[i].Parcels > out[j].Parcels
			}
			return out[i].Name < out[j].Name
		})
		if len(out) > 10 {
			out = out[:10]
		}
		return out
	}
	avg := 0.0
	if delivered > 0 {
		avg = float64(totalDays) / float64(delivered)
	}
	writeJSON(w, http.StatusOK, map[string]any{"months": ms, "shops": list(shops), "carriers": list(carriers), "spent": spent, "extra": extra,
		"avg_days": avg, "delivered": delivered})
}
