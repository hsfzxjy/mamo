package mamo

import (
	"bytes"
	"fmt"
	"runtime/debug"
)

type PanicError struct {
	Value interface{}
	Stack []byte
}

// Error implements error interface.
func (p *PanicError) Error() string {
	return fmt.Sprintf("%v\n\n%s", p.Value, p.Stack)
}

func newPanicError(v interface{}) error {
	stack := debug.Stack()

	// The first line of the stack trace is of the form "goroutine N [status]:"
	// but by the time the panic reaches Do the goroutine may no longer exist
	// and its status will have changed. Trim out the misleading line.
	if line := bytes.IndexByte(stack[:], '\n'); line >= 0 {
		stack = stack[line+1:]
	}
	return &PanicError{Value: v, Stack: stack}
}
