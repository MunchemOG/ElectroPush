// Package javasrc holds the bits of java source handling that more than one
// part of epsh needs to get right the same way.
package javasrc

// Mask blanks out comments and literals, keeping every other byte where it was.
func Mask(code string) string {
	out := []byte(code)

	const (
		plain = iota
		lineComment
		blockComment
		str
		char
	)

	state := plain
	for i := 0; i < len(code); i++ {
		c := code[i]

		switch state {
		case plain:
			switch {
			case c == '/' && i+1 < len(code) && code[i+1] == '/':
				state = lineComment
				out[i], out[i+1] = ' ', ' '
				i++
			case c == '/' && i+1 < len(code) && code[i+1] == '*':
				state = blockComment
				out[i], out[i+1] = ' ', ' '
				i++
			case c == '"':
				state = str
			case c == '\'':
				state = char
			}

		case lineComment:
			if c == '\n' {
				state = plain
			} else {
				out[i] = ' '
			}

		case blockComment:
			if c == '*' && i+1 < len(code) && code[i+1] == '/' {
				state = plain
				out[i], out[i+1] = ' ', ' '
				i++
			} else if c != '\n' {
				out[i] = ' '
			}

		case str, char:
			quote := byte('"')
			if state == char {
				quote = '\''
			}
			if c == '\\' && i+1 < len(code) {
				out[i], out[i+1] = ' ', ' '
				i++
				continue
			}
			if c == quote {
				state = plain
				continue
			}
			out[i] = ' '
		}
	}

	return string(out)
}
