package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-parcels/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-parcels/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-parcels/internal/store"
	"github.com/abjelosevic88/pocket-homelab-parcels/internal/track"
)

// ---- following parcels ----
//
// A parcel with a tracking number is followed by a tracking service when
// the person has one that can: 17TRACK with their own key (every carrier),
// else Cainiao's free tracking for AliExpress parcels. Without either, news
// comes from shipping emails (Pocket Mail) and from the person. Each parcel
// has its own next check, sooner when it is close (out for delivery: half
// an hour) and later when nothing happens.

type cainiaoAPI interface {
	Get(ctx context.Context, numbers []string) (map[string]*track.Result, error)
}

type seventeenAPI interface {
	Register(ctx context.Context, number, carrierRef string) (string, error)
	Get(ctx context.Context, numbers map[string]string) (map[string]*track.Result, error)
	Stop(ctx context.Context, numbers ...string) error
	Quota(ctx context.Context) (*track.Quota, error)
}

// providerFor is the service that follows this parcel for this person, ""
// when none can.
func (s *Server) providerFor(st store.Settings, p store.Parcel) string {
	if p.TrackingNumber == "" || p.Archived || track.Final(p.Status) {
		return ""
	}
	if strings.TrimSpace(st.Track17Key) != "" {
		return "17track"
	}
	if st.TrackCainiao && track.CainiaoCovers(p.Carrier, p.TrackingNumber) {
		return "cainiao"
	}
	return ""
}

// nextCheck is when to ask again, by how close the parcel is.
func nextCheck(p store.Parcel, now time.Time) time.Time {
	switch p.Status {
	case track.StatusOutForDelivery:
		return now.Add(30 * time.Minute)
	case track.StatusInTransit, track.StatusException:
		return now.Add(2 * time.Hour)
	case track.StatusPickup:
		return now.Add(4 * time.Hour)
	}
	// pending / label created: often for days; a parcel nobody has heard of
	// for a month is asked about once a day
	created, _ := time.Parse(time.RFC3339Nano, p.CreatedAt)
	if !created.IsZero() && now.Sub(created) > 30*24*time.Hour {
		return now.Add(24 * time.Hour)
	}
	return now.Add(3 * time.Hour)
}

// trackDue checks the parcels whose turn it is (at most 40 a minute).
func (s *Server) trackDue(ctx context.Context) {
	if !s.trackMu.TryLock() {
		return
	}
	defer s.trackMu.Unlock()
	due, err := s.store.DueChecks(ctx, time.Now().UTC().Format(time.RFC3339), 40)
	if err != nil {
		s.log.Warn("tracking: listing due checks failed", "err", err)
		return
	}
	for _, p := range due {
		if ctx.Err() != nil {
			return
		}
		cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		if _, err := s.checkParcel(cctx, p.UserID, &p); err != nil {
			s.log.Debug("tracking: check failed", "parcel", p.ID, "err", err)
		}
		cancel()
	}
}

// checkSoon checks one parcel in the background (just added, number changed).
func (s *Server) checkSoon(uid, id int64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		p, err := s.store.GetParcel(ctx, uid, id)
		if err != nil {
			return
		}
		if _, err := s.checkParcel(ctx, uid, p); err != nil {
			s.log.Debug("tracking: first check failed", "parcel", id, "err", err)
		}
	}()
}

