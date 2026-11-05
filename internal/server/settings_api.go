package server

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-parcels/internal/ai"
	"github.com/abjelosevic88/pocket-homelab-parcels/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-parcels/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-parcels/internal/store"
	"github.com/abjelosevic88/pocket-homelab-parcels/internal/track"
)

// ---- settings ----

func mask(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "••••"
	}
	return s[:4] + "••••" + s[len(s)-4:]
}

// publicSettings is what one person sees: their settings with the secrets
// masked and, per Pocket app, the mode in effect (<app>_mode), whether it
// works and how (<app>_via: core | direct | "") and why not (<app>_detail).
func (s *Server) publicSettings(ctx context.Context, u *store.User, st store.Settings) map[string]any {
	out := map[string]any{
		"core_configured": s.core.Configured(), "core_registered": s.coreStatus()["registered"],
		"title": st.Title, "date_format": st.DateFormat, "time_format": st.TimeFormat, "week_start": st.WeekStart, "currency": st.Currency,
		"track_cainiao": st.TrackCainiao, "track17_key": mask(st.Track17Key), "track17_configured": st.Track17Key != "",
		"auto_archive_days": st.AutoArchiveDays, "stale_days": st.StaleDays,
		"notify_moves": st.NotifyMoves, "notify_out": st.NotifyOut, "notify_pickup": st.NotifyPickup, "notify_delivered": st.NotifyDelivered,
		"notify_problems": st.NotifyProblems, "notify_returns": st.NotifyReturns, "notify_stale": st.NotifyStale, "notify_hour": st.NotifyHour,
		"notify_url": st.NotifyURL, "notify_format": st.NotifyFormat, "notify_configured": st.NotifyURL != "",
		"mail_scan": st.MailScan, "mail_auto_add": st.MailAutoAdd, "mail_days": st.MailDays,
		"money_account_id": st.MoneyAccountID, "money_category_id": st.MoneyCategoryID,
		"wishlist_bought": st.WishlistBought, "inventory_auto": st.InventoryAuto, "inventory_location": st.InventoryLocation,
		"calendar_sync": st.CalendarSync, "calendar_returns": st.CalendarReturns, "calendar_id": st.CalendarID,
		"tasks_pickup": st.TasksPickup, "tasks_returns": st.TasksReturns,
		"ai_provider": st.AIProvider, "ai_pocket_model": st.AIPocketModel, "openai_api_key": mask(st.OpenAIKey), "openai_model": st.OpenAIModel, "openai_base_url": st.OpenAIBaseURL,
		"setup_complete": st.SetupComplete, "base_url": s.cfg.BaseURL,
	}
	for _, a := range pocketApps {
		mode, via, detail := s.connStatus(ctx, u, st, a)
		out[a.slug+"_mode"], out[a.slug+"_via"], out[a.slug+"_detail"] = mode, via, detail
		out[a.slug+"_configured"] = via != ""
		_, url, token, public := connSettings(st, a)
		out[a.slug+"_url"], out[a.slug+"_token"], out[a.slug+"_public_url"] = url, mask(token), public
		if public == "" && via != "" {
			public = s.resolvePublic(ctx, a.slug)
		}
		out[a.slug+"_open_url"] = public
	}
	return out
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.GetSettings(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), userFrom(r.Context()), st))
}

