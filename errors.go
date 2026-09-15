package firecrest

import (
	"errors"
	"fmt"
	"io"
	"net/http"
)

var (
	ErrUnauthorized = errors.New("FirecREST unauthorized")
	ErrNotFound     = errors.New("FirecREST resource not found")
)

// APIError exposes the HTTP status and FirecREST error envelope.
type APIError struct {
	StatusCode int
	Detail     string
	Err        error
}

func (e *APIError) Error() string {
	return fmt.Sprintf("FirecREST API error: status=%d detail=%q", e.StatusCode, e.Detail)
}
func (e *APIError) Unwrap() error { return e.Err }

func newAPIError(resp *http.Response) error {
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	apiErr := &APIError{StatusCode: resp.StatusCode, Detail: string(data)}
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		apiErr.Err = ErrUnauthorized
	case http.StatusNotFound:
		apiErr.Err = ErrNotFound
	}
	return apiErr
}
