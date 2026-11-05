package server

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-parcels/internal/store"
	"github.com/abjelosevic88/pocket-homelab-parcels/internal/track"
)

// ---- demo data (DEMO_DATA=true) ----
//
// Half a year of a person's parcels: eight on their way in every state
// (out for delivery, waiting at the post office, a failed attempt, one sent
// to a friend, one without a tracking number), forty delivered ones with
// their journeys, a jacket going back before its deadline and three
// shipping emails waiting for an answer. Tracking services are not asked
// in demo mode.

// demoTracker answers like a tracking service that has no news.
type demoTracker struct{}

func (demoTracker) Get(_ context.Context, numbers []string) (map[string]*track.Result, error) {
	out := map[string]*track.Result{}
	for _, n := range numbers {
		out[n] = &track.Result{Number: n, Status: track.StatusPending}
	}
	return out, nil
}

type demo17 struct{ demoTracker }

func (demo17) Register(context.Context, string, string) (string, error) { return "0", nil }
func (demo17) Get(_ context.Context, numbers map[string]string) (map[string]*track.Result, error) {
	out := map[string]*track.Result{}
	for n := range numbers {
		out[n] = &track.Result{Number: n, Status: track.StatusPending}
	}
	return out, nil
}
func (demo17) Stop(context.Context, ...string) error { return nil }
func (demo17) Quota(context.Context) (*track.Quota, error) {
	return &track.Quota{Total: 100, Used: 12, Remain: 88}, nil
}

type demoStep struct {
	hours  float64 // after the start
	status string
	text   string
	where  string
}

// journeys are typical routes, from the shop to the door.
var journeys = map[string][]demoStep{
	"cainiao": {
		{0, track.StatusInfoReceived, "Order information received by the seller", "Shenzhen"},
		{20, track.StatusInTransit, "Accepted by carrier", "Shenzhen"},
		{30, track.StatusInTransit, "Departed from sorting center", "Shenzhen"},
		{70, track.StatusInTransit, "Export customs clearance complete", "Hong Kong"},
		{96, track.StatusInTransit, "Departed from departure country/region", "Hong Kong"},
		{150, track.StatusInTransit, "Arrived at destination country/region", "Ljubljana"},
		{190, track.StatusInTransit, "Import customs clearance complete", "Ljubljana"},
		{230, track.StatusInTransit, "Arrived at local delivery center", "Sarajevo"},
		{250, track.StatusOutForDelivery, "Out for delivery", "Sarajevo"},
		{258, track.StatusDelivered, "Delivered", "Sarajevo"},
	},
	"dhl-paket": {
		{0, track.StatusInfoReceived, "The shipment has been prepared electronically by the sender", ""},
		{14, track.StatusInTransit, "The shipment has been picked up", "Leipzig"},
		{20, track.StatusInTransit, "The shipment has been processed in the parcel center", "Leipzig"},
		{40, track.StatusInTransit, "The shipment has been loaded onto the delivery vehicle for export", "Leipzig"},
		{70, track.StatusInTransit, "The shipment has arrived in the destination country", "Sarajevo"},
		{90, track.StatusOutForDelivery, "The shipment is out for delivery", "Sarajevo"},
		{96, track.StatusDelivered, "The shipment has been delivered", "Sarajevo"},
	},
	"ups": {
		{0, track.StatusInfoReceived, "Shipper created a label, UPS has not received the package yet", ""},
		{12, track.StatusInTransit, "Picked up", "Köln"},
		{18, track.StatusInTransit, "Departed from facility", "Köln"},
		{36, track.StatusInTransit, "Arrived at facility", "Wien"},
		{60, track.StatusInTransit, "Import scan", "Sarajevo"},
		{70, track.StatusOutForDelivery, "Out for delivery today", "Sarajevo"},
		{76, track.StatusDelivered, "Delivered — signed by Aleksandar", "Sarajevo"},
	},
	"post": {
		{0, track.StatusInfoReceived, "Electronic notification received", ""},
		{10, track.StatusInTransit, "Posting / collection", "Sarajevo"},
		{20, track.StatusInTransit, "Departure from outward office of exchange", "Sarajevo"},
		{60, track.StatusInTransit, "Arrival at inward office of exchange", "Zagreb"},
		{80, track.StatusInTransit, "Departure to the delivery office", "Zagreb"},
		{96, track.StatusDelivered, "Delivered to the addressee", "Split"},
	},
	"gls": {
		{0, track.StatusInfoReceived, "The parcel data was entered into the GLS system", ""},
		{16, track.StatusInTransit, "The parcel has left the parcel center", "Graz"},
		{40, track.StatusInTransit, "The parcel has reached the parcel center", "Sarajevo"},
		{48, track.StatusOutForDelivery, "The parcel is expected to be delivered during the day", "Sarajevo"},
		{52, track.StatusDelivered, "The parcel has been delivered", "Sarajevo"},
	},
}

