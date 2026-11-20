package httpapi

import (
	"net/http"

	"alisaai/internal/service"
)

// Exercise retained handlers without exposing them in the production router.
func legacyTestRouter(tickTick *service.TickTick, chat chatService) http.Handler {
	mux := http.NewServeMux()
	handler := tickTickHandler{service: tickTick}
	mux.HandleFunc("GET /api/v1/ticktick/projects", handler.projects)
	mux.HandleFunc("GET /api/v1/ticktick/tasks", handler.tasks)
	mux.HandleFunc("GET /api/v1/ticktick/inbox/tasks", handler.inboxTasks)
	mux.HandleFunc("POST /api/v1/gigachat/chat", gigaChatHandler{service: chat}.chat)
	return mux
}
