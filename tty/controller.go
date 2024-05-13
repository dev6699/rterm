package tty

import "io"

type Controller interface {
	io.ReadWriter
}
