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
	"time"

	"alisaai/internal/service"
)

// Alice allows 4.5 seconds including network transit. Leave time for the tunnel.
const aliceTimeout = 3500 * time.Millisecond
const aliceTextLimit = 1024
const aliceWelcome = "Здравствуйте! Я помогу с задачами из входящих ТикТик. Скажите: какие у меня задачи во входящих?"
const aliceUnavailable = "Сейчас не получилось получить ответ. Попробуйте ещё раз чуть позже."
const aliceTimedOut = "Не успел получить ответ. Попробуйте ещё раз."

type aliceRequest struct {
	Version string `json:"version"`
	Meta    struct {
		TimeZone string `json:"timezone"`
	} `json:"meta"`
	Session *struct {
		SessionID string `json:"session_id"`
		SkillID   string `json:"skill_id"`
		New       bool   `json:"new"`
	} `json:"session"`
	Request *struct {
		Type              string `json:"type"`
		Command           string `json:"command"`
		OriginalUtterance string `json:"original_utterance"`
	} `json:"request"`
}

type aliceResponse struct {
	Version  string `json:"version"`
	Response struct {
		Text       string `json:"text"`
		EndSession bool   `json:"end_session"`
	} `json:"response"`
}

type aliceHandler struct {
	service chatService
	timeout time.Duration
}

func (h aliceHandler) webhook(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "Content-Type must be application/json"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	defer r.Body.Close()
	var input aliceRequest
	decoder := json.NewDecoder(r.Body)
	// Alice may add fields; ignore NLU and state that this handler does not use.
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
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid Alice request JSON"})
		}
		return
	}
	if input.Version != "1.0" || input.Request == nil || input.Request.Type == "" || input.Session == nil || strings.TrimSpace(input.Session.SessionID) == "" || strings.TrimSpace(input.Session.SkillID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Expected Alice protocol 1.0 with request and session"})
		return
	}
	if input.Request.Type != "SimpleUtterance" {
		writeAliceResponse(w, "Пожалуйста, задайте вопрос голосом или текстом.", false)
		return
	}
	command := strings.TrimSpace(input.Request.Command)
	if strings.EqualFold(strings.TrimSpace(input.Request.OriginalUtterance), "ping") || strings.EqualFold(command, "ping") {
		writeAliceResponse(w, "Навык работает.", false)
		return
	}
	// An activation phrase may appear in original_utterance; do not send it to the model.
	if command == "" && input.Session.New {
		writeAliceResponse(w, aliceWelcome, false)
		return
	}
	if command == "" {
		command = strings.TrimSpace(input.Request.OriginalUtterance)
	}
	switch strings.ToLower(command) {
	case "", "помощь", "что ты умеешь":
		writeAliceResponse(w, aliceWelcome, false)
		return
	case "выход", "выйти", "заверши навык", "хватит":
		writeAliceResponse(w, "До встречи!", true)
		return
	}
	if h.service == nil {
		writeAliceResponse(w, aliceUnavailable, false)
		return
	}
	answer, err := h.service.Chat(service.WithUserTimeZone(ctx, input.Meta.TimeZone), command)
	// Also reject late successful answers if an implementation ignores cancellation.
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		var netError net.Error
		if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &netError) && netError.Timeout() {
			writeAliceResponse(w, aliceTimedOut, false)
		} else {
			writeAliceResponse(w, aliceUnavailable, false)
		}
		return
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		answer = aliceUnavailable
	}
	writeAliceResponse(w, answer, false)
}

func writeAliceResponse(w http.ResponseWriter, text string, endSession bool) {
	characters := []rune(text)
	if len(characters) > aliceTextLimit {
		text = string(characters[:aliceTextLimit-1]) + "…"
	}
	output := aliceResponse{Version: "1.0"}
	output.Response.Text, output.Response.EndSession = text, endSession
	writeJSON(w, http.StatusOK, output)
}
