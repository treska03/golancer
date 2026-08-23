package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/treska03/golancer/internal/backend"
	"github.com/treska03/golancer/internal/domain"
)

type RegisterBackendRequest struct {
	URL string `json:"url"`
}

func ListBackendsHandler(reg *backend.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		backends := reg.ListBackends()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string][]*domain.Backend{"backends": backends})
	}
}

func RegisterBackendHandler(reg *backend.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := getRegisteredURL(r)
		if err != nil {
			ErrorResponse(w, err, http.StatusBadRequest)
			return
		}
		id, err := reg.AddNewBackend(u)
		if err != nil {
			ErrorResponse(w, err, http.StatusConflict)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"instanceID": id})
	}
}

func DeregisterBackendHandler(reg *backend.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := r.PathValue("instanceID")
		if target == "" {
			ErrorResponse(w, errors.New("instanceID must be provided"), http.StatusBadRequest)
			return
		}
		if ok := reg.RemoveInstanceByID(target); !ok {
			ErrorResponse(w, fmt.Errorf("backend with instanceID %s not found", target), http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

func getRegisteredURL(r *http.Request) (*url.URL, error) {
	defer r.Body.Close()

	var req RegisterBackendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}

	// Parse the string into a *url.URL struct. url.Parse accepts relative
	// references (e.g. "foo") without error, so we must additionally require an
	// absolute URL with both a scheme and a host.
	parsedURL, err := url.Parse(req.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid url %q: %w", req.URL, err)
	}
	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return nil, fmt.Errorf("url %q must include a scheme and host", req.URL)
	}

	return parsedURL, nil
}
