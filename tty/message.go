package tty

type Message rune

const (
	Input          Message = '0'
	Output         Message = '1'
	ResizeTerminal Message = '2'
)
