package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/oliveagle/jsonpath"
)

// Lookup implements a jsonpath search
type Lookup interface {
	// Runs a lookup against the given object
	Lookup(interface{}) (interface{}, error)
}

// Lookups is a sequence of Lookup objects
type Lookups []Lookup

// Lookup implements interface Lookup
func (l Lookups) Lookup(data interface{}) (interface{}, error) {
	for _, lookup := range l {
		// Workaround for the jsonpath library to filter top-level arrays...
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

// NewLookup turns a chain of filters into a list of Lookups
func NewLookup(chain string) (Lookups, error) {
	result := make(Lookups, 0, 10)
	for _, filter := range strings.Split(chain, "|") {
		if f := strings.TrimSpace(filter); len(f) > 0 {
			// If the first word looks like "include", behave as a line filter
			x := strings.Fields(f)
			if len(x) > 0 && strings.HasPrefix("include", strings.ToLower(strings.TrimSpace(x[0]))) {
				result = append(result, &includeLookup{text: getText(f)})
				continue
			}
			if len(x) > 0 && strings.HasPrefix("begin", strings.ToLower(strings.TrimSpace(x[0]))) {
				result = append(result, &beginLookup{text: getText(f)})
				continue
			}
			// Syntactic sugar: if it looks like a filter,
			// wrap it inside $._[]
			if strings.HasPrefix(f, "?(") {
				f = fmt.Sprintf("$._[%s]", f)
			}
			c, err := jsonpath.Compile(f)
			if err != nil {
				return nil, err
			}
			result = append(result, c)
		}
	}
	return result, nil
}

type includeLookup struct {
	text string
}

type beginLookup struct {
	text string
}

// Skips the "include" or "begin" keyword
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
func (l *includeLookup) Lookup(data interface{}) (interface{}, error) {
	lines, err := Select(data, nil)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.Index(line, l.text) >= 0 {
			result = append(result, line)
		}
	}
	return result, nil
}

// Lookup implements Lookup interface
func (l *beginLookup) Lookup(data interface{}) (interface{}, error) {
	lines, err := Select(data, nil)
	if err != nil {
		return nil, err
	}
	var result []string
	for index, line := range lines {
		if strings.Index(line, l.text) >= 0 {
			result = lines[index:]
			break
		}
	}
	return result, nil
}

// Select turns the response into an array of lines
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
	default:
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
		return nil, err
	}
	return strings.Split(string(out), "\n"), nil
}
