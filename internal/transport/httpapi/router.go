package httpapi

import "net/http"

// NewRouter registers the application's HTTP endpoints.
func NewRouter(gigaChat chatService) http.Handler {
	mux := http.NewServeMux()
	// Other handlers are retained but temporarily not exposed, including healthz.
	mux.HandleFunc("POST /api/v1/alice/webhook", aliceHandler{service: gigaChat, timeout: aliceTimeout}.webhook)
	return mux
}
