package manage

import (
	"net/url"

	"github.com/hydrogencms/hydrogen/core/content"
)

// ParseListQuery reads the list query parameters the management API accepts (locale, status, q, sort, limit,
// offset and where[field][op]) into list options; any other parameter is a bad request. The delivery API
// reuses it so both list the same way.
func ParseListQuery(typ string, q url.Values) (content.ListOptions, error) { return parseList(typ, q) }

// ErrorEnvelope is the JSON value of the error body {"error":{"code":…,"message":…}} that e is sent as. The
// delivery API encodes it with its own headers, so both APIs share one error shape.
func ErrorEnvelope(e *Error) any {
	return envelope(errorBody{Code: e.Code, Message: e.Message, Fields: e.Fields})
}

// ParseWhere reads the where[field][op]=value parameters of q (ops as in lists) into filters; other
// parameters are ignored.
func ParseWhere(q url.Values) ([]content.Filter, error) { return parseWhere(q) }
