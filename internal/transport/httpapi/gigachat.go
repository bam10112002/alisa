package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"

	"alisaai/internal/integrations/gigachat"
	"alisaai/internal/integrations/ticktick"
)

type chatService interface {
	Chat(context.Context, string) (string, error)
}

type gigaChatHandler struct{ service chatService }

func (h gigaChatHandler) chat(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "Content-Type must be application/json"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	defer r.Body.Close()
	var input struct {
		Message string `json:"message"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&input)
	if err == nil {
		var extra any
		if trailing := decoder.Decode(&extra); trailing != io.EOF {
			err = trailing
			if err == nil {
				err = errors.New("multiple JSON values")
			}
		}
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "Request body exceeds 64 KiB"})
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Expected a JSON object with a message string"})
		}
		return
	}
	if strings.TrimSpace(input.Message) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message must not be empty"})
		return
	}
	if h.service == nil {
		writeGigaChatError(w, gigachat.ErrNotConfigured)
		return
	}
	answer, err := h.service.Chat(r.Context(), input.Message)
	if err != nil {
		writeGigaChatError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"answer": answer})
}

func writeGigaChatError(w http.ResponseWriter, err error) {
	status, message := http.StatusBadGateway, "GigaChat request failed"
	var apiError *gigachat.APIError
	var tickTickError *ticktick.APIError
	var netError net.Error
	switch {
	case errors.Is(err, gigachat.ErrNotConfigured):
		status, message = http.StatusServiceUnavailable, "GigaChat integration is not configured"
	case errors.Is(err, ticktick.ErrNotConfigured), errors.As(err, &tickTickError):
		writeTickTickError(w, err)
		return
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netError) && netError.Timeout():
		status, message = http.StatusGatewayTimeout, "GigaChat request timed out"
	case errors.As(err, &apiError):
		switch apiError.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			message = "GigaChat rejected the configured credentials"
		case http.StatusTooManyRequests:
			status, message = http.StatusServiceUnavailable, "GigaChat rate limit reached; try again later"
		}
	}
	writeJSON(w, status, map[string]string{"error": message})
}
