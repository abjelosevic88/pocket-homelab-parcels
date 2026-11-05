package track

import (
	"html"
	"net/url"
	"regexp"
	"strings"
)

// Shipment is what a shipping email says about one parcel.
type Shipment struct {
	Number  string `json:"tracking_number"`
	Carrier string `json:"carrier"`
	Shop    string `json:"shop"`
	Order   string `json:"order_number"`
	Title   string `json:"title"`
	Status  string `json:"status"` // what the email says happened, "" = nothing clear
}

// Email is the part of a message the reader looks at.
type Email struct {
	Subject  string
	FromName string
	FromAddr string
	Text     string
	HTML     string
}

// numberKeywords are the words that introduce a tracking number. Numbers
// made of digits only are taken only right after one of them.
var numberKeywords = regexp.MustCompile(`(?i)(tracking\s*(number|no\.?|#|id|code)?|track(ing)?\s+your|shipment\s*(number|no\.?|id)|parcel\s*(number|no\.?|id)|package\s*(number|no\.?|id)|consignment\s*(number|no\.?)|waybill|awb|sendungsnummer|sendungs-?nr\.?|paketnummer|trackingnummer|sendungsverfolgung|numéro de suivi|número de seguimiento|numero di tracciamento|broj\s+pošiljke|broj\s+posiljke|broj\s+za\s+praćenje|kod\s+za\s+praćenje|prać?enje\s+pošiljke|tracking\s+broj)`)

// candidates find numbers that identify their carrier by shape.
var candidates = regexp.MustCompile(`\b(?:[A-Z]{2}\s?\d{3}\s?\d{3}\s?\d{3}\s?[A-Z]{2}|1Z[0-9A-Z]{16}|LP\d{12,16}|YT\d{16}|TBA\d{12}|JJD\d{14,22}|JVGL\d{8,20}|3S[A-Z0-9]{11,13}|H\d{19}|4PX\d{10,20}(?:CN)?|CAINIAO[A-Z0-9]{6,20})\b`)

var tokenRe = regexp.MustCompile(`^[A-Z0-9\-]+$`)

// numberAfter reads the number right after a tracking word: on the same
// line, or on the next one when the word ends its line. Groups split by
// spaces ("RR 123 456 785 CN") are joined; a word ends it.
func numberAfter(rest string) string {
	rest = strings.TrimLeft(rest, " \t:：#.-–—")
	line, next, _ := strings.Cut(rest, "\n")
	if strings.TrimSpace(strings.Trim(line, ":：#.-–—")) == "" {
		line, _, _ = strings.Cut(next, "\n")
	}
	var parts []string
	prefix := false
	for _, t := range strings.Fields(line) {
		t = strings.Trim(t, ".,;:()[]")
		if !tokenRe.MatchString(t) {
			break
		}
		if !hasDigits(t, 1) {
			switch {
			case len(parts) == 0 && (t == "IS" || t == "JE" || t == "EST" || t == "LAUTET" || t == "NO" || t == "NR" || t == "BROJ" || t == "ID"):
				continue
			case len(parts) == 0 && len(t) == 2:
				parts, prefix = append(parts, t), true
				continue
			case prefix && len(t) == 2 && hasDigits(strings.Join(parts, ""), 6):
				parts = append(parts, t)
			}
			break
		}
		parts = append(parts, t)
		if len(parts) > 6 {
			break
		}
	}
	n := Normalize(strings.Join(parts, ""))
	if hasDigits(n, 6) {
		return n
	}
	return ""
}

// linkNumber finds the number in a carrier's tracking link.
var linkParams = []string{"tracknum", "tracknumbers", "tracking-id", "trackingid", "trackingnumber", "tracking_number", "tracking", "nums", "piececode", "barcode", "mailnolist", "mailnos", "id", "number", "awb", "tlabels", "trknbr", "sendungsnummer", "idc"}

var hrefRe = regexp.MustCompile(`(?i)href\s*=\s*["']([^"']+)["']`)
var tagRe = regexp.MustCompile(`(?s)<[^>]*>`)
var styleRe = regexp.MustCompile(`(?is)<(style|script)[^>]*>.*?</(style|script)>`)
var spaceRe = regexp.MustCompile(`[ \t\r\f\v]+`)

// orderRe finds an order number.
var orderRe = regexp.MustCompile(`(?i)(?:order|bestellung|bestellnummer|auftrag|narudžb[a-z]*|narudzb[a-z]*|porudžbin[a-z]*|porudzbin[a-z]*|commande|pedido|ordine)\s*(?:number|no\.?|nr\.?|nummer|broj|id|#|n°)?\s*[:#]?\s*#?\s*([A-Z0-9][A-Z0-9\-]{4,30})`)

