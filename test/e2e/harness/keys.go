//go:build integration

package harness

import "strings"

// ParseKeys converts a key sequence string into raw bytes.
// Recognized escape tokens (case-insensitive, inside angle brackets):
//
//	<cr>/<enter>      → 0x0D (carriage return)
//	<esc>             → 0x1B
//	<c-c>/<ctrl+c>   → 0x03
//	<c-a> … <c-z>    → Ctrl+letter
//	<tab>             → 0x09
//	<bs>/<backspace>  → 0x7F
//	<up>              → ESC[A
//	<down>            → ESC[B
//	<right>           → ESC[C
//	<left>            → ESC[D
//	<home>            → ESC[H
//	<end>             → ESC[F
//	<pgup>/<pageup>   → ESC[5~
//	<pgdn>/<pagedown> → ESC[6~
//	<del>/<delete>    → ESC[3~
//	<f1>…<f12>        → corresponding ANSI sequences
//
// Anything else inside <…> is passed through literally (e.g. <:> → ":").
func ParseKeys(s string) string {
	var out strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '<' {
			end := strings.IndexByte(s[i:], '>')
			if end == -1 {
				out.WriteByte(s[i])
				i++
				continue
			}
			token := strings.ToLower(s[i+1 : i+end])
			i += end + 1
			switch token {
			case "cr", "enter":
				out.WriteByte(0x0D)
			case "esc", "escape":
				out.WriteByte(0x1B)
			case "c-c", "ctrl+c":
				out.WriteByte(0x03)
			case "c-a", "ctrl+a":
				out.WriteByte(0x01)
			case "c-b", "ctrl+b":
				out.WriteByte(0x02)
			case "c-d", "ctrl+d":
				out.WriteByte(0x04)
			case "c-e", "ctrl+e":
				out.WriteByte(0x05)
			case "c-f", "ctrl+f":
				out.WriteByte(0x06)
			case "c-g", "ctrl+g":
				out.WriteByte(0x07)
			case "c-h", "ctrl+h":
				out.WriteByte(0x08)
			case "c-k", "ctrl+k":
				out.WriteByte(0x0B)
			case "c-l", "ctrl+l":
				out.WriteByte(0x0C)
			case "c-n", "ctrl+n":
				out.WriteByte(0x0E)
			case "c-p", "ctrl+p":
				out.WriteByte(0x10)
			case "c-r", "ctrl+r":
				out.WriteByte(0x12)
			case "c-u", "ctrl+u":
				out.WriteByte(0x15)
			case "c-w", "ctrl+w":
				out.WriteByte(0x17)
			case "tab":
				out.WriteByte(0x09)
			case "bs", "backspace":
				out.WriteByte(0x7F)
			case "up":
				out.WriteString("\x1b[A")
			case "down":
				out.WriteString("\x1b[B")
			case "right":
				out.WriteString("\x1b[C")
			case "left":
				out.WriteString("\x1b[D")
			case "home":
				out.WriteString("\x1b[H")
			case "end":
				out.WriteString("\x1b[F")
			case "pgup", "pageup":
				out.WriteString("\x1b[5~")
			case "pgdn", "pagedown":
				out.WriteString("\x1b[6~")
			case "del", "delete":
				out.WriteString("\x1b[3~")
			case "f1":
				out.WriteString("\x1bOP")
			case "f2":
				out.WriteString("\x1bOQ")
			case "f3":
				out.WriteString("\x1bOR")
			case "f4":
				out.WriteString("\x1bOS")
			case "f5":
				out.WriteString("\x1b[15~")
			case "f6":
				out.WriteString("\x1b[17~")
			case "f7":
				out.WriteString("\x1b[18~")
			case "f8":
				out.WriteString("\x1b[19~")
			case "f9":
				out.WriteString("\x1b[20~")
			case "f10":
				out.WriteString("\x1b[21~")
			case "f11":
				out.WriteString("\x1b[23~")
			case "f12":
				out.WriteString("\x1b[24~")
			default:
				// Pass through literally (e.g. <:> → ":", <R> → "R")
				out.WriteString(s[i-end-1 : i])
			}
		} else {
			out.WriteByte(s[i])
			i++
		}
	}
	return out.String()
}

// Cmd returns a command string for SendKeys, e.g. Cmd("quit") → ":quit<cr>"
func Cmd(name string) string {
	return ":" + name + "<cr>"
}
