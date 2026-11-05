package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-parcels/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-parcels/internal/store"
	"github.com/abjelosevic88/pocket-homelab-parcels/internal/track"
)

// ---------- Pocket Calendar: delivery days and return deadlines ----------
//
// With "Show deliveries on my calendar" on, every parcel on its way with an
// expected day has one all-day event on that day in the person's Pocket
// Calendar; with "Return deadlines" on, a parcel that may still go back has
// one on its last day. A parcel remembers what its events show (cal_synced,
// return_synced); any change clears that and the next sync (right after the
// change, and every ten minutes) moves the event. Delivered, archived and
// deleted parcels lose theirs. Calendar lets an app change only the events
// it created, so the person's own events are never touched.

func deliveryText(p store.Parcel) (title, desc, date string) {
	title = "Delivery: " + p.Title
	if p.Direction == "out" {
		title = "Arrives: " + p.Title
	}
	if p.ExpectedUntil != "" {
		title += " (by " + p.ExpectedUntil[5:] + ")"
	}
	desc = "Expected delivery (Pocket Parcels)."
	if c := track.Name(p.Carrier); c != "" {
		desc += "\n" + c
		if p.TrackingNumber != "" {
			desc += " " + p.TrackingNumber
		}
	}
	if p.StatusText != "" {
		desc += "\nLatest: " + p.StatusText
	}
	return title, desc, p.ExpectedOn
}

func returnText(p store.Parcel) (title, desc, date string) {
	title = "Last day to send back: " + p.Title
	desc = "Return deadline (Pocket Parcels)."
	if p.Shop != "" {
		desc += "\nShop: " + p.Shop
	}
	if p.OrderNumber != "" {
		desc += "\nOrder " + p.OrderNumber
	}
	return title, desc, p.ReturnBy
}

func syncedKey(calID int64, title, desc, date string) string {
	return fmt.Sprintf("%d|%s|%s|%x", calID, date, title, len(desc))
}

func deliveryLive(st store.Settings, p store.Parcel) bool {
	return st.CalendarSync && p.Active() && p.ExpectedOn != ""
}

func returnLive(st store.Settings, p store.Parcel) bool {
	return st.CalendarSync && st.CalendarReturns && !p.Archived && p.ReturnBy != "" && p.ReturnState != "keeping" && p.ReturnState != "returned"
}

// syncCalendarSoon syncs one person's calendar in the background.
func (s *Server) syncCalendarSoon(uid int64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		_, _ = s.syncCalendar(ctx, uid)
	}()
}

func (s *Server) syncAllCalendars(ctx context.Context) {
	ids, err := s.store.SettingsUsers(ctx)
	if err != nil {
		return
	}
	for _, uid := range ids {
		if _, err := s.syncCalendar(ctx, uid); err != nil && !isNotConnected(err) {
			s.log.Debug("calendar sync", "user", uid, "err", err)
		}
	}
}

// calWork is one event to create, move or remove.
type calWork struct {
	p     store.Parcel
	which string // delivery | return
	live  bool
	id    *int64
	key   string
	title string
	desc  string
	date  string
}