// PlainText turns an email's HTML into text, links' addresses kept.
func PlainText(h string) string {
	h = styleRe.ReplaceAllString(h, " ")
	h = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</div>|</tr>|</li>|</h\d>`).ReplaceAllString(h, "\n")
	h = tagRe.ReplaceAllString(h, " ")
	h = html.UnescapeString(h)
	lines := strings.Split(h, "\n")
	out := lines[:0]
	for _, l := range lines {
		l = strings.TrimSpace(spaceRe.ReplaceAllString(l, " "))
		if l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// Links lists the addresses an email's HTML links to.
func Links(h string) []string {
	var out []string
	for _, m := range hrefRe.FindAllStringSubmatch(h, -1) {
		out = append(out, html.UnescapeString(m[1]))
	}
	return out
}

// LooksLikeShipping reports whether an email is about an order on its way
// (subject words), whether or not a number is found in it.
func LooksLikeShipping(subject, text string) bool {
	t := strings.ToLower(subject + "\n" + firstN(text, 1500))
	for _, w := range []string{"shipped", "has shipped", "on its way", "on the way", "dispatched", "tracking", "track your", "out for delivery", "delivered", "ready for pickup", "ready for pick-up", "shipment", "parcel", "package", "delivery",
		"versandt", "versendet", "unterwegs", "sendung", "paket", "zugestellt", "zustellung",
		"pošiljka", "posiljka", "paket", "poslana", "poslano", "isporuka", "dostava", "uručen", "preuzimanje", "praćenje"} {
		if strings.Contains(t, w) {
			return true
		}
	}
	return false
}

// Extract reads an email and returns the parcels it names: numbers found
// by shape anywhere, numbers right after a tracking word, and numbers in
// tracking links. Shop, order number, title and status come from the
// sender, subject and text.
func Extract(e Email) []Shipment {
	text := e.Text
	if strings.TrimSpace(text) == "" && e.HTML != "" {
		text = PlainText(e.HTML)
	}
	all := e.Subject + "\n" + text
	order := orderNumber(all)
	seen := map[string]bool{}
	var nums []string
	add := func(n string) {
		n = Normalize(n)
		if !ValidNumber(n) || seen[n] || n == Normalize(order) || looksLikeJunk(n) {
			return
		}
		seen[n] = true
		nums = append(nums, n)
	}
	upper := strings.ToUpper(all)
	for _, m := range candidates.FindAllString(upper, -1) {
		add(m)
	}
	for _, loc := range numberKeywords.FindAllStringIndex(upper, -1) {
		if n := numberAfter(upper[loc[1]:min(len(upper), loc[1]+120)]); n != "" {
			add(n)
		}
	}
	links := Links(e.HTML)
	links = append(links, plainLinks(text)...)
	for _, l := range links {
		if n := numberFromLink(l); n != "" {
			add(n)
		}
	}
	if len(nums) == 0 {
		return nil
	}
	shop := ShopName(e.FromName, e.FromAddr)
	title := titleFrom(e.Subject, shop)
	status := emailStatus(e.Subject, text)
	out := make([]Shipment, 0, len(nums))
	for _, n := range nums {
		out = append(out, Shipment{Number: n, Carrier: carrierFrom(n, all), Shop: shop, Order: order, Title: title, Status: status})
	}
	return out
}

// emailStatus is what the email says happened. Order confirmations and
// "shipped" emails are "in transit"; only clear words give later statuses.
func emailStatus(subject, text string) string {
	// the subject says it best; the body of "delivered" emails often also
	// lists "shipped" and "out for delivery" as earlier steps
	if st := Classify(subject); st != "" {
		return st
	}
	return Classify(firstN(text, 600))
}

// carrierFrom guesses the carrier from the number, then from a carrier
// named in the email.
func carrierFrom(n, text string) string {
	if c := Detect(n); c != "" {
		return c
	}
	t := strings.ToLower(text)
	for _, k := range []struct{ word, key string }{
		{"dhl express", "dhl-express"}, {"dhl", "dhl-paket"}, {"ups", "ups"}, {"fedex", "fedex"}, {"dpd", "dpd"}, {"gls", "gls"},
		{"hermes", "hermes"}, {"evri", "hermes"}, {"inpost", "inpost"}, {"euroexpress", "euroexpress"}, {"euro express", "euroexpress"},
		{"x express", "x-express"}, {"x-express", "x-express"}, {"overseas", "overseas"}, {"cainiao", "cainiao"}, {"yunexpress", "yunexpress"},
	} {
		if regexp.MustCompile(`\b` + regexp.QuoteMeta(k.word) + `\b`).MatchString(t) {
			return k.key
		}
	}
	return ""
}

func numberFromLink(l string) string {
	u, err := url.Parse(strings.TrimSpace(l))
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.ToLower(u.Host)
	all := strings.ToLower(host + u.Path + "?" + u.RawQuery + "#" + u.Fragment)
	tracky := false
	for _, w := range []string{"track", "verfolg", "sendung", "suivi", "tracing", "trace", "pracen", "praćen", "parcel", "shipment", "piececode", "cainiao",
		"dhl.", "ups.com", "fedex.", "dpd.", "gls-", "posta.", "post.", "postnl", "inpost"} {
		if strings.Contains(all, w) {
			tracky = true
			break
		}
	}
	if !tracky {
		return ""
	}
	q := u.Query()
	if f := u.Fragment; strings.Contains(f, "=") {
		if fq, err := url.ParseQuery(strings.TrimPrefix(f, "?")); err == nil {
			for k, v := range fq {
				q[k] = v
			}
		}
	}
	for k, v := range q {
		for _, p := range linkParams {
			if strings.EqualFold(k, p) && len(v) > 0 {
				n := Normalize(strings.Split(v[0], ",")[0])
				if ValidNumber(n) && hasDigits(n, 6) {
					return n
				}
			}
		}
	}
	// …/tracking/<number> or …/track/<number>
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		n := Normalize(parts[i])
		if n == strings.ToUpper(parts[i]) && ValidNumber(n) && hasDigits(n, 8) && (Detect(n) != "" || i > 0 && strings.Contains(strings.ToLower(parts[i-1]), "track")) {
			return n
		}
	}
	return ""
}

var plainURLRe = regexp.MustCompile(`https?://[^\s<>"')\]]+`)

func plainLinks(text string) []string { return plainURLRe.FindAllString(text, -1) }

func orderNumber(text string) string {
	for _, m := range orderRe.FindAllStringSubmatch(text, -1) {
		n := strings.Trim(m[1], "-")
		if hasDigits(n, 3) && !strings.EqualFold(n, "number") {
			return n
		}
	}
	return ""
}

// ShopName makes a shop name from the sender: its display name unless that
// is a mailbox word, else the middle of its domain.
func ShopName(name, addr string) string {
	n := strings.TrimSpace(strings.Trim(name, `"'`))
	low := strings.ToLower(n)
	generic := n == "" || strings.Contains(low, "noreply") || strings.Contains(low, "no-reply") || strings.Contains(low, "notification") ||
		strings.Contains(low, "shipping") || strings.Contains(low, "info@") || low == "customer service" || low == "orders" || low == "support"
	if !generic {
		for _, cut := range []string{" Customer Service", " Shipping", " Orders", " Notifications", " Team", " Store"} {
			if i := strings.Index(n, cut); i > 0 {
				n = n[:i]
			}
		}
		return strings.TrimSpace(n)
	}
	at := strings.LastIndex(addr, "@")
	if at < 0 {
		return n
	}
	labels := strings.Split(strings.ToLower(addr[at+1:]), ".")
	// mail.shop.co.uk → shop; e.aliexpress.com → aliexpress
	for i := len(labels) - 2; i >= 0; i-- {
		l := labels[i]
		if len(l) <= 3 && i > 0 && (l == "co" || l == "com" || l == "org" || l == "net") {
			continue
		}
		switch l {
		case "mail", "email", "e", "info", "news", "notify", "notification", "notifications", "shipping", "orders", "order":
			continue
		}
		if l == "" {
			break
		}
		return strings.ToUpper(l[:1]) + l[1:]
	}
	return n
}

// titleFrom makes a parcel name from the subject: the subject without the
// usual shipping words, else "Order from <shop>".
func titleFrom(subject, shop string) string {
	s := strings.TrimSpace(subject)
	low := strings.ToLower(s)
	for _, w := range []string{"your order", "your package", "your parcel", "your shipment", "has shipped", "has been shipped", "is on its way", "is on the way",
		"was delivered", "has been delivered", "is out for delivery", "shipping confirmation", "shipping update", "delivery update", "order confirmation", "tracking"} {
		if strings.Contains(low, w) {
			if shop != "" {
				return "Order from " + shop
			}
			return "Parcel"
		}
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

func hasDigits(s string, n int) bool {
	d := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			d++
		}
	}
	return d >= n
}

// looksLikeJunk drops phone numbers, dates and the like that sit next to a
// tracking word.
func looksLikeJunk(n string) bool {
	if len(n) < 8 && Detect(n) == "" {
		return true
	}
	if strings.HasPrefix(n, "HTTP") || strings.HasPrefix(n, "WWW") {
		return true
	}
	letters := 0
	for _, r := range n {
		if r >= 'A' && r <= 'Z' {
			letters++
		}
	}
	// a word such as "INFORMATION" with a few digits is not a number
	return letters > 6 && Detect(n) == ""
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
