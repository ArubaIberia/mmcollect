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
	Data []string
	Err  error
}

// Worker is a function capable of running a task in a controller
type Worker func() Result

// Pool of worker gophers running commands in controllers
type Pool struct {
	queue      chan Worker
	results    chan Result
	wg         sync.WaitGroup
	timeout    time.Duration
	delay      time.Duration
	skipVerify bool
}

// NewPool returns a new Task Pool
func NewPool(tasks int, delay, timeout time.Duration, skipVerify bool) *Pool {
	p := &Pool{
		queue:      make(chan Worker),
		results:    make(chan Result),
		delay:      delay,
		timeout:    timeout,
		skipVerify: skipVerify,
	}
	for tasks > 0 {
		p.wg.Add(1)
		tasks--
		go func() {
			for t := range p.queue {
				p.results <- t()
			}
			p.wg.Done()
		}()
	}
	return p
}

// Push adds the tasks to the pool
func (p *Pool) Push(username, pass string, switches []string, commands []Task) *Pool {
	go func() {
		defer func() {
			close(p.queue)
			p.wg.Wait()
			close(p.results)
		}()
		// Iterate on the switches, delivering tasks to the queue
		for _, curr := range switches {
			curr := curr // for the closure below
			p.queue <- func() Result {
				controller := NewController(curr, username, pass, p.timeout, p.skipVerify)
				data, err := p.run(controller, commands)
				return Result{MD: curr, Data: data, Err: err}
			}
		}
	}()
	return p
}

// Results returns a channel where results are streamed
func (p *Pool) Results() chan Result {
	return p.results
}

// run the required commands
func (p *Pool) run(controller *Controller, commands []Task) ([]string, error) {
	session, err := controller.Session()
	if err != nil {
		return nil, err
	}
	defer session.Close()
	results := make([]string, 0, len(commands))
	first := true
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
		text, err := Select(curr, cmd.Attr)
		if err != nil {
			return nil, err
		}
		results = append(results, text...)
	}
	return results, nil
}
