package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-parcels/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-parcels/internal/store"
)

// pushes runs every minute; once a day, at each person's own hour, it sends
// one message with the return deadlines coming up (three days before, the
// day before and on the day) and the parcels nobody has heard of for a
// while. Status pushes (out for delivery, delivered…) go out at once from
// the tracker.
func (s *Server) pushes(ctx context.Context) {
	ids, err := s.store.SettingsUsers(ctx)
	if err != nil {
		return
	}
	now := time.Now()
	for _, uid := range ids {
		st, err := s.store.GetSettings(ctx, uid)
		if err != nil || now.Hour() != st.NotifyHour || now.Minute() > 10 {
			continue
		}
		key := fmt.Sprintf("push:%d:%s", uid, store.Today())
		if _, _, done := s.store.CacheGet(ctx, key); done {
			continue
		}
		_ = s.store.CachePut(ctx, key, "1")
		s.dailyCheck(ctx, uid, st, now)
	}
}

// dailyCheck builds and sends one person's daily message; it also
// publishes parcels.return_due, notifications on or off.
func (s *Server) dailyCheck(ctx context.Context, uid int64, st store.Settings, now time.Time) (string, bool) {
	today := now.Format("2006-01-02")
	var lines []string
	worst := "info"
	returns, _, _ := s.store.ListParcels(ctx, uid, store.ParcelFilter{View: "returns"})
	for _, p := range returns {
		left := store.DaysBetween(today, p.ReturnBy)
		if (left != 3 && left != 1 && left != 0) || p.NotifiedOn == today {
			continue
		}
		_ = s.store.SetNotified(ctx, uid, p.ID, "", today)
		s.publish(ctx, uid, "parcels.return_due", parcelSubject(p.ID), map[string]any{"parcel_id": p.ID, "title": p.Title, "shop": p.Shop,
			"return_by": p.ReturnBy, "days_left": left, "url": s.parcelURL(p.ID)})
		if !st.NotifyReturns {
			continue
		}
		switch left {
		case 0:
			lines = append(lines, "• Today is the last day to send back "+p.Title)
			worst = "warning"
		case 1:
			lines = append(lines, "• Tomorrow is the last day to send back "+p.Title)
		default:
			lines = append(lines, fmt.Sprintf("• %d days left to send back %s (until %s)", left, p.Title, p.ReturnBy))
		}
	}
	if st.NotifyStale && st.StaleDays > 0 {
		active, _, _ := s.store.ListParcels(ctx, uid, store.ParcelFilter{View: "active"})
		var stuck []string
		for _, p := range active {
			v := s.view(p, st, now)
			if !v.Stale || (p.NotifiedOn != "" && store.DaysBetween(p.NotifiedOn, today) < 7) {
				continue
			}
			_ = s.store.SetNotified(ctx, uid, p.ID, "", today)
			stuck = append(stuck, p.Title)
		}
		if len(stuck) > 0 {
			lines = append(lines, fmt.Sprintf("• No news for %d days or more: %s", st.StaleDays, strings.Join(stuck, ", ")))
		}
	}
	body := strings.Join(lines, "\n")
	if len(lines) == 0 || st.NotifyURL == "" {
		return body, false
	}
	n := notify.New(st.NotifyURL, st.NotifyFormat)
	if err := n.Send(ctx, notify.Message{Title: "Your parcels today", Body: body, Priority: worst, Tags: []string{"package"}, Link: s.cfg.BaseURL + "/"}); err != nil {
		s.log.Warn("notify", "user", uid, "err", err)
		return body, false
	}
	return body, true
}

func (s *Server) handleNotifyTest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	n := notify.New(st.NotifyURL, st.NotifyFormat)
	if err := n.Send(ctx, notify.Message{Title: "Pocket Parcels", Body: "Notifications work. Parcels out for delivery, ready to pick up or delivered, and return deadlines will arrive here.", Priority: "info", Tags: []string{"package"}}); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
