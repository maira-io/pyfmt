package pyfmt

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

type flags struct {
	fillChar   rune
	align      int
	sign       string
	showRadix  bool
	minWidth   string
	precision  string
	renderVerb string
	percent    bool
}

// Render is the renderer used to render dispatched format strings into a buffer that's been set up
// beforehand.
type render struct {
	buf *buffer
	val interface{}

	flags
}

func (r *render) init(buf *buffer) {
	r.buf = buf
	r.clearFlags()
}

func (r *render) clearFlags() {
	r.flags = flags{}
}

// Flag state machine
const (
	alignState = iota
	signState
	radixState
	zeroState
	widthState
	precisionState
	verbState
	endState
)

// validFlags holds the list of valid flags, for quick checkup.
// Valid flags are 'bdoxXeEfFgGrts%'
var validFlags = map[byte]struct{}{
	'b': struct{}{}, 'd': struct{}{}, 'o': struct{}{}, 'x': struct{}{}, 'X': struct{}{},
	'e': struct{}{}, 'E': struct{}{}, 'f': struct{}{}, 'F': struct{}{}, 'g': struct{}{},
	'G': struct{}{}, 'r': struct{}{}, 't': struct{}{}, 's': struct{}{}, '%': struct{}{}}

var isDigit = map[byte]struct{}{
	'0': struct{}{}, '1': struct{}{}, '2': struct{}{}, '3': struct{}{}, '4': struct{}{},
	'5': struct{}{}, '6': struct{}{}, '7': struct{}{}, '8': struct{}{}, '9': struct{}{},
}

// splitFlags splits out the flags into the various fields. This replaces the previous regex parser
// (see render_test.go for regex)
func splitFlags(flags string) (align, sign, radix, zeroPad, minWidth, precision, verb string, err error) {
	end := len(flags)
	if end == 0 {
		return
	}
	state := alignState
	for i := 0; i < end; {
		switch state {
		case alignState:
			if end > 1 && (flags[1] == '<' || flags[1] == '>' || flags[1] == '=' || flags[1] == '^') {
				i += 2
			} else if flags[i] == '<' || flags[i] == '>' || flags[i] == '=' || flags[i] == '^' {
				i += 1
			}
			// TODO(slongfield): Support arbitrary runes as alignment characters.
			align = flags[0:i]
			state = signState
		case signState:
			if flags[i] == '+' || flags[i] == '-' || flags[i] == ' ' {
				sign = flags[i : i+1]
				i += 1
			}
			state = radixState
		case radixState:
			if flags[i] == '#' {
				radix = flags[i : i+1]
				i += 1
			}
			state = zeroState
		case zeroState:
			if flags[i] == '0' {
				zeroPad = flags[i : i+1]
				i += 1
			}
			state = widthState
		case widthState:
			var j int
			for j = i; j < end; {
				if _, ok := isDigit[flags[j]]; ok {
					j += 1
				} else {
					break
				}
			}
			minWidth = flags[i:j]
			i = j
			state = precisionState
		case precisionState:
			if flags[i] == '.' {
				var j int
				for j = i + 1; j < end; {
					if _, ok := isDigit[flags[j]]; ok {
						j += 1
					} else {
						break
					}
				}
				precision = flags[i:j]
				i = j
			}
			state = verbState
		case verbState:
			if _, ok := validFlags[flags[i]]; ok {
				verb = flags[i : i+1]
				i += 1
			}
			state = endState
		default:
			// Get to this state when we've run out of other states. If we reach this, it means we've
			// gone too far, since we've passed the verb state, but aren't at the end of the string, so
			// error.
			err = errors.New("Could not decode format specification: " + flags)
			i = end + 1
		}
	}
	return
}

func (r *render) parseFlags(flags string) error {
	r.renderVerb = "v"
	if flags == "" {
		return nil
	}
	align, sign, radix, zeroPad, minWidth, precision, verb, err := splitFlags(flags)
	if err != nil {
		return Error("Invalid flag pattern: {}, {}", flags, err)
	}
	if len(align) > 1 {
		var size int
		r.fillChar, size = utf8.DecodeRuneInString(align)
		align = align[size:]
	}
	if align != "" {
		switch align {
		case "<":
			r.align = left
		case ">":
			r.align = right
		case "=":
			r.align = padSign
		case "^":
			r.align = center
		default:
			panic("Unreachable, this should never happen.")
		}
	}
	if sign != "" {
		// "-" is the default behavior, ignore it.
		if sign != "-" {
			r.sign = sign
		}
	}
	if radix == "#" {
		r.showRadix = true
	}
	if zeroPad != "" {
		if align == "" {
			r.align = padSign
		}
		if r.fillChar == 0 {
			r.fillChar = '0'
		}
	}
	if minWidth != "" {
		r.minWidth = minWidth
	}
	if precision != "" {
		r.precision = precision
	}
	if verb != "" {
		switch verb {
		case "b", "o", "x", "X", "e", "E", "f", "F", "g", "G":
			r.renderVerb = verb
		case "d":
			r.renderVerb = verb
			r.showRadix = false
		case "%":
			r.percent = true
			r.renderVerb = "f"
		case "r":
			r.renderVerb = "#v"
		case "t":
			r.renderVerb = "T"
		case "s":
			r.renderVerb = "+v"
		default:
			panic("Unreachable, this should never happen. Flag parsing regex is corrupted.")
		}
	}
	return nil
}