// checkParcel asks the parcel's tracking service and takes in what it
// says. It reports whether anything changed.
func (s *Server) checkParcel(ctx context.Context, uid int64, p *store.Parcel) (bool, error) {
	st, err := s.store.GetSettings(ctx, uid)
	if err != nil {
		return false, err
	}
	provider := s.providerFor(st, *p)
	now := time.Now()
	if provider == "" {
		if p.Provider != "" {
			p.Provider = ""
			_ = s.store.UpdateParcel(ctx, uid, p)
		}
		if p.TrackingNumber == "" {
			return false, errors.New("this parcel has no tracking number")
		}
		return false, errors.New("nothing follows this parcel automatically: add a 17TRACK key in Settings → Tracking, or let Pocket Mail bring the news")
	}
	var res *track.Result
	ref := p.ProviderRef
	switch provider {
	case "17track":
		c := s.new17(st.Track17Key)
		if p.Provider != "17track" || ref == "" {
			ref, err = c.Register(ctx, p.TrackingNumber, "")
			if err != nil {
				wait := time.Hour
				var qe *track.QuotaError
				if errors.As(err, &qe) {
					wait = 24 * time.Hour
				}
				_ = s.store.SetChecked(ctx, uid, p.ID, now.Add(wait).UTC().Format(time.RFC3339), err.Error(), "")
				return false, err
			}
			if ref == "" {
				ref = "0"
			}
		}
		var got map[string]*track.Result
		carrier := ref
		if carrier == "0" {
			carrier = ""
		}
		got, err = c.Get(ctx, map[string]string{p.TrackingNumber: carrier})
		if err == nil {
			res = got[p.TrackingNumber]
			if res != nil && res.CarrierRef != "" && res.CarrierRef != "0" {
				ref = res.CarrierRef
			}
		}
	case "cainiao":
		var got map[string]*track.Result
		got, err = s.cainiao.Get(ctx, []string{p.TrackingNumber})
		if err == nil {
			res = got[p.TrackingNumber]
		}
	}
	if err != nil {
		_ = s.store.SetChecked(ctx, uid, p.ID, now.Add(time.Hour).UTC().Format(time.RFC3339), err.Error(), ref)
		return false, err
	}
	p.Provider = provider
	changed, err := s.applyResult(ctx, uid, p, provider, res)
	if err != nil {
		return changed, err
	}
	fresh, err := s.store.GetParcel(ctx, uid, p.ID)
	if err == nil {
		*p = *fresh
	}
	next := ""
	if !track.Final(p.Status) {
		next = nextCheck(*p, now).UTC().Format(time.RFC3339)
	}
	_ = s.store.SetChecked(ctx, uid, p.ID, next, "", ref)
	if track.Final(p.Status) && provider == "17track" {
		s.stopTrackingSoon(uid, *p)
	}
	return changed, nil
}

// applyResult saves new steps and, when the service knows something newer
// than the parcel shows, the new status. Expected days from the service
// replace only expected days the person did not type.
func (s *Server) applyResult(ctx context.Context, uid int64, p *store.Parcel, source string, res *track.Result) (bool, error) {
	if res == nil {
		return false, nil
	}
	before := *p
	fresh, err := s.addEvents(ctx, uid, p, source, res.Events)
	if err != nil {
		return false, err
	}
	changed := false
	if res.Found {
		latest := track.Event{At: res.At, Status: res.Status, Description: res.Text, Location: res.Location}
		if latest.At == "" && len(res.Events) > 0 {
			latest.At = res.Events[len(res.Events)-1].At
		}
		if s.takeLatest(p, latest) {
			changed = true
		}
		// a service sometimes reports a final status on an older step
		if res.Status != p.Status && track.Final(res.Status) && !track.Final(p.Status) {
			p.Status = res.Status
			if res.Status == track.StatusDelivered && p.DeliveredAt == "" {
				p.DeliveredAt = firstNonEmpty(latest.At, time.Now().UTC().Format(time.RFC3339))
			}
			changed = true
		}
	}
	if !p.ExpectedManual && res.ExpectedFrom != "" && (res.ExpectedFrom != p.ExpectedOn || res.ExpectedUntil != p.ExpectedUntil) && !track.Final(p.Status) {
		p.ExpectedOn, p.ExpectedUntil = res.ExpectedFrom, res.ExpectedUntil
		changed = true
	}
	if changed || p.Provider != before.Provider {
		if err := s.store.UpdateParcel(ctx, uid, p); err != nil {
			return false, err
		}
	}
	if p.Status != before.Status {
		s.afterStatus(ctx, uid, before, p, source)
	} else if len(fresh) > 0 {
		s.moved(ctx, uid, p, fresh)
	}
	return changed || len(fresh) > 0, nil
}

// addEvents keeps the steps not seen before and returns them.
func (s *Server) addEvents(ctx context.Context, uid int64, p *store.Parcel, source string, evs []track.Event) ([]track.Event, error) {
	var fresh []track.Event
	for _, e := range evs {
		if e.At == "" || strings.TrimSpace(e.Description) == "" {
			continue
		}
		se := store.Event{ParcelID: p.ID, At: e.At, Status: firstNonEmpty(e.Status, track.StatusInTransit), Description: strings.TrimSpace(e.Description),
			Location: strings.TrimSpace(e.Location), Source: source, Fingerprint: e.Fingerprint(source)}
		ok, err := s.store.AddEvent(ctx, uid, &se)
		if err != nil {
			return fresh, err
		}
		if ok {
			fresh = append(fresh, e)
		}
	}
	return fresh, nil
}

