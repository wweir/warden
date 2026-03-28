package gateway

import "net/http"

func writeModelSelectionError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusBadRequest)
}
