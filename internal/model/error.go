package model

import (
	"fmt"
	"net/http"
)

// HTTPError carries an HTTP status code along with the error message. The
// route handlers render it in whichever format the endpoint speaks (JSON, XML
// or plain text), so the status travels with the error rather than being
// re-derived at each call site.
type HTTPError struct {
	Status  int
	Message string
}

func (e *HTTPError) Error() string { return e.Message }

// BadRequest builds a 400 error with a formatted message.
func BadRequest(format string, args ...interface{}) *HTTPError {
	return &HTTPError{Status: http.StatusBadRequest, Message: fmt.Sprintf(format, args...)}
}

// NotFound builds a 404 error with a formatted message.
func NotFound(format string, args ...interface{}) *HTTPError {
	return &HTTPError{Status: http.StatusNotFound, Message: fmt.Sprintf(format, args...)}
}

// Unavailable builds a 503 error with a formatted message.
func Unavailable(format string, args ...interface{}) *HTTPError {
	return &HTTPError{Status: http.StatusServiceUnavailable, Message: fmt.Sprintf(format, args...)}
}

// StatusOf returns the HTTP status an error should be reported with,
// defaulting to 400 for errors that carry none.
func StatusOf(err error) int {
	if herr, ok := err.(*HTTPError); ok {
		return herr.Status
	}
	return http.StatusBadRequest
}
