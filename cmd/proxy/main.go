package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
)

func main() {
	target, _ := url.Parse("http://localhost:2115")
	proxy := httputil.NewSingleHostReverseProxy(target)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-Forwarded-Host", r.Header.Get("Host"))
		proxy.ServeHTTP(w, r)
	})

	log.Fatal(http.ListenAndServe("127.0.0.1:8080", nil))
}