// handlePutSettings saves the caller's personal settings. The box-wide title
// is only taken from an admin.
func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	me := userFrom(r.Context())
	isAdmin := me.Role == "admin"
	cur, err := s.store.GetSettings(r.Context(), me.ID)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	raw := map[string]any{}
	var in store.Settings
	keys, err := decodeKeys(r, &raw)
	if err != nil || remarshal(raw, &in) != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	bad := func(msg string) { writeErr(w, http.StatusBadRequest, msg) }
	if keys["title"] && isAdmin {
		cur.Title = strings.TrimSpace(in.Title)
		if cur.Title == "" {
			cur.Title = store.DefaultTitle
		}
	}
	if keys["date_format"] {
		cur.DateFormat = in.DateFormat
	}
	if keys["time_format"] {
		cur.TimeFormat = in.TimeFormat
	}
	if keys["week_start"] && (in.WeekStart == 1 || in.WeekStart == 7 || in.WeekStart == 6) {
		cur.WeekStart = in.WeekStart
	}
	if keys["currency"] {
		if c := cleanCurrency(in.Currency); c != "" {
			cur.Currency = c
		} else {
			bad("currency is a three-letter code like EUR")
			return
		}
	}
	intIn := func(k string, dst *int, v, lo, hi int, msg string) bool {
		if !keys[k] {
			return true
		}
		if v < lo || v > hi {
			bad(msg)
			return false
		}
		*dst = v
		return true
	}
	if keys["notify_hour"] {
		if in.NotifyHour < 0 || in.NotifyHour > 23 {
			bad("the hour is 0 to 23")
			return
		}
		cur.NotifyHour = in.NotifyHour
	}
	if !intIn("auto_archive_days", &cur.AutoArchiveDays, in.AutoArchiveDays, 0, 365, "archive after 0 (never) to 365 days") ||
		!intIn("stale_days", &cur.StaleDays, in.StaleDays, 0, 90, "stuck after 0 (never) to 90 days") ||
		!intIn("mail_days", &cur.MailDays, in.MailDays, 1, 365, "look back 1 to 365 days") {
		return
	}
	secret := func(k string, dst *string, v string) {
		if keys[k] && !strings.Contains(v, "••") {
			*dst = strings.TrimSpace(v)
		}
	}
	url := func(k string, dst *string, v string) {
		if keys[k] {
			*dst = strings.TrimRight(strings.TrimSpace(v), "/")
		}
	}
	text := func(k string, dst *string, v string) {
		if keys[k] {
			*dst = strings.TrimSpace(v)
		}
	}
	boolIn := func(k string, dst *bool, v bool) {
		if keys[k] {
			*dst = v
		}
	}
	idIn := func(k string, dst *int64, v int64) {
		if keys[k] && v >= 0 {
			*dst = v
		}
	}
	trackWas, cainiaoWas := cur.Track17Key, cur.TrackCainiao
	boolIn("track_cainiao", &cur.TrackCainiao, in.TrackCainiao)
	secret("track17_key", &cur.Track17Key, in.Track17Key)
	boolIn("notify_moves", &cur.NotifyMoves, in.NotifyMoves)
	boolIn("notify_out", &cur.NotifyOut, in.NotifyOut)
	boolIn("notify_pickup", &cur.NotifyPickup, in.NotifyPickup)
	boolIn("notify_delivered", &cur.NotifyDelivered, in.NotifyDelivered)
	boolIn("notify_problems", &cur.NotifyProblems, in.NotifyProblems)
	boolIn("notify_returns", &cur.NotifyReturns, in.NotifyReturns)
	boolIn("notify_stale", &cur.NotifyStale, in.NotifyStale)
	text("notify_url", &cur.NotifyURL, in.NotifyURL)
	if keys["notify_format"] && in.NotifyFormat != "" {
		cur.NotifyFormat = in.NotifyFormat
	}
	boolIn("mail_scan", &cur.MailScan, in.MailScan)
	boolIn("mail_auto_add", &cur.MailAutoAdd, in.MailAutoAdd)
	idIn("money_account_id", &cur.MoneyAccountID, in.MoneyAccountID)
	idIn("money_category_id", &cur.MoneyCategoryID, in.MoneyCategoryID)
	boolIn("wishlist_bought", &cur.WishlistBought, in.WishlistBought)
	boolIn("inventory_auto", &cur.InventoryAuto, in.InventoryAuto)
	text("inventory_location", &cur.InventoryLocation, in.InventoryLocation)
	calWas, retWas := cur.CalendarSync, cur.CalendarReturns
	boolIn("calendar_sync", &cur.CalendarSync, in.CalendarSync)
	boolIn("calendar_returns", &cur.CalendarReturns, in.CalendarReturns)
	idIn("calendar_id", &cur.CalendarID, in.CalendarID)
	boolIn("tasks_pickup", &cur.TasksPickup, in.TasksPickup)
	boolIn("tasks_returns", &cur.TasksReturns, in.TasksReturns)
	// each Pocket app: mode, URL, token, public URL
	inMap := map[string]string{}
	for k, v := range raw {
		if sv, ok := v.(string); ok {
			inMap[k] = sv
		}
	}
	for _, a := range pocketApps {
		mode, u, tok, pub := connFields(&cur, a.slug)
		if v := inMap[a.slug+"_mode"]; keys[a.slug+"_mode"] && (v == modeCore || v == modeDirect || v == modeOff) {
			*mode = v
		}
		url(a.slug+"_url", u, inMap[a.slug+"_url"])
		secret(a.slug+"_token", tok, inMap[a.slug+"_token"])
		url(a.slug+"_public_url", pub, inMap[a.slug+"_public_url"])
	}
	if keys["ai_provider"] {
		if !validAIProvider(in.AIProvider) {
			bad("AI is automatic, Pocket AI or OpenAI")
			return
		}
		cur.AIProvider = in.AIProvider
	}
	if keys["ai_pocket_model"] {
		if !validPocketModel(in.AIPocketModel) {
			bad("unknown model")
			return
		}
		cur.AIPocketModel = in.AIPocketModel
	}
	secret("openai_api_key", &cur.OpenAIKey, in.OpenAIKey)
	text("openai_model", &cur.OpenAIModel, in.OpenAIModel)
	url("openai_base_url", &cur.OpenAIBaseURL, in.OpenAIBaseURL)
	if err := s.store.SaveUserSettings(r.Context(), me.ID, cur); err != nil {
		s.fail(w, err, "save settings")
		return
	}
	if isAdmin && keys["title"] {
		if err := s.store.SaveBoxSettings(r.Context(), cur); err != nil {
			s.fail(w, err, "save settings")
			return
		}
	}
	for _, k := range modeKeys {
		if keys[k] {
			s.reportModesSoon()
			break
		}
	}
	if cur.CalendarSync != calWas || cur.CalendarReturns != retWas || keys["calendar_id"] || keys["calendar_mode"] {
		s.syncCalendarSoon(me.ID)
	}
	if cur.Track17Key != trackWas || cur.TrackCainiao != cainiaoWas {
		s.refreshProviders(r.Context(), me.ID)
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), me, cur))
}

