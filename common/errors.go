package common

import (
	"fmt"
)

var (
	errCodeDefined = make(map[int]interface{})
)

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"msg"`
	ID      string `json:"id,omitempty"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("%d %s", e.Code, e.Message)
}

func (e *Error) Equal(target *Error) bool {
	return e.Code == target.Code
}

func NewError(code int, message string) *Error {
	if _, exist := errCodeDefined[code]; exist {
		panic(fmt.Sprintf("error code %d already exist", code))
	}

	err := &Error{Code: code, Message: message}
	errCodeDefined[code] = err

	return err
}

func GetError(code int) *Error {
	if err, exist := errCodeDefined[code]; exist {
		if e, ok := err.(*Error); ok {
			return e
		}
	}

	return nil
}

var ErrAPINotFound = NewError(1404, "api not found")

var ErrTooFast = NewError(10000, "play too fast")
var ErrTooSlowly = NewError(10001, "play too slowly")
