package track

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Event is one step a tracking service reported.
type Event struct {
	At          string `json:"at"` // RFC 3339
	Status      string `json:"status"`
	Description string `json:"description"`
	Location    string `json:"location"`
}

// Fingerprint identifies the same step when a service reports it again.
func (e Event) Fingerprint(source string) string {
	h := sha1.Sum([]byte(source + "|" + e.At + "|" + strings.ToLower(strings.TrimSpace(e.Description)) + "|" + strings.ToLower(strings.TrimSpace(e.Location))))
	return hex.EncodeToString(h[:10])
}

// Result is what a tracking service knows about one parcel.
type Result struct {
	Number        string  `json:"number"`
	Found         bool    `json:"found"`  // the service has news about it
	Status        string  `json:"status"` // one of the statuses
	Text          string  `json:"text"`   // the latest step in words
	Location      string  `json:"location"`
	At            string  `json:"at"`
	ExpectedFrom  string  `json:"expected_from"` // YYYY-MM-DD
	ExpectedUntil string  `json:"expected_until"`
	CarrierName   string  `json:"carrier_name"` // as the service calls it
	CarrierRef    string  `json:"carrier_ref"`  // the service's own carrier id
	Events        []Event `json:"events"`       // oldest first
}

// ErrNoKey means the service needs a key the person has not given.
var ErrNoKey = errors.New("no API key")

// QuotaError means the service refused for want of quota.
type QuotaError struct{ Msg string }

func (e *QuotaError) Error() string { return e.Msg }

// ---------------- the words carriers use ----------------

// statusWords map phrases (lower case, several languages) to a status. The
// first match in this order wins, so the more final words come first.
var statusWords = []struct {
	status string
	words  []string
}{
	{StatusReturned, []string{"returned to sender", "return to sender", "returning to sender", "being returned", "sent back", "retour", "zurück an den absender", "vraćeno pošiljaocu", "vraćen pošiljaocu", "vraćeno pošiljatelju", "povrat pošiljaocu"}},
	{StatusDelivered, []string{"delivered", "has been delivered", "was delivered", "signed for", "zugestellt", "ausgeliefert", "livré", "entregado", "consegnato", "doręczono", "uručen", "uručena", "uručeno", "isporučen", "isporučena", "isporučeno", "dostavljen", "dostavljena", "dostavljeno", "pošiljka je uručena", "signed", "received by"}},
	{StatusPickup, []string{"ready for pickup", "ready for pick-up", "ready to collect", "available for pickup", "available for collection", "awaiting collection", "awaiting pickup", "at the pickup point", "in the parcel locker", "abholbereit", "zur abholung", "in der filiale", "packstation", "spremno za preuzimanje", "čeka preuzimanje", "ceka preuzimanje", "na preuzimanju", "u poštanskom uredu", "obavještenje o prispijeću", "prispjela u poštu", "paketomat"}},
	{StatusOutForDelivery, []string{"out for delivery", "with delivery courier", "on the delivery vehicle", "on its way to you today", "arriving today", "in zustellung", "wird heute zugestellt", "en cours de livraison", "u dostavi", "na dostavi", "predat kuriru", "predata kuriru", "zadužen dostavljač", "u isporuci"}},
	{StatusException, []string{"delivery attempt", "attempted delivery", "failed delivery", "could not be delivered", "unable to deliver", "delivery failed", "exception", "held by customs", "customs hold", "address problem", "damaged", "lost", "nicht zugestellt", "zustellung nicht möglich", "neuspješna dostava", "neuspjela dostava", "nije uručen", "nije uručena", "nije dostavljen", "pokušaj dostave", "carinsko zadržavanje"}},
	{StatusInfoReceived, []string{"label created", "shipping label", "information received", "shipment information", "electronic information", "pre-advice", "order information received", "awaiting item", "elektronisch angekündigt", "sendungsdaten übermittelt", "najavljena", "podaci o pošiljci"}},
	{StatusInTransit, []string{"in transit", "shipped", "dispatched", "has shipped", "on its way", "departed", "arrived at", "accepted", "picked up", "customs", "export", "import", "sorting", "hub", "unterwegs", "versandt", "verschickt", "bearbeitet", "u prometu", "u tranzitu", "poslan", "poslana", "poslano", "otpremljen", "otpremljena", "prijem", "primljen", "primljena", "carina", "izvoz", "uvoz", "sortiranje"}},
}