// takeLatest makes a step the parcel's latest when it is newer than what
// the parcel shows. It reports whether anything changed.
func (s *Server) takeLatest(p *store.Parcel, e track.Event) bool {
	if e.Status == "" {
		return false
	}
	at := track.ParseTime(e.At)
	cur := track.ParseTime(p.StatusAt)
	if !cur.IsZero() && !at.IsZero() && at.Before(cur) {
		return false
	}
	if p.Status == e.Status && p.StatusText == e.Description && p.StatusAt == e.At && p.Location == e.Location {
		return false
	}
	p.Status, p.StatusText, p.StatusAt, p.Location = e.Status, e.Description, e.At, e.Location
	if e.Status == track.StatusDelivered && p.DeliveredAt == "" {
		p.DeliveredAt = firstNonEmpty(e.At, time.Now().UTC().Format(time.RFC3339))
	}
	if e.Status != track.StatusDelivered {
		p.DeliveredAt = ""
	}
	return true
}

// statusTopics are the events each status publishes besides status_changed.
var statusTopics = map[string]string{
	track.StatusOutForDelivery: "parcels.out_for_delivery",
	track.StatusPickup:         "parcels.ready_for_pickup",
	track.StatusDelivered:      "parcels.delivered",
	track.StatusException:      "parcels.problem",
	track.StatusReturned:       "parcels.problem",
	track.StatusExpired:        "parcels.problem",
}

func (s *Server) eventPayload(p store.Parcel) map[string]any {
	return map[string]any{"parcel_id": p.ID, "title": p.Title, "tracking_number": p.TrackingNumber, "carrier": p.Carrier, "carrier_name": track.Name(p.Carrier),
		"shop": p.Shop, "direction": p.Direction, "status": p.Status, "status_label": track.StatusLabel(p.Status), "latest": p.StatusText, "location": p.Location,
		"expected_on": p.ExpectedOn, "url": s.parcelURL(p.ID)}
}

// afterStatus does what a new status calls for: events on the Core bus, the
// push, a pick-up task, the delivered parcel on the shelves.
func (s *Server) afterStatus(ctx context.Context, uid int64, before store.Parcel, p *store.Parcel, source string) {
	payload := s.eventPayload(*p)
	payload["previous_status"] = before.Status
	payload["source"] = source
	s.publish(ctx, uid, "parcels.status_changed", parcelSubject(p.ID), payload)
	if t, ok := statusTopics[p.Status]; ok {
		s.publish(ctx, uid, t, parcelSubject(p.ID), s.eventPayload(*p))
	}
	st, _ := s.store.GetSettings(ctx, uid)
	if source != "manual" {
		s.pushStatus(ctx, uid, st, p)
	}
	if p.Status == track.StatusPickup && st.TasksPickup && p.TaskID == nil && p.Direction == "in" {
		s.pickupTaskSoon(uid, p.ID)
	}
	if p.Status == track.StatusDelivered && st.InventoryAuto && p.InventoryItemID == nil && p.Direction == "in" {
		s.inventorySoon(uid, p.ID)
	}
	if track.Final(p.Status) || p.ExpectedOn != before.ExpectedOn {
		s.syncCalendarSoon(uid)
	}
}

// pushWanted says whether the person wants a push for this status.
func pushWanted(st store.Settings, status string) bool {
	switch status {
	case track.StatusOutForDelivery:
		return st.NotifyOut
	case track.StatusPickup:
		return st.NotifyPickup
	case track.StatusDelivered:
		return st.NotifyDelivered
	case track.StatusException, track.StatusReturned, track.StatusExpired:
		return st.NotifyProblems
	case track.StatusInTransit, track.StatusInfoReceived:
		return st.NotifyMoves
	}
	return false
}

// pushStatus sends one push for a new status (once per status).
func (s *Server) pushStatus(ctx context.Context, uid int64, st store.Settings, p *store.Parcel) {
	if st.NotifyURL == "" || p.NotifiedStatus == p.Status || !pushWanted(st, p.Status) {
		return
	}
	_ = s.store.SetNotified(ctx, uid, p.ID, p.Status, "")
	p.NotifiedStatus = p.Status
	title := p.Title + ": " + strings.ToLower(track.StatusLabel(p.Status))
	body := firstNonEmpty(p.StatusText, track.StatusLabel(p.Status))
	if p.Location != "" && !strings.Contains(body, p.Location) {
		body += " — " + p.Location
	}
	prio := "info"
	switch p.Status {
	case track.StatusDelivered:
		prio = "success"
	case track.StatusPickup, track.StatusOutForDelivery:
		prio = "info"
		if p.Status == track.StatusPickup {
			body += "\nIt is waiting for you to pick it up."
		}
	case track.StatusException, track.StatusReturned, track.StatusExpired:
		prio = "warning"
	}
	n := notify.New(st.NotifyURL, st.NotifyFormat)
	if err := n.Send(ctx, notify.Message{Title: title, Body: body, Priority: prio, Tags: []string{"package"}, Link: s.parcelURL(p.ID)}); err != nil {
		s.log.Warn("notify", "user", uid, "err", err)
	}
}