// syncCalendar brings one person's events in line with their parcels and
// says how many it created, moved and removed.
func (s *Server) syncCalendar(ctx context.Context, uid int64) (map[string]int, error) {
	s.calMu.Lock()
	defer s.calMu.Unlock()
	st, err := s.store.GetSettings(ctx, uid)
	if err != nil {
		return nil, err
	}
	ps, err := s.store.AllParcels(ctx, uid)
	if err != nil {
		return nil, err
	}
	n := map[string]int{"created": 0, "moved": 0, "removed": 0, "failed": 0}
	var work []calWork
	for _, p := range ps {
		t, d, date := deliveryText(p)
		k := syncedKey(st.CalendarID, t, d, date)
		if live := deliveryLive(st, p); (live && p.CalSynced != k) || (!live && p.CalEventID != nil) {
			work = append(work, calWork{p, "delivery", live, p.CalEventID, k, t, d, date})
		}
		t, d, date = returnText(p)
		k = syncedKey(st.CalendarID, t, d, date)
		if live := returnLive(st, p); (live && p.ReturnSynced != k) || (!live && p.ReturnEventID != nil) {
			work = append(work, calWork{p, "return", live, p.ReturnEventID, k, t, d, date})
		}
	}
	if len(work) == 0 {
		return n, nil
	}
	conn, err := s.clients(ctx, uid).conn(ctx, appCalendar, scopeCalendarWrite)
	if err != nil {
		return n, err
	}
	for _, wk := range work {
		if !wk.live {
			if err := conn.client.DeleteEvent(ctx, *wk.id); err != nil {
				n["failed"]++
				continue
			}
			_ = s.store.SetCalendar(ctx, uid, wk.p.ID, wk.which, nil, "")
			n["removed"]++
			continue
		}
		ev := pocket.Event{CalendarID: st.CalendarID, Title: wk.title, Description: wk.desc, URL: s.parcelURL(wk.p.ID), Date: wk.date}
		if wk.id != nil {
			err := conn.client.UpdateEvent(ctx, *wk.id, ev)
			if err == nil {
				_ = s.store.SetCalendar(ctx, uid, wk.p.ID, wk.which, wk.id, wk.key)
				n["moved"]++
				continue
			}
			if !errors.Is(err, pocket.ErrNotFound) {
				n["failed"]++
				continue
			}
			// deleted in Calendar: make a new one
		}
		id, err := conn.client.CreateEvent(ctx, ev)
		if err != nil || id == 0 {
			n["failed"]++
			continue
		}
		_ = s.store.SetCalendar(ctx, uid, wk.p.ID, wk.which, &id, wk.key)
		n["created"]++
	}
	if n["failed"] > 0 {
		return n, fmt.Errorf("%d calendar changes failed", n["failed"])
	}
	return n, nil
}

// dropCalendarEvents removes the events of parcels that are gone (best effort).
func (s *Server) dropCalendarEvents(uid int64, ps []store.Parcel) {
	var ids []int64
	for _, p := range ps {
		for _, id := range []*int64{p.CalEventID, p.ReturnEventID} {
			if id != nil {
				ids = append(ids, *id)
			}
		}
	}
	if len(ids) == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		conn, err := s.clients(ctx, uid).conn(ctx, appCalendar, scopeCalendarWrite)
		if err != nil {
			return
		}
		for _, id := range ids {
			if err := conn.client.DeleteEvent(ctx, id); err != nil {
				s.log.Debug("calendar: removing an event failed", "event", id, "err", err)
			}
		}
	}()
}

// handleCalendarSync runs the sync now (Settings → Connected apps).
func (s *Server) handleCalendarSync(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	n, err := s.syncCalendar(ctx, actorID(ctx))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "counts": n})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "counts": n})
}

// ---------- pickers: what the other apps have ----------

// pick answers a picker: the items, or why the app is not reachable.
func pick(w http.ResponseWriter, items any, err error, app pocketApp) {
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"connected": !isNotConnected(err), "detail": app.name + ": " + strings.TrimPrefix(err.Error(), app.name+" "), "items": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connected": true, "items": items})
}

func (s *Server) handlePickMoneyAccounts(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	conn, err := s.clients(ctx, actorID(ctx)).conn(ctx, appMoney, scopeAccountsRead)
	if err != nil {
		pick(w, nil, err, appMoney)
		return
	}
	accts, err := conn.client.Accounts(ctx)
	pick(w, accts, err, appMoney)
}

func (s *Server) handlePickMoneyCategories(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	conn, err := s.clients(ctx, actorID(ctx)).conn(ctx, appMoney, scopeAccountsRead)
	if err != nil {
		pick(w, nil, err, appMoney)
		return
	}
	cats, err := conn.client.Categories(ctx, int64(queryInt(r, "account_id")))
	pick(w, cats, err, appMoney)
}

func (s *Server) handlePickCalendars(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	conn, err := s.clients(ctx, actorID(ctx)).conn(ctx, appCalendar, scopeCalendarRead)
	if err != nil {
		pick(w, nil, err, appCalendar)
		return
	}
	cals, err := conn.client.Calendars(ctx)
	pick(w, cals, err, appCalendar)
}

func (s *Server) handlePickContacts(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"connected": true, "items": []any{}})
		return
	}
	conn, err := s.clients(ctx, actorID(ctx)).conn(ctx, appContacts, scopeContactsRead)
	if err != nil {
		pick(w, nil, err, appContacts)
		return
	}
	cs, err := conn.client.SearchContacts(ctx, q, 12)
	pick(w, cs, err, appContacts)
}

func (s *Server) handlePickDocuments(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	conn, err := s.clients(ctx, actorID(ctx)).conn(ctx, appDocuments, scopeDocumentsRead)
	if err != nil {
		pick(w, nil, err, appDocuments)
		return
	}
	docs, err := conn.client.SearchDocuments(ctx, strings.TrimSpace(r.URL.Query().Get("q")), 15)
	pick(w, docs, err, appDocuments)
}