// Classify reads a step's words and says which status they mean, "" when
// they say nothing clear.
func Classify(text string) string {
	t := strings.ToLower(text)
	for _, w := range []string{"will be delivered", "to be delivered", "expected to be delivered", "being delivered", "scheduled for delivery", "estimated delivery", "wird zugestellt", "voraussichtlich", "bit će uručen", "bice urucen", "biće dostavljen", "bit će dostavljen"} {
		if strings.Contains(t, w) {
			if strings.Contains(t, "today") || strings.Contains(t, "heute") || strings.Contains(t, "danas") {
				return StatusOutForDelivery
			}
			return StatusInTransit
		}
	}
	if strings.Contains(t, "delivered to") {
		for _, w := range []string{"locker", "pickup point", "pick-up point", "parcel shop", "collection point", "parcelshop", "packstation"} {
			if strings.Contains(t, w) {
				return StatusPickup
			}
		}
		for _, w := range []string{"airline", "carrier", "customs", "hub", "facility", "warehouse", "sorting", "transit", "logistics", "courier"} {
			if strings.Contains(t, w) {
				return StatusInTransit
			}
		}
	}
	for _, sw := range statusWords {
		for _, w := range sw.words {
			if strings.Contains(t, w) {
				// "not delivered" and friends are a problem, not a delivery
				if sw.status == StatusDelivered && (strings.Contains(t, "not delivered") || strings.Contains(t, "nicht zugestellt") || strings.Contains(t, "nije uruč") || strings.Contains(t, "nije dostav") || strings.Contains(t, "attempt")) {
					return StatusException
				}
				return sw.status
			}
		}
	}
	return ""
}

// ---------------- 17TRACK ----------------

// SeventeenTrack is the 17TRACK API v2.2 (https://api.17track.net). Every
// number registered uses one unit of the account's quota; reading what it
// found is free.
type SeventeenTrack struct {
	Key  string
	Base string // default https://api.17track.net/track/v2.2
	HTTP *http.Client
}

