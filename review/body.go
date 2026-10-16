package review

import (
	"bufio"
	"errors"
	"io"
	"net/http"

	"github.com/hydrogencms/hydrogen/manage"
)

// optionalBody decodes a JSON object body that may be absent.
func optionalBody(w http.ResponseWriter, r *http.Request, dst any) error {
	br := bufio.NewReader(r.Body)
	if _, err := br.Peek(1); errors.Is(err, io.EOF) {
		return nil
	}
	r.Body = io.NopCloser(br)
	return manage.DecodeJSON(w, r, dst)
}
