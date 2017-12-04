package pyfmt

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
)

// Using a simple []byte instead of bytes.Buffer to avoid the dependency.
type buffer []byte

func (b *buffer) WriteString(s string) {
	*b = append(*b, s...)
}

const (
	useMap = iota
	useList
	useStruct
)

// ff is used to store a formatter's state.
type ff struct {
	buf buffer

	// Which kind of arg source we shoul use. One of {useMap, useList, useStruct}
	argSrc int

	// argList is the list of arguments, if it was passed that way.
	argList []interface{}
	listPos int

	// argMap is a map of strings, as an alternate format parameter method
	argMap map[string]interface{}

	// argStruct is a struct used for format parameters.
	argStruct interface{}

	// render renders format parameters
	r render
}

// newFormater creates a new ff struct.
// TODO(slongfield): Investigate using a sync.Pool to avoid reallocation.
func newFormater() *ff {
	f := ff{}
	f.listPos = 0
	f.r.init(&f.buf)
	return &f
}

// doFormat parses the string, and executes a format command. Stores the output in ff's buf.
func (f *ff) doFormat(format string) error {
	end := len(format)
	for i := 0; i < end; {
		cachei := i
		// First, get to a '{'
		for i < end && format[i] != '{' {
			// If we see a '}' before a '{' it's an error, unless the next character is also a '}'.
			if format[i] == '}' {
				if i+1 == end || format[i+1] != '}' {
					return errors.New("Single '}' encountered in format string")
				} else {
					f.buf.WriteString(format[cachei:i])
					i++
					cachei = i
				}
			}
			i++
		}
		if i > cachei {
			f.buf.WriteString(format[cachei:i])
		}
		if i >= end {
			break
		}
		i++
		// If the next character is also '{', just put the '{' back in and continue.
		if format[i] == '{' {
			f.buf.WriteString("{")
			i++
			continue
		}
		// TODO(slongfield): Parse the format string.
		f.r.clearflags()
		cachei = i
		for i < end && format[i] != '}' {
			i++
		}
		if format[i] != '}' {
			return errors.New("Single '{' encountered in format string")
		}
		argName := format[cachei:i]
		var err error
		f.r.val, err = f.getArg(argName)
		if err != nil {
			return err
		}
		if err = f.r.render(); err != nil {
			return err
		}
		i++
	}
	return nil
}

func (f *ff) getArg(argName string) (interface{}, error) {
	switch f.argSrc {
	case useList:
		if argName == "" {
			if f.listPos < len(f.argList) {
				arg := f.argList[f.listPos]
				f.listPos++
				return arg, nil
			} else {
				return nil, fmt.Errorf("Format index (%d) out of range (%d)", f.listPos, len(f.argList))
			}
		} else {
			pos, err := strconv.Atoi(argName)
			if err != nil {
				return nil, fmt.Errorf("Invalid index: %s: %v", argName, err)
			}
			if pos < len(f.argList) {
				arg := f.argList[pos]
				return arg, nil
			} else {
				return nil, fmt.Errorf("Format index (%d) out of range (%d)", pos, len(f.argList))
			}
		}
	case useMap:
		arg, ok := f.argMap[argName]
		if !ok {
			return nil, fmt.Errorf("KeyError: %s", argName)
		}
		return arg, nil
	case useStruct:
		arg := reflect.ValueOf(f.argStruct).FieldByName(argName)
		if arg.IsValid() {
			b := arg.Interface()
			fmt.Printf("Returning arg: %v %v\n", b, arg.Kind())
			return b, nil
		}
		return nil, fmt.Errorf("KeyError: %s", argName)
	default:
		return nil, errors.New("unreachable")
	}
}

// Format is the equivlent of Python's string.format() function. Takes a list of possible elements
// to use in formatting, and substitutes them. Only allows for the {}, {0} style of substitutions.
func Format(format string, a ...interface{}) (string, error) {
	f := newFormater()
	f.argList = a
	f.argSrc = useList
	err := f.doFormat(format)
	if err != nil {
		return "", err
	}
	s := string(f.buf)
	return s, nil
}

// FormatMap is similar to Python's string.format(), but takes a map from name to interface to allow
// for {name} style formatting.
func FormatMap(format string, a map[string]interface{}) (string, error) {
	f := newFormater()
	f.argSrc = useMap
	f.argMap = a
	err := f.doFormat(format)
	if err != nil {
		return "", err
	}
	s := string(f.buf)
	return s, nil
}

// Similar to FormatMap, but it takes an arbitrary struct, and uses reflection to get the elements.
func FormatStruct(format string, a interface{}) (string, error) {
	if reflect.ValueOf(a).Kind() != reflect.Struct {
		return "", errors.New("FormatStruct must be called with a struct")
	}
	f := newFormater()
	f.argSrc = useStruct
	f.argStruct = a
	err := f.doFormat(format)
	if err != nil {
		return "", err
	}
	s := string(f.buf)
	return s, nil
}

// MustFormat is like Format, but panics on error.
func MustFormat(format string, a ...interface{}) string {
	s, err := Format(format, a...)
	if err != nil {
		panic(err)
	}
	return s
}

// MustFormatMap is like FormatMap, but panics on error.
func MustFormatMap(format string, a map[string]interface{}) string {
	s, err := FormatMap(format, a)
	if err != nil {
		panic(err)
	}
	return s
}

// MustFormatStruct is like FormatStruct, but panics on error.
func MustFormatStruct(format string, a interface{}) string {
	s, err := FormatStruct(format, a)
	if err != nil {
		panic(err)
	}
	return s
}
