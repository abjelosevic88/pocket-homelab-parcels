// Package track knows carriers and tracking numbers, talks to the tracking
// services (17TRACK with the person's own key, Cainiao for AliExpress
// parcels without one) and reads shipping emails.
package track

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// Statuses a parcel moves through. Everything a carrier, a tracking service
// or an email says is mapped onto these.
const (
	StatusPending        = "pending"          // added, nothing heard yet
	StatusInfoReceived   = "info_received"    // the shop told the carrier, nothing handed over yet
	StatusInTransit      = "in_transit"       // on its way
	StatusOutForDelivery = "out_for_delivery" // on the van today
	StatusPickup         = "pickup"           // waiting at a post office, locker or pick-up point
	StatusDelivered      = "delivered"
	StatusException      = "exception" // a failed attempt, customs hold, damage…
	StatusReturned       = "returned"  // on its way back to the sender, or back there
	StatusExpired        = "expired"   // no news for so long that tracking gave up
)

// Statuses in the order a parcel normally goes through them.
var Statuses = []string{StatusPending, StatusInfoReceived, StatusInTransit, StatusOutForDelivery, StatusPickup, StatusDelivered, StatusException, StatusReturned, StatusExpired}

// ValidStatus reports whether s is one of the statuses.
func ValidStatus(s string) bool {
	for _, x := range Statuses {
		if x == s {
			return true
		}
	}
	return false
}

// Final reports whether nothing more is expected to happen.
func Final(status string) bool {
	return status == StatusDelivered || status == StatusReturned || status == StatusExpired
}

// StatusLabel is the status in plain words.
func StatusLabel(s string) string {
	switch s {
	case StatusPending:
		return "Waiting for news"
	case StatusInfoReceived:
		return "Label created"
	case StatusInTransit:
		return "On the way"
	case StatusOutForDelivery:
		return "Out for delivery"
	case StatusPickup:
		return "Ready to pick up"
	case StatusDelivered:
		return "Delivered"
	case StatusException:
		return "Problem"
	case StatusReturned:
		return "Sent back"
	case StatusExpired:
		return "Tracking ended"
	}
	return s
}

// Carrier is a delivery company Pocket Parcels knows by name.
type Carrier struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Country string `json:"country,omitempty"` // ISO code of a national post
	Page    string `json:"-"`                 // tracking page, %s = the number
	// Patterns that identify its numbers. Numbers that only digits identify
	// (FedEx, DPD, GLS…) are never guessed from the number alone.
	Patterns []*regexp.Regexp `json:"-"`
	Cainiao  bool             `json:"cainiao"` // Cainiao's free tracking knows these
}

func re(s string) *regexp.Regexp { return regexp.MustCompile(`^(?:` + s + `)$`) }

// s10 is the UPU S10 format every national post uses: two letters, eight
// digits, a check digit and the country, e.g. RR123456785CN.
var s10 = re(`[A-Z]{2}\d{9}[A-Z]{2}`)