// moved sends a push for new steps that did not change the status (only
// for people who want every step).
func (s *Server) moved(ctx context.Context, uid int64, p *store.Parcel, fresh []track.Event) {
	st, _ := s.store.GetSettings(ctx, uid)
	if !st.NotifyMoves || st.NotifyURL == "" || len(fresh) == 0 {
		return
	}
	last := fresh[len(fresh)-1]
	body := last.Description
	if last.Location != "" {
		body += " — " + last.Location
	}
	n := notify.New(st.NotifyURL, st.NotifyFormat)
	_ = n.Send(ctx, notify.Message{Title: p.Title + ": " + strings.ToLower(track.StatusLabel(p.Status)), Body: body, Priority: "info", Tags: []string{"package"}, Link: s.parcelURL(p.ID)})
}

// stopTrackingSoon tells 17TRACK to stop following a parcel that is done
// or deleted (best effort; the person's quota is not refunded either way).
func (s *Server) stopTrackingSoon(uid int64, p store.Parcel) {
	if p.TrackingNumber == "" || p.ProviderRef == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		st, err := s.store.GetSettings(ctx, uid)
		if err != nil || st.Track17Key == "" {
			return
		}
		if err := s.new17(st.Track17Key).Stop(ctx, p.TrackingNumber); err != nil {
			s.log.Debug("17track: stop failed", "parcel", p.ID, "err", err)
		}
	}()
}

// refreshProviders sets who follows each active parcel after the person
// changed their tracking settings, and checks the newly followed ones soon.
func (s *Server) refreshProviders(ctx context.Context, uid int64) {
	st, err := s.store.GetSettings(ctx, uid)
	if err != nil {
		return
	}
	ps, _, err := s.store.ListParcels(ctx, uid, store.ParcelFilter{View: "active"})
	if err != nil {
		return
	}
	for _, p := range ps {
		pv := s.providerFor(st, p)
		if pv == p.Provider {
			continue
		}
		p.Provider, p.NextCheckAt = pv, ""
		if pv != "17track" {
			p.ProviderRef = ""
		}
		_ = s.store.UpdateParcel(ctx, uid, &p)
	}
}

// archiveAll moves everybody's long-delivered parcels out of the list.
func (s *Server) archiveAll(ctx context.Context) {
	ids, err := s.store.SettingsUsers(ctx)
	if err != nil {
		return
	}
	for _, uid := range ids {
		st, err := s.store.GetSettings(ctx, uid)
		if err != nil {
			continue
		}
		if n, err := s.store.ArchiveDelivered(ctx, uid, st.AutoArchiveDays); err == nil && n > 0 {
			s.log.Debug("archived delivered parcels", "user", uid, "count", n)
		}
	}
}

// ---------------- tasks ----------------

// pickupTaskSoon adds "Pick up …" to the person's tasks (Pocket Tasks).
func (s *Server) pickupTaskSoon(uid, id int64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if _, err := s.makeTask(ctx, uid, id, "pickup"); err != nil && !isNotConnected(err) {
			s.log.Debug("tasks: pick-up task failed", "parcel", id, "err", err)
		}
	}()
}

// returnTaskSoon adds "Send back …" to the person's tasks.
func (s *Server) returnTaskSoon(uid, id int64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if _, err := s.makeTask(ctx, uid, id, "return"); err != nil && !isNotConnected(err) {
			s.log.Debug("tasks: return task failed", "parcel", id, "err", err)
		}
	}()
}

