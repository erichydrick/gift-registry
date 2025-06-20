package registry

import (
	"gift-registry/internal/util"
	"net/http"
)

// Returns the registry items, grouped by person
func RegistryHandler(svr *util.ServerUtils) http.Handler {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		res.WriteHeader(200)
		res.Write([]byte("<p id=\"registry-data\">Success!</p>"))

	})

}