// New17Track makes a client for one person's key.
func New17Track(key string) *SeventeenTrack {
	return &SeventeenTrack{Key: strings.TrimSpace(key), Base: "https://api.17track.net/track/v2.2", HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (c *SeventeenTrack) call(ctx context.Context, path string, body, v any) error {
	if c == nil || c.Key == "" {
		return ErrNoKey
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.Base, "/")+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("17token", c.Key)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return errors.New("17TRACK refused the key")
	}
	if res.StatusCode == http.StatusTooManyRequests {
		return errors.New("17TRACK: too many requests, trying again later")
	}
	if res.StatusCode >= 300 {
		return fmt.Errorf("17TRACK: HTTP %d", res.StatusCode)
	}
	var env struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("17TRACK: %w", err)
	}
	if env.Code != 0 {
		var e struct {
			Errors []struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"errors"`
		}
		_ = json.Unmarshal(env.Data, &e)
		if len(e.Errors) > 0 {
			return fmt.Errorf("17TRACK: %s", e.Errors[0].Message)
		}
		return fmt.Errorf("17TRACK: code %d", env.Code)
	}
	if v == nil {
		return nil
	}
	return json.Unmarshal(env.Data, v)
}

type t17Number struct {
	Number  string `json:"number"`
	Carrier int    `json:"carrier,omitempty"`
}

type t17Rejected struct {
	Number string `json:"number"`
	Error  struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Quota is what is left of the account's allowance.
type Quota struct {
	Total  int `json:"quota_total"`
	Used   int `json:"quota_used"`
	Remain int `json:"quota_remain"`
	Today  int `json:"today_used"`
}

// Quota reads the account's allowance (also the key check).
func (c *SeventeenTrack) Quota(ctx context.Context) (*Quota, error) {
	var q Quota
	if err := c.call(ctx, "/getquota", map[string]any{}, &q); err != nil {
		return nil, err
	}
	return &q, nil
}

// Register asks 17TRACK to follow a number (one unit of quota). A number it
// already follows counts as registered. It returns 17TRACK's carrier id.
func (c *SeventeenTrack) Register(ctx context.Context, number, carrierRef string) (string, error) {
	n := t17Number{Number: number}
	n.Carrier, _ = strconv.Atoi(carrierRef)
	var out struct {
		Accepted []t17Number   `json:"accepted"`
		Rejected []t17Rejected `json:"rejected"`
	}
	if err := c.call(ctx, "/register", []t17Number{n}, &out); err != nil {
		return "", err
	}
	for _, a := range out.Accepted {
		if a.Number == number {
			return strconv.Itoa(a.Carrier), nil
		}
	}
	for _, r := range out.Rejected {
		if r.Number != number {
			continue
		}
		msg := strings.ToLower(r.Error.Message)
		switch {
		case r.Error.Code == -18019901 || strings.Contains(msg, "already"):
			return carrierRef, nil
		case strings.Contains(msg, "quota"):
			return "", &QuotaError{"17TRACK: the allowance on this key is used up"}
		}
		return "", fmt.Errorf("17TRACK: %s", r.Error.Message)
	}
	return carrierRef, nil
}

// Stop tells 17TRACK to stop following numbers (delivered or deleted parcels).
func (c *SeventeenTrack) Stop(ctx context.Context, numbers ...string) error {
	if len(numbers) == 0 {
		return nil
	}
	body := make([]t17Number, 0, len(numbers))
	for _, n := range numbers {
		body = append(body, t17Number{Number: n})
	}
	return c.call(ctx, "/stoptrack", body, nil)
}

type t17Event struct {
	TimeISO     string `json:"time_iso"`
	TimeUTC     string `json:"time_utc"`
	Description string `json:"description"`
	Location    string `json:"location"`
	Stage       string `json:"stage"`
	SubStatus   string `json:"sub_status"`
}

type t17Info struct {
	Number    string `json:"number"`
	Carrier   int    `json:"carrier"`
	TrackInfo *struct {
		LatestStatus struct {
			Status    string `json:"status"`
			SubStatus string `json:"sub_status"`
		} `json:"latest_status"`
		LatestEvent *t17Event `json:"latest_event"`
		TimeMetrics struct {
			Estimated struct {
				From string `json:"from"`
				To   string `json:"to"`
			} `json:"estimated_delivery_date"`
		} `json:"time_metrics"`
		Tracking struct {
			Providers []struct {
				Provider struct {
					Key  int    `json:"key"`
					Name string `json:"name"`
				} `json:"provider"`
				Events []t17Event `json:"events"`
			} `json:"providers"`
		} `json:"tracking"`
	} `json:"track_info"`
}

// status17 maps 17TRACK's main and sub statuses.
func status17(main, sub string) string {
	switch {
	case strings.HasPrefix(sub, "Exception_Return"):
		return StatusReturned
	case main == "InfoReceived":
		return StatusInfoReceived
	case main == "InTransit":
		return StatusInTransit
	case main == "OutForDelivery":
		return StatusOutForDelivery
	case main == "AvailableForPickup":
		return StatusPickup
	case main == "Delivered":
		return StatusDelivered
	case main == "DeliveryFailure", main == "Exception":
		return StatusException
	case main == "Expired":
		return StatusExpired
	}
	return StatusPending
}

func event17(e t17Event) Event {
	at := firstNonEmpty(e.TimeISO, e.TimeUTC)
	st := ""
	if e.Stage != "" {
		st = status17(stageMain(e.Stage), e.SubStatus)
	}
	if st == "" || st == StatusPending {
		st = firstNonEmpty(Classify(e.Description), StatusInTransit)
	}
	return Event{At: normTime(at), Status: st, Description: strings.TrimSpace(e.Description), Location: strings.TrimSpace(e.Location)}
}

// stageMain maps an event's stage (InfoReceived, PickedUp, Departure,
// Arrival, AvailableForPickup, OutForDelivery, Delivered, Returning,
// Returned) to a main status.
func stageMain(stage string) string {
	switch stage {
	case "InfoReceived":
		return "InfoReceived"
	case "AvailableForPickup", "OutForDelivery", "Delivered":
		return stage
	case "Returning", "Returned":
		return "Exception"
	}
	return "InTransit"
}

// Get reads what 17TRACK knows about registered numbers (up to 40 a call).
func (c *SeventeenTrack) Get(ctx context.Context, numbers map[string]string) (map[string]*Result, error) {
	out := map[string]*Result{}
	body := make([]t17Number, 0, len(numbers))
	for n, ref := range numbers {
		x := t17Number{Number: n}
		x.Carrier, _ = strconv.Atoi(ref)
		body = append(body, x)
	}
	for len(body) > 0 {
		chunk := body
		if len(chunk) > 40 {
			chunk = body[:40]
		}
		body = body[len(chunk):]
		var data struct {
			Accepted []t17Info     `json:"accepted"`
			Rejected []t17Rejected `json:"rejected"`
		}
		if err := c.call(ctx, "/gettrackinfo", chunk, &data); err != nil {
			return out, err
		}
		for _, a := range data.Accepted {
			r := &Result{Number: a.Number, Status: StatusPending, CarrierRef: strconv.Itoa(a.Carrier), Events: []Event{}}
			out[a.Number] = r
			ti := a.TrackInfo
			if ti == nil {
				continue
			}
			r.Status = status17(ti.LatestStatus.Status, ti.LatestStatus.SubStatus)
			r.Found = ti.LatestStatus.Status != "" && ti.LatestStatus.Status != "NotFound"
			r.ExpectedFrom, r.ExpectedUntil = dateOnly(ti.TimeMetrics.Estimated.From), dateOnly(ti.TimeMetrics.Estimated.To)
			if r.ExpectedUntil == r.ExpectedFrom {
				r.ExpectedUntil = ""
			}
			seen := map[string]bool{}
			for _, p := range ti.Tracking.Providers {
				if r.CarrierName == "" {
					r.CarrierName = p.Provider.Name
				}
				for _, e := range p.Events {
					ev := event17(e)
					k := ev.At + "|" + ev.Description
					if ev.At == "" || seen[k] {
						continue
					}
					seen[k] = true
					r.Events = append(r.Events, ev)
				}
			}
			sortEvents(r.Events)
			if ti.LatestEvent != nil {
				le := event17(*ti.LatestEvent)
				r.Text, r.Location, r.At = le.Description, le.Location, le.At
			} else if n := len(r.Events); n > 0 {
				r.Text, r.Location, r.At = r.Events[n-1].Description, r.Events[n-1].Location, r.Events[n-1].At
			}
		}
		for _, rj := range data.Rejected {
			// not registered (yet): nothing to read
			out[rj.Number] = &Result{Number: rj.Number, Status: StatusPending, Events: []Event{}}
		}
	}
	return out, nil
}

// ---------------- Cainiao ----------------

// Cainiao reads Cainiao's public tracking (AliExpress parcels and the posts
// carrying them). No key; best effort: when the page changes it simply
// finds nothing.
type Cainiao struct {
	Base string // default https://global.cainiao.com
	HTTP *http.Client
	UA   string
}

// NewCainiao makes a client.
func NewCainiao(ua string) *Cainiao {
	return &Cainiao{Base: "https://global.cainiao.com", HTTP: &http.Client{Timeout: 25 * time.Second}, UA: ua}
}

type cnDetail struct {
	Time       int64  `json:"time"`
	TimeStr    string `json:"timeStr"`
	TimeZone   string `json:"timeZone"`
	Desc       string `json:"desc"`
	StandDesc  string `json:"standerdDesc"`
	DescTitle  string `json:"descTitle"`
	ActionCode string `json:"actionCode"`
	Group      struct {
		NodeDesc string `json:"nodeDesc"`
	} `json:"group"`
}

type cnModule struct {
	MailNo      string     `json:"mailNo"`
	Status      string     `json:"status"`
	StatusDesc  string     `json:"statusDesc"`
	DestCountry string     `json:"destCountry"`
	DetailList  []cnDetail `json:"detailList"`
	LatestTrace *cnDetail  `json:"latestTrace"`
	ETA         *struct {
		From string `json:"etaFrom"`
		To   string `json:"etaTo"`
	} `json:"globalEtaInfo"`
}

// cainiaoStatus maps Cainiao's action codes and statuses.
func cainiaoStatus(code, words string) string {
	c := strings.ToUpper(code)
	switch {
	case strings.Contains(c, "RETURN"):
		return StatusReturned
	case strings.Contains(c, "SIGN") && !strings.Contains(c, "FAIL") && !strings.Contains(c, "WAIT"):
		return StatusDelivered
	case strings.Contains(c, "WAIT_SIGN"), strings.Contains(c, "PICKUP"), strings.Contains(c, "STA_INBOUND"):
		return StatusPickup
	case strings.Contains(c, "DELIVERING"), strings.Contains(c, "OUT_FOR"):
		return StatusOutForDelivery
	case strings.Contains(c, "FAIL"), strings.Contains(c, "EXCEPTION"), strings.Contains(c, "ABNORMAL"):
		return StatusException
	case c == "WAIT_ACCEPT", strings.Contains(c, "CREATE"), strings.Contains(c, "ORDER"):
		return StatusInfoReceived
	}
	if st := Classify(words); st != "" {
		return st
	}
	if c == "" {
		return ""
	}
	return StatusInTransit
}

// Get reads several numbers at once (Cainiao takes up to ten).
func (c *Cainiao) Get(ctx context.Context, numbers []string) (map[string]*Result, error) {
	out := map[string]*Result{}
	for len(numbers) > 0 {
		chunk := numbers
		if len(chunk) > 10 {
			chunk = numbers[:10]
		}
		numbers = numbers[len(chunk):]
		u := strings.TrimRight(c.Base, "/") + "/global/detail.json?" + url.Values{"mailNos": {strings.Join(chunk, ",")}, "lang": {"en-US"}}.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return out, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", firstNonEmpty(c.UA, "Mozilla/5.0"))
		res, err := c.HTTP.Do(req)
		if err != nil {
			return out, err
		}
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
		res.Body.Close()
		if res.StatusCode >= 300 {
			return out, fmt.Errorf("Cainiao: HTTP %d", res.StatusCode)
		}
		var data struct {
			Success bool       `json:"success"`
			Module  []cnModule `json:"module"`
		}
		if err := json.Unmarshal(raw, &data); err != nil {
			return out, fmt.Errorf("Cainiao answered something else than tracking (%w)", err)
		}
		for _, m := range data.Module {
			r := &Result{Number: Normalize(m.MailNo), Status: StatusPending, CarrierName: "Cainiao", Events: []Event{}}
			out[r.Number] = r
			for _, d := range m.DetailList {
				words := firstNonEmpty(d.StandDesc, d.Desc, d.DescTitle)
				st := firstNonEmpty(cainiaoStatus(d.ActionCode, words), StatusInTransit)
				at := cnTime(d)
				if at == "" || words == "" {
					continue
				}
				r.Events = append(r.Events, Event{At: at, Status: st, Description: strings.TrimSpace(words)})
			}
			sortEvents(r.Events)
			if n := len(r.Events); n > 0 {
				r.Found = true
				last := r.Events[n-1]
				r.Text, r.At = last.Description, last.At
				r.Status = firstNonEmpty(cainiaoStatus(m.Status, m.StatusDesc), last.Status)
				if r.Status == StatusPending {
					r.Status = last.Status
				}
			}
			if m.ETA != nil {
				r.ExpectedFrom, r.ExpectedUntil = dateOnly(m.ETA.From), dateOnly(m.ETA.To)
			}
		}
	}
	return out, nil
}

// cnTime turns Cainiao's milliseconds (or its "2024-04-03 10:00:00" text in
// the time zone it names) into RFC 3339.
func cnTime(d cnDetail) string {
	loc := time.UTC
	if tz := strings.TrimPrefix(strings.ToUpper(d.TimeZone), "GMT"); tz != "" {
		if h, err := strconv.Atoi(strings.TrimPrefix(tz, "+")); err == nil {
			loc = time.FixedZone("GMT"+tz, h*3600)
		}
	}
	if d.TimeStr != "" {
		if t, err := time.ParseInLocation("2006-01-02 15:04:05", d.TimeStr, loc); err == nil {
			return t.Format(time.RFC3339)
		}
	}
	if d.Time > 0 {
		return time.UnixMilli(d.Time).In(loc).Format(time.RFC3339)
	}
	return ""
}

// ---------------- helpers ----------------

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// normTime turns the many time shapes services use into RFC 3339.
func normTime(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, f := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.Parse(f, s); err == nil {
			return t.Format(time.RFC3339)
		}
	}
	return ""
}

func dateOnly(s string) string {
	if t := normTime(s); t != "" {
		return t[:10]
	}
	return ""
}

// ParseTime reads an RFC 3339 time; zero when it is not one.
func ParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t, _ = time.Parse(time.RFC3339Nano, s)
	}
	return t
}

func sortEvents(ev []Event) {
	// insertion sort: short lists, stable, by instant
	for i := 1; i < len(ev); i++ {
		for j := i; j > 0 && ParseTime(ev[j].At).Before(ParseTime(ev[j-1].At)); j-- {
			ev[j], ev[j-1] = ev[j-1], ev[j]
		}
	}
}
