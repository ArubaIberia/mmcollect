package main

import (
	"sync"
	"time"
)

// Task is a command to run on a controller
type Task struct {
	Cmd  string
	Path Lookup
	Attr []string
}

// Result is the result of running one or more commands in a controller
type Result struct {
	MD   string
	Data []interface{}
	Err  error
}

// Pool of worker gophers running commands in controllers
type Pool struct {
	results    chan Result
	wg         sync.WaitGroup
	timeout    time.Duration
	delay      time.Duration
	sem        chan bool
	skipVerify bool
}

// NewPool returns a new Task Pool
func NewPool(tasks int, delay, timeout time.Duration, skipVerify bool) *Pool {
	p := &Pool{
		delay:      delay,
		timeout:    timeout,
		skipVerify: skipVerify,
		results:    make(chan Result, tasks),
		sem:        make(chan bool, tasks),
	}
	return p
}

// Push adds the tasks to the pool
func (p *Pool) Push(md, username, pass string, commands []Task, script Script) {
	// Leave notice a new thread is running
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		// Concurrency limit
		p.sem <- true
		defer func() { _ = <-p.sem }()
		// Iterate on the switches, delivering tasks to the queue
		controller := NewController(md, username, pass, p.timeout, p.skipVerify)
		data, err := p.run(controller, commands, script)
		p.results <- Result{MD: md, Data: data, Err: err}
	}()
}

// Close tells the pool no more tasks will be pushed
func (p *Pool) Close() {
	go func() {
		p.wg.Wait()
		close(p.results)
	}()
}

// Results returns a channel where results are streamed
func (p *Pool) Results() chan Result {
	return p.results
}

// run the required commands
func (p *Pool) run(controller *Controller, commands []Task, script Script) ([]interface{}, error) {
	session, err := controller.Session()
	if err != nil {
		return nil, err
	}
	defer session.Close()
	result := make([]interface{}, 0, len(commands))
	first := true
	// Get data
	for _, cmd := range commands {
		// add delay, if requested
		if first {
			first = false
		} else if p.delay > 0 {
			time.Sleep(p.delay)
		}
		curr, err := session.Show(cmd.Cmd, cmd.Path)
		if err != nil {
			return nil, err
		}
		if cmd.Attr != nil && len(cmd.Attr) > 0 {
			selected, err := Select(curr, cmd.Attr)
			if err != nil {
				return nil, err
			}
			curr = selected
		}
		result = append(result, curr)
	}
	if script != nil {
		post, err := script.Run(session, result)
		if err != nil {
			return nil, err
		}
		result = []interface{}{post}
	}
	return result, nil
}
