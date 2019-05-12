package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// WriterFactory creates a new writer for every MD
type WriterFactory func(MD string) (io.WriteCloser, error)

type seqFactory struct {
	sem chan struct{}
}

func newSeqFactory() WriterFactory {
	sem := make(chan struct{}, 1)
	return WriterFactory(func(MD string) (io.WriteCloser, error) {
		label := strings.Join([]string{"*** Controller", MD}, " ")
		sem <- struct{}{}
		fmt.Fprintln(os.Stderr, label)
		return seqFactory{sem: sem}, nil
	})
}

func (f seqFactory) Write(p []byte) (n int, err error) {
	return os.Stdout.Write(p)
}

func (f seqFactory) Close() error {
	<-f.sem
	return nil
}

func newFactory(prefix string) WriterFactory {
	return WriterFactory(func(MD string) (io.WriteCloser, error) {
		fname := fmt.Sprintf("%s%s.log", prefix, MD)
		label := strings.Join([]string{"*** Controller", MD, "[ ", fname, " ]"}, " ")
		fmt.Fprintln(os.Stderr, label)
		return os.OpenFile(fname, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	})
}

// NewFactory returns a writer factory for the given prefix
func NewFactory(prefix string) WriterFactory {
	if prefix == "" {
		// If output is to stdout, make it sequential
		return newSeqFactory()
	}
	return newFactory(prefix)
}
