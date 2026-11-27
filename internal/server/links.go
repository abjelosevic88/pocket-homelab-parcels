package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-parcels/internal/store"
)

// Pocket Core app links (docs/links.md in pocket-homelab-core), Parcels as
// the target: another Pocket app the person allowed in Core may act here for
// them with a pcx_ token, only within these scopes. Every route not listed
// in linkRoutes stays closed to it (403).

var linkOffers = []core.Offer{
	{Scope: "parcels:read", Name: "See your parcels", Description: "Read your parcels, where each one is and when it should arrive, their tracking history and what you sent back — for your journal, calendar, dashboards and Pocket AI."},
	{Scope: "parcels:write", Name: "Add parcels", Description: "Add a parcel to follow from a tracking number (an order confirmation, a shipping email) and add news about it. It may change or remove only the parcels that app added itself."},
}

// portable is what Pocket Move may copy out and bring in for a person
// (docs/move.md in pocket-homelab-core); Register adds move:export/import.
var portable = &core.Portable{
	Exports: []core.PortableExport{
		{Path: "/api/v1/export.json", File: "parcels.json", Kind: core.KindPocket, Title: "Your parcels with their tracking history, receipts and links"},
		{Path: "/api/v1/export.csv", File: "parcels.csv", Kind: core.KindCSV, Title: "Your parcels as a table"},
	},
	Imports: []core.PortableImport{
		{Path: "/api/v1/import", Kind: core.KindPocket, ContentType: "application/json", MaxBytes: 32 << 20, Title: "Adds the parcels from another box with their history; parcels already here (same tracking number) are left alone"},
	},
}

// capabilities is what Pocket Search, Pocket Day and Pocket Hub may do here
// (docs/capabilities.md in pocket-homelab-core); Register adds the pocket:*
// offers, capabilities.go answers them.
var capabilities = core.Capabilities{
	Search: &core.Searchable{What: "Your parcels, by name, shop, carrier, tracking number and notes", Kinds: []string{"delivery", "sent"},
		Bangs: []string{"parcels", "parcel", "track"}},
	Day:    &core.DayFeed{Label: "Parcels", One: "parcel", Many: "parcels", What: "The parcels delivered, sent or expected that day, and the news about the others"},
	Widget: &core.Widget{Title: "Parcels", What: "What is on its way, what arrives first, and what has a problem or no news"},
	Capture: &core.QuickCapture{Kinds: []core.CaptureKind{{Kind: "parcel", Label: "Parcel", What: "A delivery to follow, from its tracking number",
		Fields: []string{"title"}}}},
}

type linkRoute struct {
	methods string
	path    *regexp.Regexp
	scope   string // "" = any linked app
}

var linkRoutes = []linkRoute{
	{"GET", regexp.MustCompile(`^/api/v1/auth/me$`), ""},
	{"GET", regexp.MustCompile(`^/api/v1/(overview|summary|parcels|search|shops|carriers|stats|day/\d{4}-\d{2}-\d{2})$`), "parcels:read"},
	{"GET", regexp.MustCompile(`^/api/v1/parcels/\d+(/events)?$`), "parcels:read"},
	{"POST", regexp.MustCompile(`^/api/v1/parcels$`), "parcels:write"},
	{"PUT DELETE", regexp.MustCompile(`^/api/v1/parcels/\d+$`), "parcels:write"},
	{"POST", regexp.MustCompile(`^/api/v1/parcels/\d+/(events|check)$`), "parcels:write"},
	{"POST", regexp.MustCompile(`^/api/v1/detect$`), "parcels:read"},
}

var errLinkScope = errors.New("outside the link's scope")

// linkAllows reports whether a link token with these scopes may make this request.
func linkAllows(scopes []string, r *http.Request) bool {
	if portable.LinkAllows(scopes, r) || capabilities.LinkAllows(scopes, r) {
		return true
	}
	for _, lr := range linkRoutes {
		if !strings.Contains(" "+lr.methods+" ", " "+r.Method+" ") || !lr.path.MatchString(r.URL.Path) {
			continue
		}
		if lr.scope == "" {
			return true
		}
		for _, sc := range scopes {
			if sc == lr.scope {
				return true
			}
		}
	}
	return false
}

// linkUser validates a pcx_ token, checks the route against its scopes and
// maps it to the person it acts for. The second value is the acting app's slug.
func (s *Server) linkUser(ctx context.Context, tok string, r *http.Request) (*store.User, string, error) {
	in, err := s.core.Introspect(ctx, tok)
	if err != nil {
		return nil, "", err
	}
	if !in.Active || !in.Delegated() || in.Audience != "parcels" || in.User == nil || in.User.Email == "" || in.App == nil || in.App.Slug == "" {
		return nil, "", http.ErrNoCookie
	}
	if !linkAllows(strings.Fields(in.Scope), r) {
		return nil, "", errLinkScope
	}
	u, err := s.coreIdentityUser(ctx, in)
	return u, in.App.Slug, err
}

const linkAppKey ctxKey = 2

// linkApp is the acting app's slug when the request came in on a link token.
func linkApp(ctx context.Context) string {
	a, _ := ctx.Value(linkAppKey).(string)
	return a
}