var carriers = []Carrier{
	{Key: "cainiao", Name: "Cainiao (AliExpress)", Page: "https://global.cainiao.com/newDetail.htm?mailNoList=%s", Cainiao: true,
		Patterns: []*regexp.Regexp{re(`LP\d{12,16}`), re(`CAINIAO[A-Z0-9]{6,20}`), re(`AE\d{12,16}`)}},
	{Key: "yunexpress", Name: "YunExpress", Page: "https://www.yuntrack.com/parcelTracking?id=%s", Patterns: []*regexp.Regexp{re(`YT\d{16}`)}},
	{Key: "4px", Name: "4PX", Page: "https://track.4px.com/#/result/0/%s", Patterns: []*regexp.Regexp{re(`4PX\d{10,20}(CN)?`)}},
	{Key: "ups", Name: "UPS", Page: "https://www.ups.com/track?tracknum=%s", Patterns: []*regexp.Regexp{re(`1Z[0-9A-Z]{16}`)}},
	{Key: "usps", Name: "USPS", Country: "US", Page: "https://tools.usps.com/go/TrackConfirmAction?tLabels=%s", Patterns: []*regexp.Regexp{re(`(92|93|94|95)\d{20}`)}},
	{Key: "fedex", Name: "FedEx", Page: "https://www.fedex.com/fedextrack/?trknbr=%s"},
	{Key: "dhl-express", Name: "DHL Express", Page: "https://www.dhl.com/global-en/home/tracking/tracking-express.html?submit=1&tracking-id=%s"},
	{Key: "dhl-paket", Name: "DHL Paket", Country: "DE", Page: "https://www.dhl.de/de/privatkunden/pakete-empfangen/verfolgen.html?piececode=%s",
		Patterns: []*regexp.Regexp{re(`JJD\d{14,22}`), re(`JVGL\d{8,20}`)}},
	{Key: "dpd", Name: "DPD", Page: "https://tracking.dpd.de/status/en_US/parcel/%s"},
	{Key: "gls", Name: "GLS", Page: "https://gls-group.com/track/%s"},
	{Key: "hermes", Name: "Hermes / Evri", Page: "https://www.myhermes.de/empfangen/sendungsverfolgung/sendungsinformation#%s", Patterns: []*regexp.Regexp{re(`H\d{19}`)}},
	{Key: "amazon", Name: "Amazon Logistics", Page: "https://track.amazon.com/tracking/%s", Patterns: []*regexp.Regexp{re(`TBA\d{12}`)}},
	{Key: "postnl", Name: "PostNL", Country: "NL", Page: "https://jouw.postnl.nl/track-and-trace/%s", Patterns: []*regexp.Regexp{re(`3S[A-Z0-9]{11,13}`)}},
	{Key: "inpost", Name: "InPost", Page: "https://inpost.pl/sledzenie-przesylek?number=%s", Patterns: []*regexp.Regexp{re(`\d{24}`)}},
	{Key: "euroexpress", Name: "EuroExpress", Country: "BA", Page: "https://www.euroexpress.ba/"},
	{Key: "x-express", Name: "X Express", Country: "BA", Page: "https://www.x-express.ba/"},
	{Key: "overseas", Name: "Overseas Express", Page: "https://www.overseas.hr/"},
	{Key: "other", Name: "Other"},
}

// posts are the national posts by the country code an S10 number ends in.
var posts = map[string]string{
	"AT": "Österreichische Post", "AU": "Australia Post", "BA": "BH Pošta", "BE": "bpost", "BG": "Bulgarian Posts",
	"CA": "Canada Post", "CH": "Swiss Post", "CN": "China Post", "CZ": "Česká pošta", "DE": "Deutsche Post", "DK": "PostNord",
	"EE": "Omniva", "ES": "Correos", "FI": "Posti", "FR": "La Poste", "GB": "Royal Mail", "GR": "ELTA", "HK": "Hongkong Post",
	"HR": "Hrvatska pošta", "HU": "Magyar Posta", "IE": "An Post", "IN": "India Post", "IT": "Poste Italiane", "JP": "Japan Post",
	"KR": "Korea Post", "LT": "Lietuvos paštas", "LV": "Latvijas Pasts", "ME": "Pošta Crne Gore", "MK": "Pošta na Severna Makedonija",
	"NL": "PostNL", "NO": "Posten", "PL": "Poczta Polska", "PT": "CTT", "RO": "Poșta Română", "RS": "Pošta Srbije", "SE": "PostNord",
	"SG": "Singapore Post", "SI": "Pošta Slovenije", "SK": "Slovenská pošta", "TR": "PTT", "TW": "Chunghwa Post", "UA": "Ukrposhta",
	"US": "USPS", "MY": "Pos Malaysia", "TH": "Thailand Post",
}

