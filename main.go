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

	"github.com/oliveagle/jsonpath"
	"golang.org/x/crypto/ssh/terminal"
)

func main() {

	// Defaults for some command line arguments
	DefaultFilter := "?(@.Status == 'up')"
	DefaultTasks := 25
	DefaultArgs := "show version | $._data[0]"
	DefaultTimeout := 60
	DefaultVerify := false

	// Define command line arguments
	optMD := flag.String("h", "", "IP address or host name of MM")
	optUsername := flag.String("u", "", "Username to log in")
	optLimit := flag.Int("l", 0, "Limit number of controllers to query")
	optFilter := flag.String("f", DefaultFilter, "Filter out what switches to collect")
	optTasks := flag.Int("t", DefaultTasks, "Number of parallel tasks")
	optOutput := flag.String("o", "", "Output to a file named after the switch")
	optTimeout := flag.Int("T", DefaultTimeout, "Request timeout in seconds")
	optVerify := flag.Bool("v", false, "Verify MD HTTPS certificate")

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
		fmt.Println("ERROR: ", errString)
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
	if optVerify == nil {
		optVerify = &DefaultVerify
	}
	if args == nil || len(args) <= 0 {
		args = []string{DefaultArgs}
	}

	// Turn the request into a list of Tasks
	lines := strings.Split(strings.Join(args, " "), ";")
	tasks := make([]Task, 0, len(lines))
	for _, line := range lines {
		// A Task can have the form <CLI command> | <jsonpath filter>
		parts := strings.SplitN(line, "|", 2)
		curr := Task{Cmd: strings.TrimSpace(parts[0]), Path: nil}
		if len(parts) > 1 {
			path, err := jsonpath.Compile(strings.TrimSpace(parts[1]))
			if err != nil {
				log.Fatal("Error compiling expression ", parts[1], ": ", err)
			}
			curr.Path = path
		}
		tasks = append(tasks, curr)
	}

	// Get the password
	fmt.Print("Password: ")
	passBytes, err := terminal.ReadPassword(0)
	if err != nil {
		log.Fatal(err)
	}
	pass := string(passBytes)
	fmt.Println("")

	// Get MD switches
	timeout := time.Second * time.Duration(*optTimeout)
	switches, err := Switches(*optMD, *optUsername, pass, *optFilter, timeout, !(*optVerify))
	if err != nil {
		log.Fatal(err)
	}
	if optLimit != nil && *optLimit != 0 && len(switches) > 0 {
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		r.Shuffle(len(switches), func(i, j int) {
			switches[i], switches[j] = switches[j], switches[i]
		})
		switches = switches[:*optLimit]
	}

	// Run the pool
	if *optTasks > len(switches) {
		*optTasks = len(switches)
	}
	pool := NewPool(*optTasks, timeout, !(*optVerify))
	pool.Push(*optUsername, pass, switches, tasks)
	log.Print("Waiting for workers to complete!")
	for r := range pool.Results() {
		var err error
		if r.Err != nil {
			err = r.Err
		} else if optOutput == nil || *optOutput == "" {
			// If no output specified, dump to stdout
			fmt.Println("CONTROLLER ", r.MD)
			err = writeLines("", r.Data)
		} else {
			// If there is an output prefix, dump to the proper file
			fname := fmt.Sprintf("%s%s.log", *optOutput, r.MD)
			fmt.Println("SAVING OUTPUT OF CONTROLLER ", r.MD, " TO ", fname)
			err = writeLines(fname, r.Data)
		}
		if err != nil {
			fmt.Println("ERROR RUNNING AGAINST MD ", r.MD, ": ", err)
		}
	}
}

// writeLines dumps the array to the given file, or stdout
func writeLines(fname string, lines []string) error {
	var w io.WriteCloser
	var err error
	if fname == "" {
		w = os.Stdout
	} else {
		w, err = os.Create(fname)
		if err != nil {
			return err
		}
	}
	for _, line := range lines {
		fmt.Fprintln(w, line)
	}
	if fname != "" {
		w.Close()
	}
	return nil
}
