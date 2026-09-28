package api

import (
	"net/http"
	"time"

	"billforge/lab"
)

func (s *Server) recordUsage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Events []struct {
			TenantID       string    `json:"tenant_id"`
			SubscriptionID string    `json:"subscription_id"`
			Source         string    `json:"source"`
			EventID        string    `json:"event_id"`
			MeterID        string    `json:"meter_id"`
			Quantity       int64     `json:"quantity"`
			EventAt        time.Time `json:"event_at"`
		} `json:"events"`
	}
	if err := decode(r, &body); err != nil || len(body.Events) == 0 || len(body.Events) > 100 {
		fail(w, 400, "INVALID_REQUEST", "1 to 100 valid events required")
		return
	}
	state, err := s.lab.State(r.Context())
	if err != nil {
		domainError(w, err)
		return
	}
	customers := map[string]string{}
	for _, sub := range state.Subscriptions {
		customers[sub.ID] = sub.CustomerID
	}
	results := make([]map[string]any, 0, len(body.Events))
	for _, event := range body.Events {
		result := map[string]any{"event_id": event.EventID}
		if event.TenantID == "" || customers[event.SubscriptionID] != event.TenantID || event.Source == "" || event.EventID == "" || event.MeterID == "" || event.Quantity <= 0 || event.EventAt.IsZero() {
			result["status"] = "rejected"
			result["error"] = "INVALID_EVENT"
			results = append(results, result)
			continue
		}
		stored, err := s.lab.RecordUsage(r.Context(), event.Source, event.EventID, event.SubscriptionID, event.MeterID, event.Quantity, event.EventAt)
		if err != nil {
			result["status"] = "rejected"
			if err == lab.ErrConflict {
				result["error"] = "EVENT_CONFLICT"
			} else {
				result["error"] = "EVENT_REJECTED"
			}
		} else {
			result["status"] = "accepted"
			result["period_index"] = stored.PeriodIndex
			result["price_version_id"] = stored.PriceVersionID
		}
		results = append(results, result)
	}
	write(w, 200, map[string]any{"results": results})
}
