package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh/terminal"
)

func main() {

	// Defaults for some command line arguments
	DefaultFilter := ""
	DefaultTasks := 25
	DefaultArgs := "show version | $._data[0]"
	DefaultTimeout := 60
	DefaultVerify := false
	DefaultDelay := 0

	// Define command line arguments
	optMD := flag.String("h", "", "IP address or host name of MM")
	optUsername := flag.String("u", "", "Username to log in")
	optLimit := flag.Int("l", 0, "Limit number of controllers to query")
	optFilter := flag.String("f", DefaultFilter, "Filter out what switches to collect")
	optTasks := flag.Int("t", DefaultTasks, "Number of parallel tasks")
	optOutput := flag.String("o", "", "Output to a file named after the switch")
	optTimeout := flag.Int("T", DefaultTimeout, "Request timeout in seconds")
	optVerify := flag.Bool("v", false, "Verify MD HTTPS certificate")
	optPassword := flag.String("p", "", "Login password")
	optDelay := flag.Int("d", DefaultDelay, "Delay between commands (seconds)")
	optScript := flag.String("s", "", "Path of script file to run for each controller")

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
	if args == nil || len(args) <= 0 {
		args = []string{DefaultArgs}
	}

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

	// Get MD switches
	log.Println("Getting the switch list")
	timeout := time.Second * time.Duration(*optTimeout)
	delay := time.Second * time.Duration(*optDelay)
	var filter Lookup
	if optFilter != nil && *optFilter != "" {
		compiled, err := NewLookup(*optFilter)
		if err != nil {
			log.Fatal(err)
		}
		filter = compiled
	}
	switches, err := NewController(*optMD, *optUsername, pass, timeout, !(*optVerify)).Switches(filter)
	if err != nil {
		log.Fatal(err)
	}

	// Limit the switches
	if optLimit != nil && *optLimit != 0 && len(switches) > 0 {
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		r.Shuffle(len(switches), func(i, j int) {
			switches[i], switches[j] = switches[j], switches[i]
		})
		switches = switches[:*optLimit]
	}
	log.Println("Switch list collected, working on a set of ", len(switches))

	// Feed the pool
	if *optTasks > len(switches) {
		*optTasks = len(switches)
	}
	pool := NewPool(*optTasks, delay, timeout, !(*optVerify))
	for _, md := range switches {
		pool.Push(md, *optUsername, pass, tasks, script)
	}
	pool.Close()
	log.Println("Waiting for workers to complete!")

	// Print results
	for r := range pool.Results() {
		if r.Err != nil {
			log.Println("**Error: Running against MD", r.MD, ",", r.Err)
		} else {
			fname := ""
			if optOutput != nil && *optOutput != "" {
				fname = fmt.Sprintf("%s%s.log", *optOutput, r.MD)
			}
			if err = writeLines(fname, r.Data, "*** Controller", r.MD, "[", fname, "]"); err != nil {
				log.Println("**Error: saving data for MD", r.MD, ",", err)
			}
		}
	}
}

// writeLines dumps the array to the given file, or stdout
func writeLines(fname string, data []interface{}, header ...interface{}) error {
	lines, err := Select(data, nil)
	if err != nil {
		return err
	}
	var w io.WriteCloser
	if fname == "" {
		w = os.Stdout
	} else {
		w, err = os.Create(fname)
		if err != nil {
			return err
		}
	}
	// Dump a header to separate different controllers
	fmt.Fprintln(os.Stderr, header...)
	if fname == "" {
		// When output is stdout, this funtion is blocking
		for _, line := range lines {
			fmt.Fprintln(w, line)
		}
	} else {
		// When output is a file, the function yields a worker and doesn't block
		go func() {
			defer w.Close()
			for _, line := range lines {
				fmt.Fprintln(w, line)
			}
		}()
	}
	return nil
}
