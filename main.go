package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh/terminal"
)

// ResultStream is the result of running one or more commands in a controller
type ResultStream struct {
	MD     string
	Stream chan (Result)
}

func main() {

	// Defaults for some command line arguments
	DefaultFilter := ""
	DefaultTasks := 25
	//DefaultArgs := "show version | $._data[0]"
	DefaultTimeout := 60
	DefaultVerify := false
	DefaultDelay := 0
	DefaultOutput := ""
	DefaultLoop := 0

	// Define command line arguments
	optMD := flag.String("h", "", "IP address or host name of MM")
	optUsername := flag.String("u", "", "Username to log in")
	optLimit := flag.Int("l", 0, "Limit number of controllers to query")
	optLoop := flag.Int("L", 0, "If greater than 0, time between repetitions of the commands. If 0, do not repeat")
	optFilter := flag.String("f", DefaultFilter, "Filter out what switches to collect")
	optTasks := flag.Int("t", DefaultTasks, "Number of parallel tasks")
	optOutput := flag.String("o", "", "Output to a file named after the switch")
	optTimeout := flag.Int("T", DefaultTimeout, "Request timeout in seconds")
	optVerify := flag.Bool("v", false, "Verify MD HTTPS certificate")
	optPassword := flag.String("p", "", "Login password")
	optDelay := flag.Int("d", DefaultDelay, "Delay between commands (seconds)")
	optScript := flag.String("s", "", "Path of script file to run for each controller")
	optBackup := flag.String("backup", "", "URL for intermediate backup storage (e.g. 'ftp://user:pass@server/folder/filename.tar.gz')")
	optHide := flag.Bool("H", false, "Hide header line before printing results")

	// Parse input
	flag.Parse()
	args, errString := flag.Args(), ""
	if optMD == nil || *optMD == "" {
		errString = "Missing Host address (-h)"
	}
	if optUsername == nil || *optUsername == "" {
		errString = "Missing user name (-u)"
	}
	if errString != "" {
		log.Println("ERROR: ", errString)
		flag.Usage()
		os.Exit(-1)
	}
	if optTasks == nil || *optTasks == 0 {
		optTasks = &DefaultTasks
	}
	if optFilter == nil {
		optFilter = &DefaultFilter
	}
	if optTimeout == nil || *optTimeout <= 0 {
		optTimeout = &DefaultTimeout
	}
	if optDelay == nil || *optDelay <= 0 {
		optDelay = &DefaultDelay
	}
	if optVerify == nil {
		optVerify = &DefaultVerify
	}
	if optOutput == nil {
		optOutput = &DefaultOutput
	}
	if optLoop == nil {
		optLoop = &DefaultLoop
	}
	//if args == nil || len(args) <= 0 {
	//	args = []string{DefaultArgs}
	//}

	// Turn the request into a list of Tasks
	commands := SplitNonEmpty(strings.Join(args, " "), ";")
	tasks := make([]Task, 0, len(commands))
	for _, command := range commands {
		// A Task can have the form <CLI command> | <jsonpath filter> > <comma-separated attributes>
		attrs := strings.SplitN(command, ">", 2)
		paths := strings.SplitN(attrs[0], "|", 2)
		curr := Task{Cmd: strings.TrimSpace(paths[0]), Path: nil, Attr: nil}
		if len(paths) > 1 {
			compiled, err := NewLookup(paths[1])
			if err != nil {
				log.Fatal("Error compiling expression", paths[1], ":", err)
			}
			curr.Path = compiled
		}
		if len(attrs) > 1 {
			curr.Attr = SplitNonEmpty(attrs[1], ",")
		}
		tasks = append(tasks, curr)
	}

	// Get the password
	var pass string
	if optPassword != nil && len(*optPassword) > 0 {
		pass = *optPassword
	} else {
		fmt.Fprint(os.Stderr, "Password: ")
		passBytes, err := terminal.ReadPassword(int(os.Stdin.Fd()))
		if err != nil {
			log.Fatal(err)
		}
		pass = string(passBytes)
		fmt.Fprintln(os.Stderr, "")
	}

	// Get the script
	var script Script
	if optScript != nil && *optScript != "" {
		copies := (*optTasks) / 10
		if optLimit != nil && *optLimit < copies {
			copies = *optLimit
		}
		scriptFile, err := NewScript(*optScript, nil, copies)
		if err != nil {
			log.Fatal("Could not load script ", *optScript, ":", err)
		}
		script = scriptFile
	}

	// Build the controller
	delay := time.Second * time.Duration(*optDelay)
	timeout := time.Second * time.Duration(*optTimeout)
	mm := NewController(*optMD, *optUsername, pass, timeout, !(*optVerify))

	// Do we need to do a backup?
	if optBackup != nil && *optBackup != "" {
		to, err := url.Parse(*optBackup)
		if err != nil {
			log.Fatal(err)
		}
		log.Println("Starting MM flash backup")
		if err := mm.Backup(to); err != nil {
			log.Fatal(err)
		}
		log.Print("Flash backup completed")
	}

	// If no other task, exit
	if len(tasks) <= 0 {
		log.Print("No more tasks to run")
		os.Exit(0)
	}

	// Get MD switches
	log.Println("Getting the switch list")
	var filter Lookup
	if optFilter != nil && *optFilter != "" {
		compiled, err := NewLookup(*optFilter)
		if err != nil {
			log.Fatal(err)
		}
		filter = compiled
	}
	switches, err := mm.Switches(filter)
	if err != nil {
		log.Fatal(err)
	}

	// Limit the switches
	if optLimit != nil && *optLimit != 0 && len(switches) > 0 {
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		r.Shuffle(len(switches), func(i, j int) {
			switches[i], switches[j] = switches[j], switches[i]
		})
		limit := *optLimit
		if limit > len(switches) {
			limit = len(switches)
		}
		switches = switches[:limit]
	}
	loop := time.Second * time.Duration(*optLoop)
	log.Println("Switch list collected, working on a set of ", len(switches))

	// Set to wait for output
	outputTask := sync.WaitGroup{}
	defer outputTask.Wait()

	// Feed the pool
	header := ""
	if optHide == nil || !(*optHide) {
		header = fmt.Sprintf("\n>>> %s\n", strings.Join(commands, ";"))
	}
	if *optTasks > len(switches) {
		*optTasks = len(switches)
	}
	factory := NewFactory(*optOutput)
	pool := NewPool(*optTasks, delay, timeout, loop, !(*optVerify))
	for _, md := range switches {
		stream := pool.Push(md, *optUsername, pass, tasks, script)
		outputTask.Add(1)
		go func(md string) {
			writeResult(factory, md, header, stream)
			outputTask.Done()
		}(md)
	}

	// Wait until finished, or interrupted
	if loop > 0 {
		// if looping forever, wait for ^C
		sigchan := make(chan os.Signal, 1)
		signal.Notify(sigchan, os.Interrupt)
		go func() {
			<-sigchan
			pool.Cancel()
		}()
	}
	log.Println("Waiting for workers to complete!")
	pool.Close()
}

// writeLines dumps the array to the given file, or stdout
func writeResult(factory WriterFactory, MD, header string, stream chan Result) {
	for result := range stream {
		data, err := result.Data, result.Err
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error in", MD, "stream:", err)
			continue
		}
		lines, err := Select(data, nil)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error in", MD, "select:", err)
			continue
		}
		// Open the writer each time, to avoid too many handles kept open
		w, err := factory(MD)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error in", MD, "factory:", err)
			continue
		}
		if header != "" {
			fmt.Fprintln(w, header)
		}
		for _, line := range lines {
			fmt.Fprintln(w, line)
		}
		w.Close()
	}
}