type demoParcel struct {
	title, shop, carrier string
	price                int64 // cents; 0 = not known
	route                string
}

var demoDelivered = []demoParcel{
	{"Wireless earbuds", "AliExpress", "cainiao", 2399, "cainiao"},
	{"Kitchen scale", "Amazon.de", "dhl-paket", 1899, "dhl-paket"},
	{"Yoga mat", "Decathlon", "gls", 2499, "gls"},
	{"Phone charger", "AliExpress", "cainiao", 899, "cainiao"},
	{"Desk lamp", "IKEA", "dhl-paket", 3499, "dhl-paket"},
	{"Guitar picks", "Thomann", "ups", 650, "ups"},
	{"Hiking socks", "Decathlon", "gls", 1299, "gls"},
	{"Smart plug (4 pack)", "Amazon.de", "dhl-paket", 2999, "dhl-paket"},
	{"Bike light", "AliExpress", "cainiao", 1150, "cainiao"},
	{"Water bottle", "Decathlon", "gls", 999, "gls"},
	{"Coffee beans (1 kg)", "Kafeterija", "post-ba", 2800, "post"},
	{"Board game", "Amazon.de", "ups", 3999, "ups"},
	{"Running shorts", "Decathlon", "gls", 1499, "gls"},
	{"Camera strap", "AliExpress", "cainiao", 750, "cainiao"},
	{"Capo", "Thomann", "ups", 1490, "ups"},
	{"Notebook (3 pack)", "Amazon.de", "dhl-paket", 1299, "dhl-paket"},
	{"Cable organiser", "AliExpress", "cainiao", 499, "cainiao"},
	{"Plant pots", "IKEA", "dhl-paket", 1999, "dhl-paket"},
	{"Vitamin D", "iHerb", "ups", 1820, "ups"},
	{"Mouse pad", "AliExpress", "cainiao", 640, "cainiao"},
	{"Headlamp", "Decathlon", "gls", 1999, "gls"},
	{"Book: Dune", "Knjižara", "post-ba", 2400, "post"},
	{"Keyboard keycaps", "AliExpress", "cainiao", 2650, "cainiao"},
	{"Electric toothbrush heads", "Amazon.de", "dhl-paket", 1599, "dhl-paket"},
	{"Guitar stand", "Thomann", "ups", 2290, "ups"},
	{"Sunglasses", "AliExpress", "cainiao", 1399, "cainiao"},
	{"Thermos", "Decathlon", "gls", 2299, "gls"},
	{"HDMI cable", "Amazon.de", "dhl-paket", 899, "dhl-paket"},
	{"Phone stand", "AliExpress", "cainiao", 550, "cainiao"},
	{"Tea sampler", "Čajdžinica", "post-ba", 1900, "post"},
	{"Picture frames", "IKEA", "dhl-paket", 2499, "dhl-paket"},
	{"Tuner", "Thomann", "ups", 1990, "ups"},
	{"Swim goggles", "Decathlon", "gls", 1599, "gls"},
	{"USB stick 128 GB", "AliExpress", "cainiao", 1099, "cainiao"},
	{"Rain jacket", "Decathlon", "gls", 4999, "gls"},
	{"Scented candles", "Amazon.de", "dhl-paket", 1799, "dhl-paket"},
	{"Watch band", "AliExpress", "cainiao", 820, "cainiao"},
	{"Strings for the bass", "Thomann", "ups", 2890, "ups"},
}

func demoNumber(r *rand.Rand, carrier string) string {
	d := func(n int) string {
		var b strings.Builder
		for i := 0; i < n; i++ {
			b.WriteByte(byte('0' + r.Intn(10)))
		}
		return b.String()
	}
	switch carrier {
	case "cainiao":
		return "LP00" + d(12)
	case "dhl-paket":
		return "JJD00" + d(16)
	case "ups":
		return "1Z" + strings.ToUpper(fmt.Sprintf("%06X", r.Intn(1<<24))) + d(10)
	case "gls":
		return d(11)
	case "post-ba":
		return "RE" + d(9) + "BA"
	case "post-cn":
		return "LY" + d(9) + "CN"
	}
	return d(12)
}