// postPages are the national posts with a tracking page that takes the number.
var postPages = map[string]string{
	"DE": "https://www.deutschepost.de/de/s/sendungsverfolgung.html?piececode=%s",
	"GB": "https://www.royalmail.com/track-your-item#/tracking-results/%s",
	"US": "https://tools.usps.com/go/TrackConfirmAction?tLabels=%s",
	"HR": "https://posiljka.posta.hr/hr/tracking/trackingdata?barcode=%s",
	"NL": "https://jouw.postnl.nl/track-and-trace/%s",
	"AT": "https://www.post.at/s/sendungssuche?snr=%s",
	"CH": "https://service.post.ch/ekp-web/ui/entry/search/%s",
}

// cainiaoPosts are the posts whose parcels Cainiao usually carries (AliExpress).
var cainiaoPosts = map[string]bool{"CN": true, "HK": true, "SG": true, "MY": true}

// Normalize makes a typed or pasted number comparable: upper case, no
// spaces, dashes or dots.
func Normalize(n string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(n)) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ValidNumber reports whether n looks like a tracking number at all.
func ValidNumber(n string) bool {
	if len(n) < 6 || len(n) > 40 {
		return false
	}
	digits := 0
	for _, r := range n {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	return digits >= 4
}

// Lookup returns a carrier by key ("post-ba" style keys are national posts).
func Lookup(key string) (Carrier, bool) {
	if cc, ok := strings.CutPrefix(key, "post-"); ok {
		cc = strings.ToUpper(cc)
		name, known := posts[cc]
		if !known {
			if len(cc) != 2 {
				return Carrier{}, false
			}
			name = "Post (" + cc + ")"
		}
		return Carrier{Key: key, Name: name, Country: cc, Page: postPages[cc], Cainiao: cainiaoPosts[cc]}, true
	}
	for _, c := range carriers {
		if c.Key == key {
			return c, true
		}
	}
	return Carrier{}, false
}

// Name is the carrier's name, or "" when not known.
func Name(key string) string {
	if c, ok := Lookup(key); ok {
		return c.Name
	}
	return ""
}

// Detect guesses the carrier from the number alone: "" when it cannot tell.
func Detect(number string) string {
	n := Normalize(number)
	for _, c := range carriers {
		for _, p := range c.Patterns {
			if p.MatchString(n) {
				return c.Key
			}
		}
	}
	if s10.MatchString(n) {
		return "post-" + strings.ToLower(n[len(n)-2:])
	}
	return ""
}

// TrackingURL is a page where a person can follow the parcel themselves:
// the carrier's own page when it takes the number, else 17TRACK's.
func TrackingURL(carrier, number string) string {
	n := Normalize(number)
	if n == "" {
		return ""
	}
	if c, ok := Lookup(carrier); ok && c.Page != "" && strings.Contains(c.Page, "%s") {
		return fmt.Sprintf(c.Page, url.QueryEscape(n))
	}
	return "https://t.17track.net/en#nums=" + url.QueryEscape(n)
}

// CainiaoCovers reports whether Cainiao's free tracking can follow this parcel.
func CainiaoCovers(carrier, number string) bool {
	if carrier == "" {
		carrier = Detect(number)
	}
	c, ok := Lookup(carrier)
	return ok && c.Cainiao
}

// List is every named carrier for pickers, the national posts after the companies.
func List() []Carrier {
	out := make([]Carrier, 0, len(carriers)+len(posts))
	for _, c := range carriers {
		if c.Key != "other" {
			out = append(out, c)
		}
	}
	var cc []string
	for k := range posts {
		cc = append(cc, k)
	}
	sort.Slice(cc, func(i, j int) bool { return posts[cc[i]] < posts[cc[j]] })
	for _, k := range cc {
		c, _ := Lookup("post-" + strings.ToLower(k))
		out = append(out, c)
	}
	other, _ := Lookup("other")
	return append(out, other)
}