// handlePickTransactions finds the payment for an order: expenses near the
// order day (q matches shop or description).
func (s *Server) handlePickTransactions(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	q := r.URL.Query()
	conn, err := s.clients(ctx, actorID(ctx)).conn(ctx, appMoney, scopeTxRead)
	if err != nil {
		pick(w, nil, err, appMoney)
		return
	}
	from, to := q.Get("from"), q.Get("to")
	if day := q.Get("near"); validDate(day) && day != "" {
		from, to = store.AddDays(day, -10), store.AddDays(day, 10)
	}
	txs, err := conn.client.Transactions(ctx, strings.TrimSpace(q.Get("q")), from, to, 25)
	pick(w, txs, err, appMoney)
}

func (s *Server) handlePickWishes(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	conn, err := s.clients(ctx, actorID(ctx)).conn(ctx, appWishlist, scopeWishesRead)
	if err != nil {
		pick(w, nil, err, appWishlist)
		return
	}
	ws, err := conn.client.Wishes(ctx, strings.TrimSpace(r.URL.Query().Get("q")), 50)
	if err == nil && r.URL.Query().Get("all") == "" {
		open := []pocket.Wish{}
		for _, x := range ws {
			if x.Status != "bought" && x.Status != "received" && x.Status != "archived" {
				open = append(open, x)
			}
		}
		ws = open
	}
	pick(w, ws, err, appWishlist)
}

// ---------- doing things in the other apps for one parcel ----------

// handleParcelTask adds "Pick up …" or "Send back …" to Pocket Tasks.
func (s *Server) handleParcelTask(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var in struct {
		Kind string `json:"kind"`
	}
	_ = decode(r, &in)
	if in.Kind == "" {
		in.Kind = "pickup"
	}
	uid := actorID(ctx)
	t, err := s.makeTask(ctx, uid, idParam(r, "id"), in.Kind)
	if err != nil {
		s.linkFail(w, err, "Pocket Tasks", "this parcel already has that task")
		return
	}
	p, _ := s.store.GetParcel(ctx, uid, idParam(r, "id"))
	writeJSON(w, http.StatusOK, map[string]any{"task": t, "parcel": s.detail(ctx, uid, p)})
}

// handleParcelInventory puts what arrived on the shelves in Pocket Inventory.
func (s *Server) handleParcelInventory(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var in struct {
		Location string `json:"location"`
	}
	_ = decode(r, &in)
	uid := actorID(ctx)
	it, err := s.toInventory(ctx, uid, idParam(r, "id"), strings.TrimSpace(in.Location))
	if err != nil {
		s.linkFail(w, err, "Pocket Inventory", "this parcel is already on your shelves")
		return
	}
	p, _ := s.store.GetParcel(ctx, uid, idParam(r, "id"))
	writeJSON(w, http.StatusOK, map[string]any{"item": it, "parcel": s.detail(ctx, uid, p)})
}

