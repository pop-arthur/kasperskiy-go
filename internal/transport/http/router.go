package httptransport

import "net/http"

func (h *handler) router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			respond(w, http.StatusMethodNotAllowed, errorResponse{Message: "Method not allowed"})
			return
		}
		h.create(w, r)
	})
	mux.HandleFunc("/info/{md5}", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.get(w, r)
		case http.MethodPut:
			h.update(w, r)
		default:
			w.Header().Set("Allow", "GET, PUT")
			respond(w, http.StatusMethodNotAllowed, errorResponse{Message: "Method not allowed"})
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusNotFound, errorResponse{Message: "Route not found"})
	})
	return mux
}
