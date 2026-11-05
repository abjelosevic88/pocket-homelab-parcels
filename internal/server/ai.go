package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-parcels/internal/ai"
	"github.com/abjelosevic88/pocket-homelab-parcels/internal/store"
	"github.com/abjelosevic88/pocket-homelab-parcels/internal/track"
)

// aiFor is the person's AI: Pocket AI through Core, their own
// OpenAI-compatible key, or nil with why.
func (s *Server) aiFor(ctx context.Context, uid int64, st store.Settings) (*ai.Client, string, string) {
	email := ""
	if u, err := s.store.GetUser(ctx, uid); err == nil {
		email = u.Email
	}
	pocket, status := s.core.SelectAI(ctx, email, st.AIProvider, st.OpenAIKey != "")
	if pocket != nil {
		if !status.Ready {
			return nil, "", "Pocket AI: " + status.Detail
		}
		c := ai.New("", "", "", 150*time.Second)
		c.Pocket = pocket.WithModel(st.AIPocketModel)
		return c, "pocket", ""
	}
	if st.OpenAIKey == "" {
		return nil, "", status.Detail
	}
	return ai.New(st.OpenAIBaseURL, st.OpenAIKey, st.OpenAIModel, 150*time.Second), "openai", ""
}

func (s *Server) handleAIStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	cl, used, why := s.aiFor(ctx, actorID(ctx), st)
	writeJSON(w, http.StatusOK, map[string]any{"ready": cl != nil, "provider": used, "detail": why})
}

func (s *Server) handleAIModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, status := s.core.SelectAI(ctx, userFrom(ctx).Email, "pocket", false)
	if c == nil || !status.Ready {
		writeErr(w, http.StatusServiceUnavailable, status.Detail)
		return
	}
	models, err := c.Models(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models)
}

func validAIProvider(v string) bool { return core.ValidAIProvider(v) }

func validPocketModel(v string) bool { return core.ValidInferenceModel(v) }

const extractVoice = `You read one email and say which parcels it is about. Answer JSON only:
{"parcels":[{"tracking_number":"","carrier":"","shop":"","order_number":"","item":"","status":"","expected_on":""}]}
- tracking_number: exactly as written in the email (never invent one, never an order number, phone or customer number); leave the list empty when there is none.
- carrier: the delivery company named (DHL, UPS, Pošta, Cainiao…), or "".
- shop: the shop that sent the order, or "".
- item: what was ordered, in a few words, or "".
- status: one of shipped, out_for_delivery, pickup, delivered, problem, or "" when the email does not say.
- expected_on: the expected delivery day as YYYY-MM-DD when the email gives one, else "".`

// mailText is the email the AI reads.
type mailText struct {
	Subject, From, Text string
}

// extractWithAI asks the person's AI which parcels an email names. Numbers
// that do not appear in the email are dropped.
func (s *Server) extractWithAI(ctx context.Context, uid int64, e mailText) ([]track.Shipment, error) {
	st, _ := s.store.GetSettings(ctx, uid)
	c, _, why := s.aiFor(ctx, uid, st)
	if c == nil {
		return nil, &notConnected{"AI: " + firstNonEmpty(why, "not set up")}
	}
	text := e.Text
	if len(text) > 6000 {
		text = text[:6000]
	}
	var out struct {
		Parcels []struct {
			Number   string `json:"tracking_number"`
			Carrier  string `json:"carrier"`
			Shop     string `json:"shop"`
			Order    string `json:"order_number"`
			Item     string `json:"item"`
			Status   string `json:"status"`
			Expected string `json:"expected_on"`
		} `json:"parcels"`
	}
	if err := c.JSON(ctx, extractVoice, fmt.Sprintf("From: %s\nSubject: %s\n\n%s", e.From, e.Subject, text), &out); err != nil {
		return nil, err
	}
	flat := track.Normalize(e.Subject + " " + e.Text)
	var res []track.Shipment
	for _, p := range out.Parcels {
		n := track.Normalize(p.Number)
		if !track.ValidNumber(n) || !strings.Contains(flat, n) {
			continue
		}
		sh := track.Shipment{Number: n, Carrier: track.Detect(n), Shop: strings.TrimSpace(p.Shop), Order: strings.TrimSpace(p.Order), Title: strings.TrimSpace(p.Item)}
		switch p.Status {
		case "shipped":
			sh.Status = track.StatusInTransit
		case "out_for_delivery":
			sh.Status = track.StatusOutForDelivery
		case "pickup":
			sh.Status = track.StatusPickup
		case "delivered":
			sh.Status = track.StatusDelivered
		case "problem":
			sh.Status = track.StatusException
		}
		res = append(res, sh)
	}
	return res, nil
}
