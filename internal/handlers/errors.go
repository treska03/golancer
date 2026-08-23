package handlers

import (
	"fmt"
	"net/http"
)

func ErrorResponse(w http.ResponseWriter, err error, code int) {
	msg := fmt.Sprintf("Error: %v", err)
	http.Error(w, msg, code)
}