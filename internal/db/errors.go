package db

import "errors"

// Typed sentinel errors returned by every Driver implementation. Callers use
// errors.Is to test them.
var (
	ErrNotFound      = errors.New("db: not found")
	ErrConflict      = errors.New("db: conflict")
	ErrBadCondition  = errors.New("db: bad condition")
	ErrBadCollection = errors.New("db: bad collection name")
	ErrReadOnly      = errors.New("db: read-only")
	ErrClosed        = errors.New("db: closed")
	ErrUnsupportedOp = errors.New("db: unsupported operation")
	ErrDuplicateUID  = errors.New("db: duplicate uid")
	ErrValidation    = errors.New("db: validation failed")
)
