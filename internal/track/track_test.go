package track

import (
	"strings"
	"testing"
)

func TestDetect(t *testing.T) {
	cases := map[string]string{
		"RR123456785CN":          "post-cn",
		"rr 123 456 785 cn":      "post-cn",
		"RE123456789BA":          "post-ba",
		"LP00123456789012":       "cainiao",
		"1Z999AA10123456784":     "ups",
		"JJD0012345678901234":    "dhl-paket",
		"TBA123456789012":        "amazon",
		"YT1234567890123456":     "yunexpress",
		"9400111899223197428490": "usps",
		"123456789012":           "", // FedEx? DPD? never guessed
	}
	for in, want := range cases {
		if got := Detect(in); got != want {
			t.Errorf("Detect(%q) = %q, want %q", in, got, want)
		}
	}
	if Name("post-ba") != "BH Pošta" || Name("post-zz") != "Post (ZZ)" || Name("nope") != "" {
		t.Error("names")
	}
	if !CainiaoCovers("", "LP00123456789012") || !CainiaoCovers("post-cn", "RR123456785CN") || CainiaoCovers("ups", "1Z999AA10123456784") {
		t.Error("cainiao coverage")
	}
	if u := TrackingURL("ups", "1z999aa10123456784"); u != "https://www.ups.com/track?tracknum=1Z999AA10123456784" {
		t.Errorf("ups url %s", u)
	}
	if u := TrackingURL("post-ba", "RE123456789BA"); !strings.HasPrefix(u, "https://t.17track.net/") {
		t.Errorf("fallback url %s", u)
	}
}

func TestClassify(t *testing.T) {
	cases := map[string]string{
		"Delivered": StatusDelivered,
		"Your parcel has been delivered to the door": StatusDelivered,
		"The parcel will be delivered today":         StatusOutForDelivery,
		"Your parcel will be delivered on Friday":    StatusInTransit,
		"Delivered to the parcel locker":             StatusPickup,
		"Delivered to airline":                       StatusInTransit,
		"Pošiljka je uručena primaocu":               StatusDelivered,
		"Spremno za preuzimanje u pošti":             StatusPickup,
		"Delivery attempt failed — nobody home":      StatusException,
		"Not delivered: address unknown":             StatusException,
		"Die Sendung wurde zugestellt":               StatusDelivered,
		"Returned to sender":                         StatusReturned,
		"Shipping label created":                     StatusInfoReceived,
		"Departed from facility":                     StatusInTransit,
		"Hello":                                      "",
	}
	for in, want := range cases {
		if got := Classify(in); got != want {
			t.Errorf("Classify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExtract(t *testing.T) {
	// a shipped email with the number after a tracking word and an order number
	got := Extract(Email{Subject: "Your order #304-1234567-1234567 has shipped", FromName: "Amazon.de", FromAddr: "shipment-tracking@amazon.de",
		Text: "Hello,\nyour package is on its way.\nTracking number: 1Z 999 AA1 0123 4567 84\nCarrier: UPS\nCall us at +49 30 1234567"})
	if len(got) != 1 || got[0].Number != "1Z999AA10123456784" || got[0].Carrier != "ups" || got[0].Shop != "Amazon.de" || got[0].Order != "304-1234567-1234567" || got[0].Status != StatusInTransit {
		t.Fatalf("amazon = %+v", got)
	}
	// only a tracking link, in HTML, from a no-reply address
	got = Extract(Email{Subject: "Ihr Paket ist unterwegs", FromName: "noreply", FromAddr: "noreply@mail.thomann.de",
		HTML: `<p>Hallo!</p><a href="https://www.dhl.de/de/privatkunden/pakete-empfangen/verfolgen.html?piececode=00340434161234567890&amp;lang=de">Sendung verfolgen</a>`})
	if len(got) != 1 || got[0].Number != "00340434161234567890" || got[0].Shop != "Thomann" {
		t.Fatalf("thomann = %+v", got)
	}
	// a number by shape in a Bosnian email, delivered
	got = Extract(Email{Subject: "Pošiljka RE123456789BA je uručena", FromName: "BH Pošta", FromAddr: "info@posta.ba", Text: "Poštovani, vaša pošiljka je uručena."})
	if len(got) != 1 || got[0].Number != "RE123456789BA" || got[0].Status != StatusDelivered || got[0].Carrier != "post-ba" {
		t.Fatalf("bh posta = %+v", got)
	}
	// a newsletter with numbers but no tracking word: nothing
	if got := Extract(Email{Subject: "Black Friday deals", FromAddr: "news@shop.com", Text: "Save 20% on 123456789012 products. Call 0800123456."}); len(got) != 0 {
		t.Fatalf("newsletter = %+v", got)
	}
	// the order number is never taken for the tracking number
	got = Extract(Email{Subject: "Order confirmation", FromName: "Shop", FromAddr: "a@shop.com", Text: "Order number: 8812345678\nTracking number: 4455667788990"})
	if len(got) != 1 || got[0].Number != "4455667788990" {
		t.Fatalf("order vs tracking = %+v", got)
	}
	if !LooksLikeShipping("Your order has shipped", "") || LooksLikeShipping("Weekly newsletter", "Top stories") {
		t.Error("LooksLikeShipping")
	}
}
