package admin

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
	"unicode/utf8"

	"billforge/lab"
)

func safeValue(value any) (any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var safe any
	if err := decoder.Decode(&safe); err != nil {
		return nil, err
	}
	return stringNumbers(safe), nil
}

type resourcePageCursor struct {
	After int64  `json:"after"`
	Scope string `json:"scope"`
}

func resourceFilterScope(resource string, filters map[string]string) string {
	values := url.Values{}
	for key, value := range filters {
		values.Set(key, value)
	}
	sum := sha256.Sum256([]byte(resource + "\x00" + values.Encode()))
	return fmt.Sprintf("%x", sum)
}

func redactFinancialResourceKeys(resource string, page []map[string]any, capabilities []string) {
	for _, capability := range capabilities {
		if capability == "finance.adjust" {
			return
		}
	}
	for _, item := range page {
		switch resource {
		case "payments":
			delete(item, "ProviderKey")
		case "refunds":
			delete(item, "ProviderKey")
			delete(item, "SourceProviderKey")
			delete(item, "RequestKey")
		}
	}
}

func (s *Server) listResource(w http.ResponseWriter, r *http.Request) {
	resource := r.PathValue("resource")
	filters := make(map[string]string)
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		apiError(w, http.StatusBadRequest, "INVALID_FILTER", "Malformed list parameter")
		return
	}
	for key, values := range query {
		if len(values) != 1 {
			apiError(w, http.StatusBadRequest, "INVALID_FILTER", "List parameters must have one value")
			return
		}
		if key != "limit" && key != "cursor" {
			filters[key] = values[0]
		}
	}
	scope := resourceFilterScope(resource, filters)
	limit := 50
	if input := r.URL.Query().Get("limit"); input != "" {
		parsed, err := strconv.Atoi(input)
		if err != nil || parsed < 1 || parsed > 100 {
			apiError(w, http.StatusBadRequest, "INVALID_LIMIT", "Limit must be 1–100")
			return
		}
		limit = parsed
	}
	var after int64
	if input := r.URL.Query().Get("cursor"); input != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(input)
		var cursor resourcePageCursor
		if err == nil {
			err = json.Unmarshal(decoded, &cursor)
		}
		if err != nil || cursor.After < 1 || cursor.Scope != scope {
			apiError(w, http.StatusBadRequest, "INVALID_CURSOR", "Invalid list cursor")
			return
		}
		after = cursor.After
	}
	page, pageTotal, nextRowID, pageErr := s.lab.AdminFilteredResourcePage(r.Context(), resource, after, limit, filters)
	if pageErr == nil {
		if resource == "payments" || resource == "refunds" {
			item, _ := s.lookupSession(r)
			redactFinancialResourceKeys(resource, page, item.capabilities)
		}
		safe, err := safeValue(page)
		if err != nil {
			apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode resource")
			return
		}
		next := ""
		if nextRowID > 0 {
			encoded, err := json.Marshal(resourcePageCursor{After: nextRowID, Scope: scope})
			if err != nil {
				apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode cursor")
				return
			}
			next = base64.RawURLEncoding.EncodeToString(encoded)
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": safe, "next_cursor": next, "total": pageTotal, "observed_at": time.Now().UTC().Format(time.RFC3339Nano)})
		return
	}
	if errors.Is(pageErr, lab.ErrAdminInvalidCommand) {
		apiError(w, http.StatusBadRequest, "INVALID_FILTER", "Filter is not supported for this resource")
		return
	}
	if errors.Is(pageErr, lab.ErrAdminUnsupportedAction) {
		apiError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found")
		return
	}
	apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load resource")
}

func (s *Server) quoteDetail(w http.ResponseWriter, r *http.Request) {
	quote, err := s.lab.AdminQuoteDetail(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, http.StatusNotFound, "NOT_FOUND", "Quote not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load quote")
		return
	}
	safe, err := safeValue(quote)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode quote")
		return
	}
	writeJSON(w, http.StatusOK, safe)
}

func (s *Server) listCustomers(w http.ResponseWriter, r *http.Request) {
	if !validateQueryKeys(w, r, "INVALID_FILTER", "limit", "cursor") {
		return
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			apiError(w, http.StatusBadRequest, "INVALID_LIMIT", "Limit must be 1–100")
			return
		}
		limit = parsed
	}
	after := ""
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil || len(decoded) == 0 || !utf8.Valid(decoded) {
			apiError(w, http.StatusBadRequest, "INVALID_CURSOR", "Invalid customer cursor")
			return
		}
		after = string(decoded)
	}
	items, total, next, err := s.lab.AdminCustomerPage(r.Context(), after, limit)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load customers")
		return
	}
	safe, err := safeValue(items)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode customers")
		return
	}
	nextCursor := ""
	if next != "" {
		nextCursor = base64.RawURLEncoding.EncodeToString([]byte(next))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": safe, "next_cursor": nextCursor, "total": total, "observed_at": time.Now().UTC().Format(time.RFC3339Nano)})
}

func (s *Server) customerDetail(w http.ResponseWriter, r *http.Request) {
	detail, err := s.lab.AdminCustomerDetail(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, http.StatusNotFound, "NOT_FOUND", "Customer not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load customer")
		return
	}
	safe, err := safeValue(detail)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode customer")
		return
	}
	writeJSON(w, http.StatusOK, safe)
}

func (s *Server) subscriptionDetail(w http.ResponseWriter, r *http.Request) {
	detail, err := s.lab.AdminSubscriptionDetail(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, http.StatusNotFound, "NOT_FOUND", "Subscription not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load subscription")
		return
	}
	safe, err := safeValue(detail)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode subscription")
		return
	}
	writeJSON(w, http.StatusOK, safe)
}

func (s *Server) migrationDetail(w http.ResponseWriter, r *http.Request) {
	migration, err := s.lab.PriceMigration(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, http.StatusNotFound, "NOT_FOUND", "Migration not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load migration")
		return
	}
	safe, err := safeValue(migration)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode migration")
		return
	}
	writeJSON(w, http.StatusOK, safe)
}
