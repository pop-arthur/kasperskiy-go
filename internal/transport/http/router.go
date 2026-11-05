package httptransport

import "net/http"

func (h *handler) router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /info", h.create)
	mux.HandleFunc("GET /info/{md5}", h.get)
	mux.HandleFunc("PUT /info/{md5}", h.update)
	// GET-шаблон ServeMux также принимает HEAD; явный маршрут сохраняет контракт 405.
	mux.HandleFunc("HEAD /info/{md5}", methodNotAllowed("GET, PUT"))
	mux.HandleFunc("/info", methodNotAllowed("POST"))
	mux.HandleFunc("/info/{md5}", methodNotAllowed("GET, PUT"))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusNotFound, ErrorResponse{Message: "Route not found"})
	})
	return mux
}

func methodNotAllowed(allow string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", allow)
		respond(w, http.StatusMethodNotAllowed, ErrorResponse{Message: "Method not allowed"})
	}
}
