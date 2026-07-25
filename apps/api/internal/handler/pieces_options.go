package handler

// pieces_options.go proxies dynamic-prop options lookups to the pieces
// worker's HTTP resolver (apps/pieces-worker/src/options-server.ts), which
// invokes the piece prop's real options() with the node's credential. The
// worker being down is a business outcome for the builder UI (it falls back
// to By-ID entry), so it maps to {ok:false} — never a 5xx.
//
//	POST /api/pieces/options  → forwarded verbatim; response passed through.

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/agently/api/internal/services"
)

const piecesOptionsMaxBody = 1 << 20

func piecesOptions(_ *services.Platform) http.HandlerFunc {
	client := &http.Client{Timeout: 12 * time.Second}
	return func(w http.ResponseWriter, r *http.Request) {
		base := os.Getenv("PIECES_WORKER_URL")
		if base == "" {
			base = "http://localhost:7391"
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, piecesOptionsMaxBody))
		if err != nil {
			writeOptionsError(w, "unreadable request body", "BadRequest")
			return
		}
		resp, err := client.Post(base+"/options", "application/json", bytes.NewReader(body))
		if err != nil {
			writeOptionsError(w, "pieces worker unavailable", "OptionsUnavailable")
			return
		}
		defer resp.Body.Close()
		out, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if err != nil {
			writeOptionsError(w, "pieces worker response unreadable", "OptionsUnavailable")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(out)
	}
}

func writeOptionsError(w http.ResponseWriter, msg, errType string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":false,"error":"` + msg + `","errorType":"` + errType + `"}`))
}