// render renders a single element by passing that element and the translated format string
// into the fmt formatter.
func (r *render) render() error {
	var prefix, radix string
	var width int64
	var err error
	if r.percent {
		if err = r.setupPercent(); err != nil {
			return err
		}
	}
	if r.showRadix {
		if r.renderVerb == "x" || r.renderVerb == "X" {
			radix = "#"
		} else if r.renderVerb == "b" {
			prefix = "0b"
		} else if r.renderVerb == "o" {
			prefix = "0o"
		}
	}

	if r.minWidth == "" {
		width = 0
	} else {
		width, err = strconv.ParseInt(r.minWidth, 10, 64)
		if err != nil {
			return Error("Can't convert width {} to int", r.minWidth)
		}
	}

	// Only let Go handle the width for floating+complex types, elsewhere the alignment rules are
	// different.
	if r.renderVerb != "f" && r.renderVerb != "F" && r.renderVerb != "g" && r.renderVerb != "G" && r.renderVerb != "e" && r.renderVerb != "E" {
		r.minWidth = ""
	}

	str := fmt.Sprintf("%"+r.sign+radix+r.minWidth+r.precision+r.renderVerb, r.val)

	if prefix != "" {
		// Get rid of any prefix added by minWidth. We'll add this back in later when we
		// WriteAlignedString to the underlying buffer
		str = strings.TrimLeft(str, " ")
		if str != string(r.fillChar) {
			str = strings.TrimLeft(str, string(r.fillChar))
		}
		if len(str) > 0 && str[0] == '-' {
			str = strings.Join([]string{"-", prefix, str[1:]}, "")
		} else if len(str) > 0 && str[0] == '+' {
			str = strings.Join([]string{"+", prefix, str[1:]}, "")
		} else if r.sign == " " {
			str = strings.Join([]string{" ", prefix, str}, "")
		} else {
			str = strings.Join([]string{prefix, str}, "")
		}
	}

	if r.renderVerb == "f" || r.renderVerb == "F" || r.renderVerb == "g" || r.renderVerb == "G" || r.renderVerb == "e" || r.renderVerb == "E" {
		str = strings.TrimSpace(str)
		if r.sign == " " && str[0] != '-' {
			str = " " + str
		}
	}

	if r.percent {
		str, err = transformPercent(str)
		if err != nil {
			return err
		}
	}

	if len(str) > 0 {
		if str[0] != '(' && (r.align == left || r.align == padSign) {
			if str[0] == '-' {
				r.buf.WriteString("-")
				str = str[1:]
				width -= 1
			} else if str[0] == '+' {
				r.buf.WriteString("+")
				str = str[1:]
				width -= 1
			} else if str[0] == ' ' {
				r.buf.WriteString(" ")
				str = str[1:]
				width -= 1
			} else {
				r.buf.WriteString(r.sign)
			}
		}
	}

	if r.showRadix && r.align == padSign {
		r.buf.WriteString(str[0:2])
		r.buf.WriteAlignedString(str[2:], r.align, width-2, r.fillChar)
	} else {
		r.buf.WriteAlignedString(str, r.align, width, r.fillChar)
	}
	return nil
}

func (r *render) setupPercent() error {
	// Increase the precision by two, to make sure we have enough digits.
	if r.precision == "" {
		r.precision = ".8"
	} else {
		precision, err := strconv.ParseInt(r.precision[1:], 10, 64)
		if err != nil {
			return err
		}
		r.precision = Must(".{}", precision+2)
	}
	return nil
}

func transformPercent(p string) (string, error) {
	var parts []string
	var sign string
	if p[0] == '-' {
		sign = "-"
		p = p[1:]
	}
	parts = strings.SplitN(p, ".", 2)
	var suffix string
	if len(parts) == 2 {
		prefix, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return "", Error("Couldn't parse format prefix from: {}", p)
		}
		if prefix == 0 {
			if parts[1][2:] != "" {
				suffix = "." + parts[1][2:]
			}
			if parts[1][0] == '0' {
				return strings.Join([]string{sign, parts[1][1:2], suffix, "%"}, ""), nil
			} else {
				return strings.Join([]string{sign, parts[1][0:2], suffix, "%"}, ""), nil
			}
		} else if len(parts[0]) == 1 {
			if parts[1][2:] != "" {
				suffix = "." + parts[1][2:]
			}
			return strings.Join([]string{sign, parts[0], parts[1][0:2], suffix, "%"}, ""), nil
		}
		if parts[1][2:] != "" {
			suffix = "." + parts[1][2:]
		}
		return strings.Join([]string{sign, parts[0], parts[1][0:2], suffix, "%"}, ""), nil
	}
	if _, err := strconv.ParseInt(p, 10, 64); err != nil {
		return p + "%", nil
	}
	return p + "00%", nil

}
