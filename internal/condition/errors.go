package condition

import (
	"errors"
	"fmt"
)

// Sentinel errors. Wrap with %w in ParseError/EvalError when raised.
var (
	ErrParse           = errors.New("condition: parse error")
	ErrUnsupportedOp   = errors.New("condition: unsupported operation")
	ErrInvalidArgument = errors.New("condition: invalid argument")
)

// ParseError carries the parser position and the offending token (if any).
type ParseError struct {
	Pos     int
	Token   string
	Message string
}

func (e *ParseError) Error() string {
	if e.Token != "" {
		return fmt.Sprintf("condition: parse error at %d near %q: %s", e.Pos, e.Token, e.Message)
	}
	return fmt.Sprintf("condition: parse error at %d: %s", e.Pos, e.Message)
}

func (e *ParseError) Unwrap() error { return ErrParse }

// InvalidConditionError matches Python's ConditionInvalid: wraps an op + args.
type InvalidConditionError struct {
	Op      string
	Args    any
	Message string
}

func (e *InvalidConditionError) Error() string {
	return fmt.Sprintf("condition: invalid %q (%v): %s", e.Op, e.Args, e.Message)
}

func (e *InvalidConditionError) Unwrap() error { return ErrInvalidArgument }