// makeTask adds a pick-up or send-back task for a parcel (once each).
func (s *Server) makeTask(ctx context.Context, uid, id int64, kind string) (*pocket.Task, error) {
	p, err := s.store.GetParcel(ctx, uid, id)
	if err != nil {
		return nil, err
	}
	if kind == "pickup" && p.TaskID != nil || kind == "return" && p.ReturnTaskID != nil {
		return nil, store.ErrConflict
	}
	conn, err := s.clients(ctx, uid).conn(ctx, appTasks, scopeTasksWrite)
	if err != nil {
		return nil, err
	}
	var title, notes, due string
	switch kind {
	case "pickup":
		title = "Pick up " + p.Title
		where := firstNonEmpty(p.Location, track.Name(p.Carrier))
		if where != "" {
			title += " (" + where + ")"
		}
		notes = firstNonEmpty(p.StatusText, "Waiting for you at a pick-up point.")
		if p.TrackingNumber != "" {
			notes += "\nTracking number: " + p.TrackingNumber
		}
		due = store.Today()
	case "return":
		title = "Send back " + p.Title
		if p.Shop != "" {
			title += " to " + p.Shop
		}
		notes = "Last day to send it back: " + firstNonEmpty(p.ReturnBy, "not set")
		if p.OrderNumber != "" {
			notes += "\nOrder " + p.OrderNumber
		}
		due = p.ReturnBy
		if due != "" && due > store.Today() {
			due = store.AddDays(due, -1)
		}
	default:
		return nil, fmt.Errorf("unknown task %q", kind)
	}
	t, err := conn.client.CreateTask(ctx, title, notes, due, s.parcelURL(p.ID))
	if err != nil {
		return nil, err
	}
	col := "task_id"
	if kind == "return" {
		col = "return_task_id"
	}
	if t.ID > 0 {
		_ = s.store.SetLink(ctx, uid, p.ID, col, &t.ID)
	}
	return t, nil
}

// ---------------- inventory ----------------

func (s *Server) inventorySoon(uid, id int64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if _, err := s.toInventory(ctx, uid, id, ""); err != nil && !isNotConnected(err) {
			s.log.Debug("inventory: adding a delivered parcel failed", "parcel", id, "err", err)
		}
	}()
}

// toInventory puts a delivered parcel's contents on the shelves (once).
func (s *Server) toInventory(ctx context.Context, uid, id int64, location string) (*pocket.Item, error) {
	p, err := s.store.GetParcel(ctx, uid, id)
	if err != nil {
		return nil, err
	}
	if p.InventoryItemID != nil {
		return nil, store.ErrConflict
	}
	st, _ := s.store.GetSettings(ctx, uid)
	conn, err := s.clients(ctx, uid).conn(ctx, appInventory, scopeItemsWrite)
	if err != nil {
		return nil, err
	}
	in := pocket.ItemInput{Name: p.Title, Location: firstNonEmpty(location, st.InventoryLocation), URL: s.parcelURL(p.ID),
		PurchaseDate: firstNonEmpty(p.OrderedOn, localDate(p.DeliveredAt)), Notes: "Arrived " + firstNonEmpty(localDate(p.DeliveredAt), store.Today())}
	if p.Shop != "" {
		in.Notes += " from " + p.Shop
	}
	if p.Price != nil {
		v := float64(*p.Price) / 100
		in.Price = &v
	}
	it, err := conn.client.CreateItem(ctx, in)
	if err != nil {
		return nil, err
	}
	if it.ID > 0 {
		_ = s.store.SetLink(ctx, uid, p.ID, "inventory_item_id", &it.ID)
	}
	return it, nil
}

// ---------------- wishlist ----------------

func (s *Server) markWishBoughtSoon(uid, id int64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := s.markWishBought(ctx, uid, id); err != nil && !isNotConnected(err) {
			s.log.Debug("wishlist: marking the wish bought failed", "parcel", id, "err", err)
		}
	}()
}

// markWishBought tells Pocket Wishlist the parcel's wish was ordered. A
// wish already bought is left as it is.
func (s *Server) markWishBought(ctx context.Context, uid, id int64) error {
	p, err := s.store.GetParcel(ctx, uid, id)
	if err != nil {
		return err
	}
	if p.WishID == nil {
		return nil
	}
	conn, err := s.clients(ctx, uid).conn(ctx, appWishlist, scopeWishesRead, scopeWishesWrite)
	if err != nil {
		return err
	}
	w, err := conn.client.GetWish(ctx, *p.WishID)
	if err != nil {
		return err
	}
	if p.WishTitle != w.Title {
		p.WishTitle = w.Title
		_ = s.store.UpdateParcel(ctx, uid, p)
	}
	if w.Status == "bought" || w.BoughtAt != "" {
		return nil
	}
	st, _ := s.store.GetSettings(ctx, uid)
	in := pocket.WishBought{Date: firstNonEmpty(p.OrderedOn, localDate(p.CreatedAt)), Price: p.Price, Store: p.Shop,
		Notes: "Ordered — follow it in Pocket Parcels: " + s.parcelURL(p.ID)}
	if p.Price != nil {
		in.Currency = p.Currency
	}
	if st.InventoryAuto {
		// Parcels puts it on the shelves when it arrives
		no := false
		in.Inventory = &no
	}
	return conn.client.MarkWishBought(ctx, *p.WishID, in)
}