// handleIntegrationTest checks a connection with the caller's own saved
// settings (or the URL/key typed into the form).
func (s *Server) handleIntegrationTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Which  string `json:"which"`
		URL    string `json:"url"`
		Key    string `json:"key"`
		Format string `json:"format"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	uid := actorID(r.Context())
	st, _ := s.store.GetSettings(r.Context(), uid)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	keyOr := func(saved string) string {
		if strings.Contains(in.Key, "••") || in.Key == "" {
			return saved
		}
		return in.Key
	}
	urlOr := func(saved string) string {
		if strings.TrimSpace(in.URL) == "" {
			return saved
		}
		return strings.TrimRight(strings.TrimSpace(in.URL), "/")
	}
	var msg string
	var err error
	if a, ok := appBySlug(in.Which); ok {
		mode, savedURL, savedTok, _ := connSettings(st, a)
		typed := in.Key != "" && !strings.Contains(in.Key, "••")
		var conn *siblingConn
		if tok := keyOr(savedTok); tok != "" && (typed || s.connMode(mode, savedTok) == modeDirect) {
			base := urlOr(savedURL)
			if base == "" {
				base = s.resolveSibling(ctx, a.slug)
			}
			conn = &siblingConn{client: pocket.New(base, tok, 15*time.Second), via: modeDirect}
		} else {
			conn, err = s.clients(ctx, uid).conn(ctx, a, a.scopes...)
		}
		if err == nil {
			how := "direct connection"
			if conn.via == modeCore {
				how = "through Pocket Core"
			}
			var v string
			if v, err = conn.client.Health(ctx); err == nil {
				msg = fmt.Sprintf("%s %s, %s", a.name, v, how)
				switch a.slug {
				case "money":
					var accts []pocket.Account
					if accts, err = conn.client.Accounts(ctx); err == nil {
						msg += fmt.Sprintf(", %d accounts you can book on", len(accts))
					}
				case "calendar":
					var cals []pocket.Calendar
					if cals, err = conn.client.Calendars(ctx); err == nil {
						msg += fmt.Sprintf(", %d calendars you can write to", len(cals))
					}
				case "documents":
					_, err = conn.client.SearchDocuments(ctx, "", 1)
				case "inventory":
					_, err = conn.client.SearchItems(ctx, "", 1)
				case "contacts":
					_, err = conn.client.SearchContacts(ctx, "a", 1)
				case "mail":
					var ms []pocket.Message
					if ms, _, err = conn.client.Messages(ctx, "receipts", "", 20, 0); err == nil {
						msg += fmt.Sprintf(", %d recent receipts to look through", len(ms))
					}
				case "wishlist":
					var ws []pocket.Wish
					if ws, err = conn.client.Wishes(ctx, "", 200); err == nil {
						msg += fmt.Sprintf(", %d wishes", len(ws))
					}
				}
			}
		}
	} else if in.Which == "17track" {
		q, qerr := s.new17(keyOr(st.Track17Key)).Quota(ctx)
		if err = qerr; err == nil {
			msg = fmt.Sprintf("The key works: %d of %d tracking numbers left", q.Remain, q.Total)
		}
	} else if in.Which == "cainiao" {
		_, err = s.cainiao.Get(ctx, []string{"LP00000000000000"})
		if err == nil {
			msg = "Cainiao answers"
		}
	} else if in.Which == "openai" {
		c := ai.New(urlOr(st.OpenAIBaseURL), keyOr(st.OpenAIKey), st.OpenAIModel, 20*time.Second)
		var ids []string
		if ids, err = c.Models(ctx); err == nil {
			msg = fmt.Sprintf("The key works (%d models)", len(ids))
		}
	} else if in.Which == "notify" {
		f := in.Format
		if f == "" {
			f = st.NotifyFormat
		}
		n := notify.New(urlOr(st.NotifyURL), f)
		err = n.Send(ctx, notify.Message{Title: "Pocket Parcels", Body: "Notifications work. Parcels out for delivery, ready to pick up or delivered, and return deadlines will arrive here.", Priority: "info", Tags: []string{"package"}})
		msg = "Test message sent via " + n.Format
	} else {
		writeErr(w, http.StatusBadRequest, "unknown integration")
		return
	}
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}

// handleCheckNow runs the daily reminders for the caller now and says what it sent.
func (s *Server) handleCheckNow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	body, sent := s.dailyCheck(ctx, uid, st, time.Now())
	writeJSON(w, http.StatusOK, map[string]any{"sent": sent, "body": body})
}

// ---- system / backup / export ----

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	var size int64
	if fi, err := os.Stat(s.cfg.DBPath); err == nil {
		size = fi.Size()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version": Version, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH,
		"core":      s.coreStatus(),
		"auth_mode": s.cfg.AuthMode, "data_dir": s.cfg.DataDir, "db_path": s.cfg.DBPath, "db_bytes": size,
		"uptime": time.Since(s.started).Round(time.Second).String(), "timezone": time.Now().Location().String(),
		"counts": s.store.CountsFor(r.Context(), actorID(r.Context())),
	})
}

// handleBackup downloads the whole database, an admin-only box operation.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	dir := filepath.Join(s.cfg.DataDir, "backups")
	name := fmt.Sprintf("pocket-parcels-%s.db", time.Now().Format("20060102-150405"))
	dst := filepath.Join(dir, name)
	if err := s.store.Backup(r.Context(), dst); err != nil {
		s.fail(w, err, "backup")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, dst)
}

// exportDoc is the JSON export of one person's data.
type exportDoc struct {
	App         string             `json:"app"`
	Version     string             `json:"version"`
	Exported    string             `json:"exported_at"`
	Parcels     []store.Parcel     `json:"parcels"`
	Events      []store.Event      `json:"events"`
	Attachments []store.Attachment `json:"attachments"`
}

func (s *Server) export(ctx context.Context, uid int64) exportDoc {
	doc := exportDoc{App: "pocket-parcels", Version: Version, Exported: store.Now()}
	doc.Parcels, _ = s.store.AllParcels(ctx, uid)
	doc.Events, _ = s.store.AllEvents(ctx, uid)
	doc.Attachments, _ = s.store.ListAttachments(ctx, uid, 0)
	return doc
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-parcels-export.json"`)
	writeJSON(w, http.StatusOK, s.export(r.Context(), actorID(r.Context())))
}

