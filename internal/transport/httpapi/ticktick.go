package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"

	"alisaai/internal/integrations/ticktick"
	"alisaai/internal/service"
)

type tickTickHandler struct {
	service *service.TickTick
}

func (h tickTickHandler) inboxTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := h.service.ListInboxTasks(r.Context())
	if err != nil {
		writeTickTickError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (h tickTickHandler) projects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.service.ListProjects(r.Context())
	if err != nil {
		writeTickTickError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, projects)
}

func (h tickTickHandler) tasks(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.URL.Query().Get("project_id"))
	tasks, err := h.service.ListTasks(r.Context(), projectID)
	if err != nil {
		writeTickTickError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

func writeTickTickError(w http.ResponseWriter, err error) {
	status, message := http.StatusBadGateway, "TickTick request failed"
	var apiError *ticktick.APIError
	var netError net.Error
	switch {
	case errors.Is(err, ticktick.ErrNotConfigured):
		status, message = http.StatusServiceUnavailable, "TickTick integration is not configured"
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netError) && netError.Timeout():
		status, message = http.StatusGatewayTimeout, "TickTick request timed out"
	case errors.As(err, &apiError):
		switch apiError.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			message = "TickTick rejected the configured credentials"
		case http.StatusNotFound:
			status, message = http.StatusNotFound, "TickTick project not found"
		case http.StatusTooManyRequests:
			status, message = http.StatusServiceUnavailable, "TickTick rate limit reached; try again later"
		}
	}
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
