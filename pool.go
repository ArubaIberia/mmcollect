package main

import (
	"net/http"
	"sync"
	"time"
)

// Task is a command to run on a controller
type Task struct {
	Cmd  string
	Path Lookup
	Attr []string
}

// Result of one execution in the loop
type Result struct {
	Data []interface{}
	Err  error
}

// Pool of worker gophers running commands in controllers
type Pool struct {
	client *http.Client
	wg     sync.WaitGroup
	delay  time.Duration
	loop   time.Duration
	sem    chan struct{}
	cancel chan struct{}
}

// NewPool returns a new Task Pool
func NewPool(tasks int, delay, loop time.Duration, client *http.Client) *Pool {
	p := &Pool{
		client: client,
		delay:  delay,
		loop:   loop,
		sem:    make(chan struct{}, tasks),
		cancel: make(chan struct{}),
	}
	return p
}

// Push adds the tasks to the pool
func (p *Pool) Push(md, username, pass string, commands []Task, script Script) chan Result {
	// Leave notice a new thread is running
	p.wg.Add(1)
	controller := NewController(md, username, pass, p.client)
	stream := make(chan Result, 1)
	go func() {
		defer p.wg.Done()
		defer controller.Close()
		defer close(stream)
		for repeat := true; repeat; {
			data, err := func() ([]interface{}, error) {
				// Concurrency limit
				p.sem <- struct{}{}
				defer func() { <-p.sem }()
				// Iterate on the switches, delivering tasks to the queue
				return p.run(controller, commands, script)
			}()
			// Do not wait on the stream with the semaphore locked!
			stream <- Result{Data: data, Err: err}
			if p.loop <= 0 {
				repeat = false
			} else {
				select {
				case <-time.After(p.loop):
					repeat = true
				case <-p.cancel:
					repeat = false
				}
			}
		}
	}()
	return stream
}

// Cancel loops
func (p *Pool) Cancel() {
	close(p.cancel)
}

// Close tells the pool no more tasks will be pushed. It does not cancel it.
func (p *Pool) Close() {
	p.wg.Wait()
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
