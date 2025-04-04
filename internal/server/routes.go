package server

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
)

const (
	name = "net.hydrick.gift-registry/server"
)

var (
	log    *slog.Logger
	tracer = otel.Tracer(name)
)

// All HTTP routes go here so devs can get an overview of the application
func (svr *server) registerRoutes(db *sql.DB, logger *slog.Logger) (http.Handler, error) {

	/*
	   I'm using a vertical slice architecture, so the handler logic will be
	   split amongst several different packages. They'll all need to be
	   initialized before registering, so do that here.
	*/
	log = logger
	mux := http.NewServeMux()

	handleFunc := func(pattern string, appHandler http.Handler) {

		handler := otelhttp.WithRouteTag(pattern, appHandler)
		mux.Handle(pattern, handler)

	}

	handleFunc("/css/", http.StripPrefix("/css/", http.FileServer(http.Dir("css"))))
	handleFunc("GET /health", HealthCheckHandler(svr.templateDir, db, log))

	/* TODO: PASSING THE LOGGER TWICE SEEMS STUPID, CAN I CLEAN THAT UP? */
	handler := otelhttp.NewHandler(telemetry(cors(mux, log), log), "/")
	log.Info("Registered all routes")
	return handler, nil

}

/* Sets the CORS response for all endpoints */
func cors(next http.Handler, logger *slog.Logger) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		logger.Info("Processing CORS", slog.String("requestURL", req.URL.String()), slog.String("pattern", req.Pattern))
		res.Header().Set("Access-Control-Allow-Origin", os.Getenv("ALLOWED_HOSTS"))
		res.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
		res.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token")
		/* TODO: CHANGE TO TRUE WHEN CREDENTIALS ARE FIGURED OUT */
		res.Header().Set("Access-Control-Allow-Credentials", "false")

		if req.Method == http.MethodOptions {

			res.WriteHeader(http.StatusNoContent)
			return

		}

		logger.Debug(fmt.Sprintf("Now calling the handler for %s", req.URL.Path))
		next.ServeHTTP(res, req)

	})

}

func telemetry(next http.Handler, logger *slog.Logger) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		/* TODO: SET UP TRACE HERE - DOES DOING THIS HERE DO ANY GOOD? */
		ctx, span := tracer.Start(req.Context(), req.URL.Path)
		defer span.End()
		next.ServeHTTP(res, req)
		/* TODO: EMIT CANONICAL LOG LINE HERE */
		/* TODO: MAKE SURE ATTRIBUTES ARE CAPTURED HERE TOO */
		logger.InfoContext(ctx, fmt.Sprintf("Finished the %s operation", req.URL.Path))

	})

}
