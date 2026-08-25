package middleware

import (
	"fmt"
	"gift-registry/internal/util"
	"log/slog"
	"net/http"
)

/* Sets the CORS response for all endpoints */
func Cors(svr *util.ServerUtils, next http.Handler) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		svr.Logger.InfoContext(req.Context(), "Processing CORS", slog.String("requestURL", req.URL.String()), slog.String("pattern", req.Pattern))
		res.Header().Set("Access-Control-Allow-Origin", svr.Getenv("ALLOWED_HOSTS"))
		/* I'll add more methods as I need them, but this is what I'm using for now */
		res.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		res.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token")

		if req.Method == http.MethodOptions {

			res.WriteHeader(http.StatusNoContent)
			return

		}

		svr.Logger.DebugContext(req.Context(), fmt.Sprintf("Now calling the %s handler for %s", req.Method, req.URL.Path))
		next.ServeHTTP(res, req)

	})

}
