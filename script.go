package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/robertkrimen/otto"
	_ "github.com/robertkrimen/otto/underscore"
)

// Script runs a JavaScript engine with a preloaded script
type Script interface {
	Run(session *Session, data []interface{}) (interface{}, error)
}

type script struct {
	vms    []*otto.Otto
	script *otto.Script
	sem    chan int
}

// NewScript returns a bundle of VM + script
func NewScript(filename string, src interface{}, copies int) (Script, error) {
	if copies < 1 {
		copies = 1
	}
	sem := make(chan int, copies)
	vm, vms := otto.New(), make([]*otto.Otto, 1, copies)
	vm.Set("getenv", jsEnv)
	vm.Set("console", map[string]interface{}{"log": jsLog})
	vms[0] = vm
	sem <- 0
	s, err := vm.Compile(filename, src)
	if err != nil {
		return nil, err
	}
	for i := 1; i < copies; i++ {
		vms = append(vms, vm.Copy())
		sem <- i
	}
	return &script{vms: vms, script: s, sem: sem}, nil
}

// Run the script with a given controller and set of data
func (s *script) Run(session *Session, data []interface{}) (interface{}, error) {
	free := <-s.sem
	defer func() { s.sem <- free }()
	vm := s.vms[free]
	// Post(cfgpath, api, data) exported to javascript
	vm.Set("session", map[string]interface{}{
		"post": s.jsPost(vm, session),
		"get":  s.jsGet(vm, session),
		"ip":   session.Controller().IP(),
		"date": time.Now().Format("20060102"),
	})
	vm.Set("data", data)
	value, err := vm.Run(s.script)
	if err != nil {
		return nil, err
	}
	native, err := value.Export()
	if err != nil {
		return nil, err
	}
	return native, nil
}

type requestFunc func(cfgPath, endpoint string, data interface{}) (interface{}, error)

// Closure for an API request to the session
func apiCall(vm *otto.Otto, rFunc requestFunc) func(otto.FunctionCall) otto.Value {
	return func(call otto.FunctionCall) otto.Value {
		args := call.ArgumentList
		if len(args) < 3 {
			return ottoErr(errors.New("Too few arguments. Must provide (config_path, api_endpoint, data)"))
		}
		if !args[0].IsString() {
			return ottoErr(errors.New("First argument must be config path (e.g. \"/mm\""))
		}
		cfgPath := call.Argument(0).String()
		if !args[1].IsString() {
			return ottoErr(errors.New("Second argument must be api endpoint (e.g. \"object/aaa_user_delete\""))
		}
		endpoint := call.Argument(1).String()
		data, err := call.Argument(2).Export()
		if err != nil {
			return ottoErr(err)
		}
		result, err := rFunc(cfgPath, endpoint, data)
		if err != nil {
			return ottoErr(err)
		}
		if result == nil {
			return otto.NullValue()
		}
		v, err := vm.ToValue(result)
		if err != nil {
			return ottoErr(err)
		}
		return v
	}
}

// Post a request, return nil if no error otherwise an error object
func (s *script) jsPost(vm *otto.Otto, session *Session) func(otto.FunctionCall) otto.Value {
	return apiCall(vm, func(cfgPath, endpoint string, data interface{}) (interface{}, error) {
		return session.Post(cfgPath, endpoint, data)
	})
}

// Get a request, return result body
func (s *script) jsGet(vm *otto.Otto, session *Session) func(otto.FunctionCall) otto.Value {
	return apiCall(vm, func(cfgPath, endpoint string, data interface{}) (interface{}, error) {
		return session.Get(cfgPath, endpoint, data)
	})
}

func ottoErr(err error) otto.Value {
	val, _ := otto.ToValue(err.Error())
	return val
}

// Implement console.log
func jsLog(call otto.FunctionCall) otto.Value {
	output := []string{}
	for _, argument := range call.ArgumentList {
		output = append(output, fmt.Sprintf("%v", argument))
	}
	log.Println(strings.Join(output, " "))
	return otto.UndefinedValue()
}

// Gets an environment variable
func jsEnv(call otto.FunctionCall) otto.Value {
	args := call.ArgumentList
	if len(args) < 1 || !args[0].IsString() {
		v, _ := otto.ToValue("")
		return v
	}
	vname, _ := args[0].ToString()
	value, _ := otto.ToValue(os.Getenv(vname))
	return value
}
