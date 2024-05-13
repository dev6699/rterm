package tty

import "io"

type Agent interface {
	io.ReadWriter

	ResizeTerminal(columns int, row int) error
}
