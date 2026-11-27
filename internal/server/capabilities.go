package server

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-parcels/internal/store"
	"github.com/abjelosevic88/pocket-homelab-parcels/internal/track"
)

// What Pocket Search, Pocket Day and Pocket Hub ask Parcels (the
// capabilities value in links.go): one route each, in the shape every Pocket
// app answers in. They read what /search, /day and /overview read, as the
// request's person, from what Parcels keeps: none of them asks a carrier or
// a tracking service. Only the capture adds something, one parcel to follow.

// parcelPath opens a parcel in the app.
func parcelPath(id int64) string { return fmt.Sprintf("/parcels/%d", id) }

// parcelKind is the parcel's kind as the list words it: coming to you or sent by you.
func parcelKind(p store.Parcel) string {
	if p.Direction == "out" {
		return "sent"
	}
	return "delivery"
}

// statusWords is the status in plain words, "No news for a while" for a
// parcel that went quiet (as the parcel's page says).
func statusWords(v parcelView) string {
	if v.Stale && v.Active() {
		return "No news for a while"
	}
	return v.StatusLabel
}

// dots joins the parts of a line that are there: "On the way · DHL Paket".
func dots(parts ...string) string {
	out := parts[:0:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

// shortDay is a day in a line: "25 Nov", with the year when it is not this one.
func shortDay(d, now time.Time) string {
	if d.Year() != now.Year() {
		return d.Format("2 Jan 2006")
	}
	return d.Format("2 Jan")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// hasWord reports whether a query has a letter or a digit to look for.
func hasWord(q string) bool {
	return strings.IndexFunc(q, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) >= 0
}

// ---------- search ----------

// pocketSearch answers Pocket Search: the parcels /search finds (name,
// tracking number, shop, order, notes, who it goes to, the latest news) and
// those of a carrier whose name has the words. A name that has the words
// comes first, then a number, shop or carrier, then the rest; newest news
// first among equals.
func (s *Server) pocketSearch(r *http.Request, q string, limit int) ([]core.SearchItem, error) {
	if !hasWord(q) {
		return nil, nil
	}
	ctx := r.Context()
	uid := actorID(ctx)
	found, _, err := s.store.ListParcels(ctx, uid, store.ParcelFilter{View: "all", Q: q, Limit: limit})
	if err != nil {
		return nil, err
	}
	lq := strings.ToLower(q)
	seen := map[int64]bool{}
	for _, p := range found {
		seen[p.ID] = true
	}
	// carriers by name ("dhl", "pošta"): parcels keep them by key
	keys, err := s.store.Carriers(ctx, uid)
	if err != nil {
		return nil, err
	}
	for _, key := range keys {
		if !strings.Contains(strings.ToLower(firstNonEmpty(track.Name(key), key)), lq) {
			continue
		}
		more, _, err := s.store.ListParcels(ctx, uid, store.ParcelFilter{View: "all", Carrier: key, Limit: limit})
		if err != nil {
			return nil, err
		}
		for _, p := range more {
			if !seen[p.ID] {
				seen[p.ID] = true
				found = append(found, p)
			}
		}
	}
	st, _ := s.store.GetSettings(ctx, uid)
	now := time.Now()
	has := func(v string) bool { return v != "" && strings.Contains(strings.ToLower(v), lq) }
	number := strings.ToLower(strings.ReplaceAll(q, " ", ""))
	out := make([]core.SearchItem, 0, len(found))
	for _, p := range found {
		v := s.view(p, st, now)
		score := 1.0
		switch {
		case has(p.Title):
			score = 3
		case has(p.TrackingNumber) || strings.Contains(strings.ToLower(p.TrackingNumber), number) || has(p.Shop) || has(v.CarrierName):
			score = 2
		}
		out = append(out, core.SearchItem{ID: itoa(p.ID), Title: p.Title, Snippet: parcelSnippet(v, now), Kind: parcelKind(p),
			Date: firstNonEmpty(p.StatusAt, p.UpdatedAt), URL: parcelPath(p.ID), Score: score})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// parcelSnippet: the status in plain words, the carrier, and the last step
// with its day: "On the way · DHL Paket · Arrived at the sorting centre, 25 Nov".
func parcelSnippet(v parcelView, now time.Time) string {
	last := ""
	if text := strings.TrimSpace(v.StatusText); text != "" && text != v.StatusLabel {
		last = text
		if at := track.ParseTime(v.StatusAt); !at.IsZero() {
			last += ", " + shortDay(at.In(time.Local), now)
		}
	}
	return dots(statusWords(v), v.CarrierName, last)
}

// ---------- day ----------

// expectedOnDay reports whether a parcel still on its way was expected on
// that day: the day itself, or a day inside its expected range.
func expectedOnDay(p store.Parcel, date string) bool {
	return p.Active() && p.ExpectedOn != "" && p.ExpectedOn <= date && (p.ExpectedUntil == "" && p.ExpectedOn == date || p.ExpectedUntil >= date)
}

// localDay is the day of an RFC 3339 time in a zone, "" when it is not one.
func localDay(at string, loc *time.Location) string {
	t := track.ParseTime(at)
	if t.IsZero() {
		return ""
	}
	return t.In(loc).Format("2006-01-02")
}

// pocketDay says what happened with the person's parcels on one day, from
// what /day reads: what was delivered (done), what was sent, what was
// expected, and the news about the others (out for delivery, at customs). A
// parcel delivered that day shows once, not again with its "Delivered" step.
func (s *Server) pocketDay(r *http.Request, date string, tz *time.Location) (*core.Day, error) {
	ctx := r.Context()
	uid := actorID(ctx)
	loc := time.Local
	if tz != nil {
		loc = tz
	}
	day, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return nil, nil
	}
	all, err := s.store.AllParcels(ctx, uid)
	if err != nil {
		return nil, err
	}
	// steps are kept with the zone the service gave them: read a wider
	// window and keep the ones whose local day it is
	evs, err := s.store.EventsBetween(ctx, uid, day.Add(-14*time.Hour).UTC().Format(time.RFC3339), day.AddDate(0, 0, 1).Add(14*time.Hour).UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	st, _ := s.store.GetSettings(ctx, uid)
	now := time.Now()
	clock := func(at string) string {
		if t := track.ParseTime(at); !t.IsZero() {
			return t.In(loc).Format("15:04")
		}
		return ""
	}
	out := &core.Day{Date: date}
	byID := map[int64]store.Parcel{}
	counted := map[int64]bool{}
	delivered := map[int64]bool{}
	nDelivered, nSent, nExpected := 0, 0, 0
	item := func(p store.Parcel, kind, at, sub string, done bool) {
		out.Items = append(out.Items, core.DayItem{ID: "parcel:" + itoa(p.ID), Time: at, Title: p.Title, Sub: sub, Kind: kind, Done: done, URL: parcelPath(p.ID)})
	}
	for _, p := range all {
		byID[p.ID] = p
		v := s.view(p, st, now)
		switch {
		case p.Status == track.StatusDelivered && localDay(p.DeliveredAt, loc) == date:
			nDelivered++
			counted[p.ID], delivered[p.ID] = true, true
			item(p, "delivered", clock(p.DeliveredAt), dots(v.CarrierName, p.Shop), true)
		case expectedOnDay(p, date):
			nExpected++
			counted[p.ID] = true
			item(p, "expected", "", dots(statusWords(v), v.CarrierName), false)
		}
		if p.Direction == "out" && (p.OrderedOn == date || p.OrderedOn == "" && localDay(p.CreatedAt, loc) == date) {
			nSent++
			counted[p.ID] = true
			at := ""
			if p.OrderedOn == "" {
				at = clock(p.CreatedAt)
			}
			item(p, "sent", at, dots("Sent"+toWhom(p), v.CarrierName), false)
		}
	}
	// the day's news, with the parcel's name
	news := map[int64]bool{}
	type stepKey struct {
		parcel     int64
		text, time string
	}
	shown := map[stepKey]bool{}
	for _, e := range evs {
		p, ok := byID[e.ParcelID]
		if !ok || localDay(e.At, loc) != date || delivered[p.ID] && e.Status == track.StatusDelivered {
			continue
		}
		k := stepKey{p.ID, strings.ToLower(strings.TrimSpace(e.Description)), clock(e.At)}
		if shown[k] {
			continue
		}
		shown[k] = true
		if !counted[p.ID] {
			news[p.ID] = true
		}
		out.Items = append(out.Items, core.DayItem{ID: "step:" + itoa(e.ID), Time: clock(e.At), Title: p.Title, Sub: dots(e.Description, e.Location),
			Kind: "news", Done: e.Status == track.StatusDelivered, URL: parcelPath(p.ID)})
	}
	if len(out.Items) == 0 {
		return nil, nil
	}

	// "1 parcel delivered, 1 expected": the first part names what is counted
	var parts []string
	part := func(n int, words string) {
		if n == 0 {
			return
		}
		if len(parts) == 0 {
			parts = append(parts, plural(n, "parcel", "parcels")+" "+words)
		} else {
			parts = append(parts, strconv.Itoa(n)+" "+words)
		}
	}
	part(nDelivered, "delivered")
	part(nSent, "sent")
	part(nExpected, "expected")
	part(len(news), "with news")
	out.Summary = strings.Join(parts, ", ")
	for _, m := range []struct {
		label string
		n     int
	}{{"Delivered", nDelivered}, {"Sent", nSent}, {"Expected", nExpected}} {
		if m.n > 0 {
			out.Metrics = append(out.Metrics, core.Metric{Label: m.label, Value: strconv.Itoa(m.n)})
		}
	}
	return out, nil
}

// toWhom is " to Ana" for a parcel sent to someone.
func toWhom(p store.Parcel) string {
	if p.ContactName != "" {
		return " to " + p.ContactName
	}
	return ""
}

// ---------- widget ----------

// arrivalWords says when a parcel on its way comes, as its row does:
// "today", "tomorrow", "Fri 4 Dec", "any day now", "2 days late".
func arrivalWords(v parcelView, now time.Time) string {
	switch {
	case v.Status == track.StatusOutForDelivery:
		return "today"
	case v.Status == track.StatusPickup:
		return "waiting for you"
	case v.ExpectedOn == "" || v.ArrivesIn == nil:
		return ""
	}
	today := now.Format("2006-01-02")
	day := func(s string) string {
		d, err := time.ParseInLocation("2006-01-02", s, time.Local)
		if err != nil {
			return s
		}
		return shortDay(d, now)
	}
	if v.ExpectedUntil != "" {
		switch {
		case v.ExpectedOn <= today && v.ExpectedUntil >= today:
			return "any day now"
		case v.ExpectedUntil < today:
			return "late"
		}
		return day(v.ExpectedOn) + " to " + day(v.ExpectedUntil)
	}
	switch n := *v.ArrivesIn; {
	case n == 0:
		return "today"
	case n == 1:
		return "tomorrow"
	case n > 1 && n < 7:
		d, _ := time.ParseInLocation("2006-01-02", v.ExpectedOn, time.Local)
		return d.Format("Mon ") + day(v.ExpectedOn)
	case n >= 7:
		return day(v.ExpectedOn)
	case n == -1:
		return "a day late"
	default:
		return strconv.Itoa(-n) + " days late"
	}
}

// pocketWidget is the home screen in a few lines, from what the overview
// reads: what is out for delivery, waits to be picked up or arrives today
// first, then what has a problem or no news for a while, then the rest on
// its way, soonest first. It reads only what Parcels already keeps (the
// tracking round fills that in); nothing here asks a carrier.
func (s *Server) pocketWidget(r *http.Request) (*core.WidgetData, error) {
	ctx := r.Context()
	uid := actorID(ctx)
	active, _, err := s.store.ListParcels(ctx, uid, store.ParcelFilter{View: "active", Limit: 500})
	if err != nil || len(active) == 0 {
		return nil, err
	}
	st, _ := s.store.GetSettings(ctx, uid)
	now := time.Now()
	var needs, problems, coming []parcelView
	nOut, nPickup, nToday, nProblem, nQuiet := 0, 0, 0, 0, 0
	for _, p := range active {
		v := s.view(p, st, now)
		switch {
		case p.Status == track.StatusOutForDelivery:
			nOut++
			needs = append(needs, v)
		case p.Status == track.StatusPickup:
			nPickup++
			needs = append(needs, v)
		case v.ArrivesIn != nil && *v.ArrivesIn == 0:
			nToday++
			needs = append(needs, v)
		case p.Status == track.StatusException:
			nProblem++
			problems = append(problems, v)
		case v.Stale:
			nQuiet++
			problems = append(problems, v)
		default:
			coming = append(coming, v)
		}
	}
	out := &core.WidgetData{URL: "/", Metrics: []core.Metric{
		{Label: "On the way", Value: strconv.Itoa(len(active))},
		{Label: "Today", Value: strconv.Itoa(len(needs))},
		{Label: "Problems", Value: strconv.Itoa(len(problems))},
	}}
	var parts []string
	for _, c := range []struct {
		n     int
		words string
	}{{nOut, "out for delivery"}, {nPickup, "ready to pick up"}, {nToday, "arriving today"}, {nProblem, "with a problem"}, {nQuiet, "with no news for a while"}, {len(coming), "on the way"}} {
		if c.n > 0 {
			parts = append(parts, strconv.Itoa(c.n)+" "+c.words)
		}
	}
	out.Line = strings.Join(parts, ", ")
	for _, group := range [][]parcelView{needs, problems, coming} {
		for _, v := range group {
			if len(out.Items) == 8 {
				break
			}
			detail := statusWords(v)
			if v.Status != track.StatusPickup {
				detail = dots(detail, arrivalWords(v, now))
			}
			out.Items = append(out.Items, core.WidgetItem{ID: itoa(v.ID), Title: v.Title, Detail: detail, URL: parcelPath(v.ID)})
		}
	}
	return out, nil
}

// ---------- capture ----------

// captureLines splits a quick line into its tracking number, the parcel's
// name and notes: the number is looked for in the title, then line by line
// in the text; what is left of that line goes away with it when it was only
// the number ("Tracking: RR…"), and the first line left names the parcel.
func captureLines(in core.CaptureInput) (number, title, notes string) {
	var lines []string
	if in.Title != "" {
		lines = append(lines, in.Title)
	}
	lines = append(lines, strings.Split(in.Text, "\n")...)
	for i, l := range lines {
		n, rest := track.FindNumber(l)
		if n == "" {
			continue
		}
		number, lines[i] = n, rest
		break
	}
	var kept []string
	for _, l := range lines {
		if l = strings.Join(strings.Fields(l), " "); l != "" {
			kept = append(kept, l)
		}
	}
	if len(kept) > 0 {
		title, notes = kept[0], strings.Join(kept[1:], "\n")
	}
	return number, title, notes
}

// pocketCapture keeps a quick line as a delivery to follow, through what POST
// /parcels does: the number is checked and its carrier guessed the same way,
// the parcel is the person's own (added by them, as if typed here: the app
// that passed the line on may not change it later), and the first check with
// the tracking service starts in the background after the answer, as for any
// new parcel. A line without a tracking number is refused. The same
// request_id again answers the same parcel and adds nothing: the receipt is
// written in the transaction that adds the parcel.
func (s *Server) pocketCapture(r *http.Request, in core.CaptureInput) (*core.CaptureResult, error) {
	ctx := r.Context()
	uid := actorID(ctx)
	op, err := core.NewOperation(fmt.Sprintf("parcels:user:%d:%s", uid, core.CapturePath), in.RequestID, in)
	if err != nil {
		return nil, err
	}
	var res core.CaptureResult
	replay := func() (*core.CaptureResult, error) {
		found, err := op.Replay(ctx, s.store.DB, &res)
		switch {
		case errors.Is(err, core.ErrOperationConflict):
			return nil, fmt.Errorf("%w: this was already kept with other words", core.ErrCapture)
		case err != nil || !found:
			return nil, err
		}
		return &res, nil
	}
	if kept, err := replay(); kept != nil || err != nil {
		return kept, err
	}

	number, title, notes := captureLines(in)
	if number == "" {
		return nil, fmt.Errorf("%w: there is no tracking number in that line: write it with what is coming, like \"RR123456785CN headphones\"", core.ErrCapture)
	}
	st, _ := s.store.GetSettings(ctx, uid)
	p := store.Parcel{Direction: "in", Currency: st.Currency, Status: track.StatusPending, Source: "manual"}
	if err := (parcelInput{TrackingNumber: &number, Title: &title, Notes: &notes}).apply(&p, map[string]bool{"tracking_number": true, "title": true, "notes": true}); err != nil {
		return nil, fmt.Errorf("%w: %s", core.ErrCapture, err.Error())
	}
	if old, err := s.store.ParcelByNumber(ctx, uid, p.TrackingNumber); err == nil {
		return nil, fmt.Errorf("%w: you already follow this tracking number, in \"%s\"", core.ErrCapture, old.Title)
	}
	_, err = s.createParcel(ctx, uid, &p, st, func(tx *sql.Tx) error {
		res = core.CaptureResult{ID: itoa(p.ID), Title: p.Title, URL: parcelPath(p.ID)}
		return op.Save(ctx, tx, res)
	})
	if err != nil {
		// the same line sent twice at once: the other one kept it
		if kept, rerr := replay(); kept != nil || rerr != nil {
			return kept, rerr
		}
		if errors.Is(err, store.ErrConflict) {
			return nil, fmt.Errorf("%w: you already follow this tracking number", core.ErrCapture)
		}
		return nil, err
	}
	return &res, nil
}
