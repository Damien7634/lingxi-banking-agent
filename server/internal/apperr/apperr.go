// Package apperr defines business errors that are safe to show to the caller.
// Any error that is not an *Error is treated as internal: its message never
// leaves the server.
package apperr

import (
	"errors"
	"fmt"
)

type Code string

const (
	BadRequest      Code = "BAD_REQUEST"
	Unauthorized    Code = "UNAUTHORIZED"
	Forbidden       Code = "FORBIDDEN"
	NotFound        Code = "NOT_FOUND"
	Conflict        Code = "CONFLICT"
	TooManyRequests Code = "TOO_MANY_REQUESTS"
	PayloadTooLarge Code = "PAYLOAD_TOO_LARGE"
	MethodNotAllow  Code = "METHOD_NOT_SUPPORTED"
	ParseError      Code = "PARSE_ERROR"
	Internal        Code = "INTERNAL_SERVER_ERROR"
)

type Error struct {
	Code    Code
	Message string
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

func New(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

func Bad(format string, args ...any) *Error       { return New(BadRequest, format, args...) }
func NotFoundf(format string, args ...any) *Error { return New(NotFound, format, args...) }
func Conflictf(format string, args ...any) *Error { return New(Conflict, format, args...) }

var (
	ErrUnauthorized = &Error{Code: Unauthorized, Message: "请先登录"}
	ErrForbidden    = &Error{Code: Forbidden, Message: "无权执行该操作"}
	ErrTooMany      = &Error{Code: TooManyRequests, Message: "操作过于频繁，请稍后再试"}
	ErrInternal     = &Error{Code: Internal, Message: "服务暂时不可用，请稍后重试"}
)

// As extracts an *Error; ok=false means the error is internal.
func As(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// HTTPStatus maps a code to its HTTP status (same table as tRPC).
func (c Code) HTTPStatus() int {
	switch c {
	case BadRequest, ParseError:
		return 400
	case Unauthorized:
		return 401
	case Forbidden:
		return 403
	case NotFound:
		return 404
	case MethodNotAllow:
		return 405
	case Conflict:
		return 409
	case PayloadTooLarge:
		return 413
	case TooManyRequests:
		return 429
	default:
		return 500
	}
}

// RPCCode maps to the JSON-RPC numeric code tRPC puts in error.code.
func (c Code) RPCCode() int {
	switch c {
	case ParseError:
		return -32700
	case BadRequest:
		return -32600
	case Unauthorized:
		return -32001
	case Forbidden:
		return -32003
	case NotFound:
		return -32004
	case MethodNotAllow:
		return -32005
	case Conflict:
		return -32009
	case PayloadTooLarge:
		return -32013
	case TooManyRequests:
		return -32029
	default:
		return -32603
	}
}
