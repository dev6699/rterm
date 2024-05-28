package tty

type Message rune

const (
	Input          Message = '0'
	Output         Message = '1'
	ResizeTerminal Message = '2'

	Auth       Message = 'a'
	AuthTry    Message = 'b'
	AuthOK     Message = 'c'
	AuthFailed Message = 'd'
)