// handleParcelWish links a parcel to a wish (and marks it bought when the
// person wants that).
func (s *Server) handleParcelWish(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	uid := actorID(ctx)
	p, err := s.store.GetParcel(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "parcel")
		return
	}
	var in struct {
		WishID     int64 `json:"wish_id"`
		MarkBought *bool `json:"mark_bought"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.WishID <= 0 {
		p.WishID, p.WishTitle = nil, ""
		_ = s.store.UpdateParcel(ctx, uid, p)
		writeJSON(w, http.StatusOK, s.detail(ctx, uid, p))
		return
	}
	conn, err := s.clients(ctx, uid).conn(ctx, appWishlist, scopeWishesRead)
	if err != nil {
		s.linkFail(w, err, "Pocket Wishlist", "")
		return
	}
	wish, err := conn.client.GetWish(ctx, in.WishID)
	if err != nil {
		s.linkFail(w, err, "Pocket Wishlist", "")
		return
	}
	p.WishID, p.WishTitle = &wish.ID, wish.Title
	if p.Shop == "" {
		p.Shop = wish.Store
	}
	if err := s.store.UpdateParcel(ctx, uid, p); err != nil {
		s.fail(w, err, "parcel")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	mark := st.WishlistBought
	if in.MarkBought != nil {
		mark = *in.MarkBought
	}
	out := map[string]any{}
	if mark && p.Direction == "in" {
		if err := s.markWishBought(ctx, uid, p.ID); err != nil {
			out["warning"] = "Linked, but Pocket Wishlist did not mark it bought: " + err.Error()
		}
	}
	p, _ = s.store.GetParcel(ctx, uid, p.ID)
	out["parcel"] = s.detail(ctx, uid, p)
	writeJSON(w, http.StatusOK, out)
}

// handleParcelMoney links the payment for an order ({tx_id}) or books the
// extra cost (customs, postage) as an expense ({book_extra: true}).
func (s *Server) handleParcelMoney(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	uid := actorID(ctx)
	p, err := s.store.GetParcel(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "parcel")
		return
	}
	var in struct {
		TxID       *int64 `json:"tx_id"`
		BookExtra  bool   `json:"book_extra"`
		AccountID  int64  `json:"account_id"`
		CategoryID int64  `json:"category_id"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	cl := s.clients(ctx, uid)
	switch {
	case in.TxID != nil && *in.TxID <= 0:
		p.MoneyTxID, p.MoneyTxLabel = nil, ""
	case in.TxID != nil:
		conn, err := cl.conn(ctx, appMoney, scopeTxRead)
		if err != nil {
			s.linkFail(w, err, "Pocket Money", "")
			return
		}
		tx, err := conn.client.Transaction(ctx, *in.TxID)
		if err != nil {
			s.linkFail(w, err, "Pocket Money", "")
			return
		}
		p.MoneyTxID = &tx.ID
		p.MoneyTxLabel = strings.TrimSpace(fmt.Sprintf("%s · %s", tx.Date, firstNonEmpty(tx.Payee, tx.Description)))
		if p.Price == nil && tx.Amount != 0 {
			amt := tx.Amount
			if amt < 0 {
				amt = -amt
			}
			p.Price = &amt
			if tx.Currency != "" {
				p.Currency = tx.Currency
			}
		}
	case in.BookExtra:
		if p.ExtraCost == nil || *p.ExtraCost <= 0 {
			writeErr(w, http.StatusBadRequest, "enter the extra cost first")
			return
		}
		if p.ExtraTxID != nil {
			writeErr(w, http.StatusConflict, "the extra cost is already in Pocket Money")
			return
		}
		acct := firstID(in.AccountID, st.MoneyAccountID)
		if acct == 0 {
			writeErr(w, http.StatusBadRequest, "choose the account in Settings → Connected apps → Pocket Money")
			return
		}
		conn, err := cl.conn(ctx, appMoney, scopeTxWrite)
		if err != nil {
			s.linkFail(w, err, "Pocket Money", "")
			return
		}
		s.moneyMu.Lock()
		id, err := conn.client.CreateTx(ctx, pocket.Tx{AccountID: acct, Date: firstNonEmpty(localDate(p.DeliveredAt), store.Today()), Amount: *p.ExtraCost, Currency: p.Currency,
			Description: "Customs and postage: " + p.Title, Payee: firstNonEmpty(track.Name(p.Carrier), p.Shop), CategoryID: firstID(in.CategoryID, st.MoneyCategoryID),
			Notes: s.parcelURL(p.ID), ExternalID: fmt.Sprintf("parcels:extra:%d", p.ID)})
		s.moneyMu.Unlock()
		if err != nil {
			s.linkFail(w, err, "Pocket Money", "")
			return
		}
		p.ExtraTxID = &id
	default:
		writeErr(w, http.StatusBadRequest, "send tx_id or book_extra")
		return
	}
	if err := s.store.UpdateParcel(ctx, uid, p); err != nil {
		s.fail(w, err, "parcel")
		return
	}
	p, _ = s.store.GetParcel(ctx, uid, p.ID)
	writeJSON(w, http.StatusOK, s.detail(ctx, uid, p))
}

func firstID(v ...int64) int64 {
	for _, x := range v {
		if x > 0 {
			return x
		}
	}
	return 0
}

// linkFail answers a failed call into another app in plain words.
func (s *Server) linkFail(w http.ResponseWriter, err error, app, already string) {
	switch {
	case errors.Is(err, store.ErrConflict) && already != "":
		writeErr(w, http.StatusConflict, already)
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "parcel not found")
	case isNotConnected(err):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, pocket.ErrNotFound):
		writeErr(w, http.StatusBadGateway, app+": not found there")
	default:
		writeErr(w, http.StatusBadGateway, app+": "+err.Error())
	}
}