// handleExportCSV writes one row per parcel.
func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ps, err := s.store.AllParcels(ctx, actorID(ctx))
	if err != nil {
		s.fail(w, err, "export")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-parcels.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"title", "tracking_number", "carrier", "direction", "shop", "order_number", "ordered_on", "status", "latest", "expected_on", "delivered_at", "days", "price", "extra_cost", "currency", "return_by", "return_state", "notes"})
	money := func(v *int64) string {
		if v == nil {
			return ""
		}
		return strconv.FormatFloat(float64(*v)/100, 'f', 2, 64)
	}
	for i := len(ps) - 1; i >= 0; i-- {
		p := ps[i]
		days := ""
		if p.Days != nil && p.Status == "delivered" {
			days = strconv.Itoa(*p.Days)
		}
		_ = cw.Write([]string{p.Title, p.TrackingNumber, firstNonEmpty(track.Name(p.Carrier), p.Carrier), p.Direction, p.Shop, p.OrderNumber, p.OrderedOn, p.Status, p.StatusText,
			p.ExpectedOn, p.DeliveredAt, days, money(p.Price), money(p.ExtraCost), p.Currency, p.ReturnBy, p.ReturnState, p.Notes})
	}
	cw.Flush()
}

// handleImportJSON merges an export into the caller's own data. Parcels
// keep their identity (the export's ids, remembered per person; else their
// tracking number), so importing the same file twice adds nothing. Links
// to the other apps are not carried over: they belong to the account the
// file came from.
func (s *Server) handleImportJSON(w http.ResponseWriter, r *http.Request) {
	var doc exportDoc
	if err := decode(r, &doc); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if doc.App != "" && doc.App != "pocket-parcels" {
		writeErr(w, http.StatusBadRequest, "this is not a Pocket Parcels export")
		return
	}
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	counts := map[string]int{"parcels": 0, "events": 0, "attachments": 0, "skipped": 0}
	idOf := func(old int64) (int64, bool) {
		v, _, ok := s.store.CacheGet(ctx, fmt.Sprintf("import:%d:parcel:%d", uid, old))
		if !ok {
			return 0, false
		}
		n, _ := strconv.ParseInt(v, 10, 64)
		if _, err := s.store.GetParcel(ctx, uid, n); err != nil {
			return 0, false
		}
		return n, true
	}
	ids := map[int64]int64{}
	for _, p := range doc.Parcels {
		old := p.ID
		if id, ok := idOf(old); ok {
			ids[old] = id
			continue
		}
		if p.TrackingNumber != "" {
			if have, err := s.store.ParcelByNumber(ctx, uid, track.Normalize(p.TrackingNumber)); err == nil {
				ids[old] = have.ID
				_ = s.store.CachePut(ctx, fmt.Sprintf("import:%d:parcel:%d", uid, old), strconv.FormatInt(have.ID, 10))
				continue
			}
		}
		np := p
		np.TrackingNumber = track.Normalize(p.TrackingNumber)
		if np.Carrier != "" {
			if _, ok := track.Lookup(np.Carrier); !ok {
				np.Carrier = ""
			}
		}
		if !track.ValidStatus(np.Status) {
			np.Status = "pending"
		}
		if np.Direction != "out" {
			np.Direction = "in"
		}
		np.Source, np.ExternalID = "import", fmt.Sprintf("import:%d", old)
		np.WishID, np.MoneyTxID, np.ExtraTxID, np.InventoryItemID, np.ContactID, np.TaskID, np.ReturnTaskID, np.MailMessageID = nil, nil, nil, nil, nil, nil, nil, nil
		np.CalEventID, np.ReturnEventID, np.CalSynced, np.ReturnSynced = nil, nil, "", ""
		np.ProviderRef, np.NextCheckAt, np.CheckError = "", "", ""
		np.Provider = s.providerFor(st, np)
		if have, err := s.store.ParcelByExternal(ctx, uid, "import", np.ExternalID); err == nil {
			ids[old] = have.ID
			continue
		}
		if err := s.store.CreateParcel(ctx, uid, &np); err != nil {
			counts["skipped"]++
			continue
		}
		ids[old] = np.ID
		_ = s.store.CachePut(ctx, fmt.Sprintf("import:%d:parcel:%d", uid, old), strconv.FormatInt(np.ID, 10))
		counts["parcels"]++
	}
	for _, e := range doc.Events {
		pid, ok := ids[e.ParcelID]
		if !ok || e.At == "" {
			continue
		}
		e.ParcelID = pid
		if e.Fingerprint == "" {
			e.Fingerprint = track.Event{At: e.At, Description: e.Description, Location: e.Location}.Fingerprint(e.Source)
		}
		if fresh, err := s.store.AddEvent(ctx, uid, &e); err == nil && fresh {
			counts["events"]++
		}
	}
	for _, a := range doc.Attachments {
		pid, ok := ids[a.ParcelID]
		if !ok {
			continue
		}
		key := fmt.Sprintf("import:%d:attachment:%d", uid, a.ID)
		if _, _, done := s.store.CacheGet(ctx, key); done {
			continue
		}
		a.ParcelID = pid
		if a.Kind != "document" && a.Kind != "url" {
			continue
		}
		if err := s.store.CreateAttachment(ctx, uid, &a); err == nil {
			_ = s.store.CachePut(ctx, key, strconv.FormatInt(a.ID, 10))
			counts["attachments"]++
		}
	}
	s.syncCalendarSoon(uid)
	writeJSON(w, http.StatusOK, counts)
}
