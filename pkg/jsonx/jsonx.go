package jsonx

import (
	"bytes"
	"errors"
	"strconv"
)

const (
	TypeFalse = iota
	TypeTrue
	TypeNull
	TypeNumber
	TypeString
	TypeArray
	TypeObject
)

func DefineJsonType(data []byte) (int, error) {
	data = bytes.TrimSpace(data)

	if len(data) == 0 {
		return -1, errors.New("empty input")
	}

	strData := string(data)

	switch strData {
	case "false":
		return TypeFalse, nil
	case "true":
		return TypeTrue, nil
	case "null":
		return TypeNull, nil
	}

	first := data[0]
	last := data[len(data)-1]

	if first == '"' && last == '"' {
		return TypeString, nil
	} else if first == '[' && last == ']' {
		return TypeArray, nil
	} else if first == '{' && last == '}' {
		return TypeObject, nil
	}

	if _, err := strconv.ParseFloat(strData, 64); err == nil {
		return TypeNumber, nil
	}

	return -1, errors.New("unrecognized JSON type")
}
