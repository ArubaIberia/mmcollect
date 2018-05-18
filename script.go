package main

import (
	"fmt"
	"sync"

	"github.com/robertkrimen/otto"
	_ "github.com/robertkrimen/otto/underscore"
)

// Script runs a JavaScript engine with a preloaded script
type Script interface {
	Run(session *Session, data []interface{}) (interface{}, error)
}

type script struct {
	vm     *otto.Otto
	script *otto.Script
	mutex  sync.Mutex
}

// NewScript returns a bundle of VM + script
func NewScript(filename string, src interface{}) (Script, error) {
	result := script{vm: otto.New()}
	s, err := result.vm.Compile(filename, src)
	if err != nil {
		return nil, err
	}
	result.script = s
	return &result, nil
}

// Run the script with a given controller and set of data
func (s *script) Run(session *Session, data []interface{}) (interface{}, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	// Post(cfgpath, api, data) exported to javascript
	s.vm.Set("Post", func(call otto.FunctionCall) otto.Value {
		cfgPath := call.Argument(0).String()
		api := call.Argument(1).String()
		data, err := call.Argument(2).Export()
		if err == nil {
			if err = session.Post(cfgPath, api, data); err == nil {
				return otto.NullValue()
			}
		}
		val, _ := s.vm.ToValue(err.Error())
		return val
	})
	// "data0", "data1", "data2"... are the output of commands
	for i, d := range data {
		s.vm.Set(fmt.Sprintf("data%d", i), d)
	}
	value, err := s.vm.Run(s.script)
	if err != nil {
		return nil, err
	}
	native, err := value.Export()
	if err != nil {
		return nil, err
	}
	return native, nil
}
