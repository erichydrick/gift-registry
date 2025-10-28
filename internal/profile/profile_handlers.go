package profile

import (
	"gift-registry/internal/util"
	"net/http"
)

// Looks up the person information and returns it.
func ProfileHandler(svr *util.ServerUtils) http.HandlerFunc {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		/*
			TODO:
			1. GET THE PERSON ID FROM THE REQUEST CONTEXT (I THINK I NEED TO ADD THE SESSION TO THE CONTEXT)
			2. LOOK UP THE PERSON DATA FROM THE TABLE (MAY WANT TO LOOK UP HOUSEHOLD INFO ONCE THAT'S ADDED?)
			3. POULATE AN OUTPUT STRUCT WITH THE PERSON DATA
			4. RETURN THE PROFILE PAGE DATA
		*/

	})

}

// Updates the person's information with the values provided from form input.
func ProfileUpdateHandler(svr *util.ServerUtils) http.HandlerFunc {

	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		/*
			TODO:
			1. GET THE FORM DATA INTO A STRUCT
			2. VALIDATE THE INPUT
				2A. INVALID => RETURN FORM WITH ERROR MESSAGE
			3. SAVE THE UPDATED DATA TO THE DATABASE
			4. RETURN....FORM DATA STRUCT (USE THE SAME STRUCT AS THE GET PROFILE?) ?
		*/

	})

}
