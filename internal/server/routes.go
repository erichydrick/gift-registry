package server

import (
	"gift-registry/internal/health"
	"gift-registry/internal/middleware"
	"gift-registry/internal/profile"
	"gift-registry/internal/registry"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// All HTTP routes go here so devs can get an overview of the application
func registerRoutes() (http.Handler, error) {

	mux := http.NewServeMux()

	handleFunc := func(pattern string, appHandler http.Handler) {

		handler := otelhttp.WithRouteTag(pattern, appHandler)
		mux.Handle(pattern, handler)

	}

	/* Static files */
	handleFunc("/css/", http.StripPrefix("/css/", http.FileServer(http.Dir(appSrv.Getenv("STATIC_FILES_DIR")+"/css"))))
	handleFunc("/js/", http.StripPrefix("/js/", http.FileServer(http.Dir(appSrv.Getenv("STATIC_FILES_DIR")+"/js"))))

	/* Base routes */
	handleFunc("GET /{$}", IndexHandler(appSrv))
	handleFunc("GET /health", health.HealthCheckHandler(appSrv))

	/* Authentication routes */
	handleFunc("GET /login", LoginFormHandler(appSrv))
	handleFunc("POST /login", LoginHandler(appSrv))
	handleFunc("GET /logout", LogoutHandler(appSrv))
	handleFunc("POST /verify", VerificationHandler(appSrv))

	/* Profile routes */
	handleFunc("GET /profile", profile.ProfileHandler(appSrv))
	handleFunc("POST /profile/{externalID}", profile.ProfileUpdateHandler(appSrv))

	/* Registry routes */
	handleFunc("GET /registry", registry.RegistryHandler(appSrv))

	handler := otelhttp.NewHandler(
		middleware.Cors(
			appSrv,
			middleware.Auth(appSrv,
				middleware.Telemetry(appSrv, mux),
			),
		),
		"/",
	)
	appSrv.Logger.Info("Registered all routes")
	return handler, nil

}

/*
TODO: MIDDLEWARES NEEDED:
2. RATE LIMITING (TO DEAL WITH SCRIPTS TRYING TO BRUTE FORCE CONF CODES)
*/
