package main

import (
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
