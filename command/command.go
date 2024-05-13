package command

import (
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"github.com/creack/pty"
)

type Command struct {
	pty *os.File
	cmd *exec.Cmd
}

func New(name string, arg []string) (*Command, error) {
	cmd := exec.Command(name, arg...)

	f, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}

	c := &Command{
		cmd: cmd,
		pty: f,
	}

	go c.wait()
	return c, nil
}

func (c *Command) wait() {
	defer c.pty.Close()
	c.cmd.Wait()
}

func (c *Command) Read(p []byte) (n int, err error) {
	return c.pty.Read(p)
}

func (c *Command) Write(p []byte) (n int, err error) {
	return c.pty.Write(p)
}

func (c *Command) ResizeTerminal(width int, height int) error {
	window := struct {
		row uint16
		col uint16
		x   uint16
		y   uint16
	}{
		uint16(height),
		uint16(width),
		0,
		0,
	}
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		c.pty.Fd(),
		syscall.TIOCSWINSZ,
		uintptr(unsafe.Pointer(&window)),
	)
	if errno != 0 {
		return errno
	} else {
		return nil
	}
}
