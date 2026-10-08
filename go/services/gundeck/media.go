package gundeck

import (
	"mime"
	"net/http"
)

func jsonMediaType(r *http.Request) bool {
	value, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && value == "application/json"
}