func (s *Server) seedDemo(ctx context.Context, uid int64) error {
	// with AUTH_MODE=none this runs inside the first request; a client that
	// leaves must not cut the seeding short
	ctx = context.WithoutCancel(ctx)
	if n, _, _ := s.store.ListParcels(ctx, uid, store.ParcelFilter{View: "all", Limit: 1}); len(n) > 0 {
		return nil
	}
	r := rand.New(rand.NewSource(42))
	now := time.Now()
	add := func(p store.Parcel, route string, start time.Time, upto int) (*store.Parcel, error) {
		p.Source, p.Currency = "demo", firstNonEmpty(p.Currency, "EUR")
		if p.Direction == "" {
			p.Direction = "in"
		}
		steps := journeys[route]
		if upto < 0 || upto > len(steps) {
			upto = len(steps)
		}
		// stretch each journey a little so no two look alike
		stretch := 0.8 + r.Float64()*0.7
		if start.IsZero() {
			// on its way: the last step shown happened a few hours ago
			h := 0.0
			if upto > 0 {
				h = steps[upto-1].hours * stretch
			}
			start = now.Add(-time.Duration(h*float64(time.Hour)) - time.Duration(2+r.Intn(10))*time.Hour)
		}
		if p.OrderedOn == "" {
			p.OrderedOn = start.AddDate(0, 0, -r.Intn(2)).Format("2006-01-02")
		}
		var evs []track.Event
		for _, st := range steps[:upto] {
			at := start.Add(time.Duration(st.hours*stretch*float64(time.Hour)) + time.Duration(r.Intn(50))*time.Minute)
			if at.After(now) {
				break
			}
			evs = append(evs, track.Event{At: at.Format(time.RFC3339), Status: st.status, Description: st.text, Location: st.where})
		}
		if n := len(evs); n > 0 {
			last := evs[n-1]
			p.Status, p.StatusText, p.StatusAt, p.Location = last.Status, last.Description, last.At, last.Location
			if last.Status == track.StatusDelivered {
				p.DeliveredAt = last.At
			}
		}
		if err := s.store.CreateParcel(ctx, uid, &p); err != nil {
			return nil, err
		}
		if _, err := s.addEvents(ctx, uid, &p, "demo", evs); err != nil {
			return nil, err
		}
		return &p, nil
	}
	price := func(v int64) *int64 { return &v }
	// a step some hours after another, never later than an hour ago
	after := func(at string, d time.Duration) string {
		t := track.ParseTime(at).Add(d)
		if t.IsZero() || t.After(now.Add(-time.Hour)) {
			t = now.Add(-time.Hour)
		}
		return t.Format(time.RFC3339)
	}
	day := func(n int) string { return now.AddDate(0, 0, n).Format("2006-01-02") }

	// delivered over the last six months
	for i, d := range demoDelivered {
		start := now.Add(-time.Duration(180-i*4-r.Intn(3)) * 24 * time.Hour).Add(time.Duration(8+r.Intn(8)) * time.Hour)
		p := store.Parcel{Title: d.title, Shop: d.shop, Carrier: d.carrier, TrackingNumber: demoNumber(r, d.carrier)}
		if d.price > 0 {
			p.Price = price(d.price)
		}
		if d.route == "cainiao" && r.Intn(4) == 0 {
			p.ExtraCost = price(int64(300 + r.Intn(5)*100)) // handling fee at the post office
		}
		got, err := add(p, d.route, start, -1)
		if err != nil {
			return err
		}
		// delivered ones older than two weeks are out of the list
		if got.DeliveredAt != "" && now.Sub(track.ParseTime(got.DeliveredAt)) > 14*24*time.Hour {
			got.Archived = true
			_ = s.store.UpdateParcel(ctx, uid, got)
		}
	}

	// a jacket going back
	jacket, err := add(store.Parcel{Title: "Winter jacket", Shop: "Zalando", Carrier: "dhl-paket", TrackingNumber: demoNumber(r, "dhl-paket"),
		Price: price(12995), OrderNumber: "10203948576", ReturnBy: day(9), ReturnState: "returning",
		Notes: "Too small in the shoulders. Free returns with the label in the box."}, "dhl-paket", now.Add(-8*24*time.Hour), -1)
	if err != nil {
		return err
	}
	_ = jacket

	// on the way, in every state
	type live struct {
		p     store.Parcel
		route string
		upto  int
	}
	items := []live{
		{store.Parcel{Title: "Coffee grinder", Shop: "Amazon.de", Carrier: "ups", Price: price(5999), OrderNumber: "302-5551234-7788123"}, "ups", 6},
		{store.Parcel{Title: "Phone case", Shop: "AliExpress", Carrier: "cainiao", Price: price(699)}, "cainiao", 8},
		{store.Parcel{Title: "USB-C hub", Shop: "AliExpress", Carrier: "cainiao", Price: price(2149), ExpectedOn: day(4), ExpectedUntil: day(9)}, "cainiao", 6},
		{store.Parcel{Title: "Running shoes", Shop: "Decathlon", Carrier: "gls", Price: price(7999), ExpectedOn: day(1)}, "gls", 2},
		{store.Parcel{Title: "Guitar strings (3 sets)", Shop: "Thomann", Carrier: "ups", Price: price(2370)}, "ups", 1},
		{store.Parcel{Title: "Birthday present for Ana", Shop: "Ana", Carrier: "post-ba", Direction: "out", ContactName: "Ana Petrović", ExtraCost: price(650),
			Notes: "Registered letter with a scarf."}, "post", 4},
		{store.Parcel{Title: "LED strip", Shop: "Temu", Carrier: "post-cn", Price: price(1299)}, "cainiao", 8},
	}
	for _, it := range items {
		it.p.TrackingNumber = demoNumber(r, it.p.Carrier)
		got, err := add(it.p, it.route, time.Time{}, it.upto)
		if err != nil {
			return err
		}
		switch got.Title {
		case "Phone case":
			ev := track.Event{At: after(got.StatusAt, 7*time.Hour), Status: track.StatusPickup, Description: "Arrived at the post office, ready for pick-up", Location: "Pošta Sarajevo 71000"}
			_, _ = s.addEvents(ctx, uid, got, "demo", []track.Event{ev})
			got.Status, got.StatusText, got.StatusAt, got.Location = ev.Status, ev.Description, ev.At, ev.Location
			_ = s.store.UpdateParcel(ctx, uid, got)
		case "LED strip":
			ev := track.Event{At: after(got.StatusAt, 9*time.Hour), Status: track.StatusException, Description: "Delivery attempt failed — nobody at the address, a notice was left", Location: "Sarajevo"}
			_, _ = s.addEvents(ctx, uid, got, "demo", []track.Event{ev})
			got.Status, got.StatusText, got.StatusAt, got.Location = ev.Status, ev.Description, ev.At, ev.Location
			_ = s.store.UpdateParcel(ctx, uid, got)
		case "Coffee grinder":
			got.ExpectedOn = day(0)
			_ = s.store.UpdateParcel(ctx, uid, got)
		}
	}
	// no tracking number: the bookshop brings it on Friday
	friday := now
	for friday.Weekday() != time.Friday {
		friday = friday.AddDate(0, 0, 1)
	}
	if _, err := add(store.Parcel{Title: "Book: Project Hail Mary", Shop: "Buybook", Price: price(2600), ExpectedOn: friday.Format("2006-01-02"), ExpectedManual: true,
		Notes: "They deliver themselves, no tracking."}, "", now.Add(-2*24*time.Hour), 0); err != nil {
		return err
	}

	// shipping emails waiting for an answer
	finds := []store.Find{
		{Subject: "Your IKEA order is on its way", FromName: "IKEA", FromAddr: "noreply@order.ikea.com", Shop: "IKEA", Title: "Order from IKEA", Carrier: "dhl-paket", OrderNumber: "1182736450"},
		{Subject: "Votre commande a été expédiée", FromName: "Decathlon", FromAddr: "no-reply@decathlon.com", Shop: "Decathlon", Title: "Order from Decathlon", Carrier: "gls", OrderNumber: "DE-88213"},
		{Subject: "Ihr Paket ist unterwegs", FromName: "Thomann", FromAddr: "service@thomann.de", Shop: "Thomann", Title: "Order from Thomann", Carrier: "ups", OrderNumber: "51234987"},
	}
	for i, f := range finds {
		f.MailMessageID = int64(9000 + i)
		f.TrackingNumber = demoNumber(r, f.Carrier)
		f.StatusHint = track.StatusInTransit
		f.ReceivedAt = now.Add(-time.Duration(3+i*5) * time.Hour).UTC().Format(time.RFC3339)
		if _, err := s.store.SaveFind(ctx, uid, &f); err != nil {
			return err
		}
	}
	s.log.Info("demo data seeded", "user", uid)
	return nil
}
