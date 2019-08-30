package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/oliveagle/jsonpath"
)

// Lookup implements a jsonpath search
type Lookup interface {
	// Runs a lookup against the given object
	Lookup(interface{}) (interface{}, error)
	// Returns a string to be used as a filter for SSH show commands
	ForSSH() (string, error)
}

// Lookups is a sequence of Lookup objects
type Lookups []Lookup

type decorated struct {
	err error
	msg string
}

// Error implements error interface
func (d decorated) Error() string {
	return fmt.Sprintf("%s, err: %s", d.msg, d.err)
}

func decorate(err error, data ...interface{}) error {
	return decorated{err: err, msg: fmt.Sprint(data...)}
}

// Lookup implements interface Lookup
func (l Lookups) Lookup(data interface{}) (interface{}, error) {
	for _, lookup := range l {
		// Workaround for the jsonpath library to filter top-level arrays.
		// if the data is a top-level array, replace with an object with a single property, "_".
		// So your filters, instead of "$[...]" must be written "$._[...]"
		switch test := data.(type) {
		case []string:
			data = map[string]interface{}{"_": test}
		case []interface{}:
			data = map[string]interface{}{"_": test}
		}
		result, err := lookup.Lookup(data)
		if err != nil {
			return nil, err
		}
		data = result
	}
	return data, nil
}

// ForSSH implements Lookup
func (l Lookups) ForSSH() (string, error) {
	filters := make([]string, 0, len(l))
	for _, curr := range l {
		filter, err := curr.ForSSH()
		if err != nil {
			return "", err
		}
		filters = append(filters, filter)
	}
	return strings.Join(filters, " | "), nil
}

type jsonLookup struct {
	*jsonpath.Compiled
}

func (jsonLookup) ForSSH() (string, error) {
	return "", errors.New("JSON filter not applicable as SSH filter")
}

// NewLookup turns a chain of filters into a list of Lookups
func NewLookup(chain string) (Lookups, error) {
	result := make(Lookups, 0, 10)
	for _, filter := range SplitNonEmpty(chain, "|") {
		x := strings.Fields(filter)
		// "include" filter?
		if len(x) > 0 && strings.HasPrefix("include", strings.ToLower(strings.TrimSpace(x[0]))) {
			result = append(result, includeLookup(getText(filter)))
			continue
		}
		// "exclude" filter?
		if len(x) > 0 && strings.HasPrefix("exclude", strings.ToLower(strings.TrimSpace(x[0]))) {
			result = append(result, excludeLookup(getText(filter)))
			continue
		}
		// "begin" filter?
		if len(x) > 0 && strings.HasPrefix("begin", strings.ToLower(strings.TrimSpace(x[0]))) {
			result = append(result, beginLookup(getText(filter)))
			continue
		}
		// jsonpath filter: add some syntactic sugar, "._[]" is added automagically.
		if strings.HasPrefix(filter, "?(") {
			filter = fmt.Sprintf("$._[%s]", filter)
		}
		compiled, err := jsonpath.Compile(filter)
		if err != nil {
			return nil, decorate(err, "Failed to compile filter", filter)
		}
		result = append(result, jsonLookup{compiled})
	}
	return result, nil
}

type includeLookup string

type excludeLookup string

type beginLookup string

// Skips the keyword ("include", "begin", etc) in a filter, and removes quotes
func getText(filter string) string {
	parts := strings.SplitN(filter, " ", 2)
	text := ""
	if len(parts) >= 2 {
		text = strings.TrimSpace(parts[1])
		if strings.HasPrefix(text, "\"") || strings.HasPrefix(text, "'") {
			text = text[1 : len(text)-1]
		}
	}
	return text
}

// Lookup implements Lookup interface
func (l includeLookup) Lookup(data interface{}) (interface{}, error) {
	lines, err := Select(data, nil)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.Index(line, string(l)) >= 0 {
			result = append(result, line)
		}
	}
	return result, nil
}

func (l includeLookup) ForSSH() (string, error) {
	return fmt.Sprintf("include \"%s\"", string(l)), nil
}

// Lookup implements Lookup interface
func (l excludeLookup) Lookup(data interface{}) (interface{}, error) {
	lines, err := Select(data, nil)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.Index(line, string(l)) < 0 {
			result = append(result, line)
		}
	}
	return result, nil
}

func (l excludeLookup) ForSSH() (string, error) {
	return fmt.Sprintf("exclude \"%s\"", string(l)), nil
}

// Lookup implements Lookup interface
func (l beginLookup) Lookup(data interface{}) (interface{}, error) {
	lines, err := Select(data, nil)
	if err != nil {
		return nil, err
	}
	var result []string
	for index, line := range lines {
		if strings.Index(line, string(l)) >= 0 {
			result = lines[index:]
			break
		}
	}
	return result, nil
}

func (l beginLookup) ForSSH() (string, error) {
	return fmt.Sprintf("begin \"%s\"", string(l)), nil
}

// Select turns the data into an array of lines
func Select(data interface{}, attribs []string) ([]string, error) {
	var result []string
	switch data := data.(type) {
	case []string:
		result = data
	case []byte:
		result = strings.Split(string(data), "\n")
	case string:
		result = strings.Split(data, "\n")
	case []interface{}:
		result = make([]string, 0, len(data))
		for _, curr := range data {
			tmp, err := Select(curr, attribs)
			if err != nil {
				return nil, err
			}
			result = append(result, tmp...)
		}
	case map[string]interface{}:
		text, err := mapToString(data, attribs)
		if err != nil {
			return nil, err
		}
		result = text
	default: // I only expect numerics or booleans to arrive here
		result = []string{fmt.Sprintf("%v", data)}
	}
	return result, nil
}

// turns a map into an array of strings
func mapToString(data map[string]interface{}, attribs []string) ([]string, error) {
	// Special case for arrays wrapped in objects
	if plain, ok := data["_"]; len(data) == 1 && ok {
		return Select(plain, attribs)
	}
	// Other objects, try to turn into CSV if attrs != nil
	if attribs != nil && len(attribs) >= 0 {
		csv := make([]string, 0, len(attribs))
		for _, attr := range attribs {
			var val string
			if curr, ok := data[attr]; ok {
				switch curr := curr.(type) {
				case []byte:
					val = string(curr)
				case string:
					val = curr
				case int:
					val = string(val)
				case float32:
					val = string(val)
				case float64:
					val = string(val)
				default:
					val = "{Object}"
				}
			}
			csv = append(csv, val)
		}
		return []string{strings.Join(csv, ";")}, nil
	}
	// As a fallback, just dump json
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, decorate(err, "Failed to marshal object to JSON,", data)
	}
	return strings.Split(string(out), "\n"), nil
}

// SplitNonEmpty splits a string and removes empty parts from the results
func SplitNonEmpty(text, separator string) []string {
	parts := strings.Split(text, separator)
	items := make([]string, 0, len(parts))
	for _, v := range parts {
		if v := strings.TrimSpace(v); len(v) > 0 {
			items = append(items, v)
		}
	}
	return items
}
