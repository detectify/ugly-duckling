package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func init() {
	flag.Usage = func() {
		h := []string{
			"Run vulnerability detection modules against URLs provided on stdin",
			"",
			"Usage:",
			"  ugly-duckling [options] [<modulepaths>]",
			"",
			"Options:",
			"  -c, --concurrency <int>   Concurrency level (default 1)",
			"  -v, --verbose             Print debug messages etc",
			"",
		}

		fmt.Fprint(os.Stderr, strings.Join(h, "\n"))
	}
}

func main() {

	var verbose bool
	flag.BoolVar(&verbose, "v", false, "verbose mode")
	flag.BoolVar(&verbose, "verbose", false, "verbose mode")

	var concurrency int
	flag.IntVar(&concurrency, "c", 1, "concurrency")
	flag.IntVar(&concurrency, "concurrency", 1, "concurrency")

	flag.Parse()

	// verbose-mode logging function
	logf := func(msg string, params ...interface{}) {
		if !verbose {
			return
		}
		fmt.Printf(msg+"\n", params...)
	}

	// find all module files
	var moduleFiles []string
	var err error
	if flag.NArg() == 0 {
		moduleFiles, err = filepath.Glob("modules/*.json")
		if err != nil {
			log.Fatal(err)
		}
	} else {
		moduleFiles = flag.Args()
	}

	// create module structs for every file
	modules := make([]*module, 0)
	for _, f := range moduleFiles {
		m, err := newModule(f)
		if err != nil {
			log.Println(err)
			continue
		}

		logf("loaded module %s", m.File)
		modules = append(modules, m)
	}

	// a semaphore is a thing we use to control access to some resource,
	// in this case we're going to use it to limit concurrency
	sem := newSemaphore(concurrency)

	// read lines of stdin to get base URLs
	sc := bufio.NewScanner(os.Stdin)

	// http client
	client := newClient(false, "")

	for sc.Scan() {
		baseURL := strings.TrimSpace(sc.Text())
		if baseURL == "" {
			continue
		}

		for _, m := range modules {
			logf("running %s against %s", m.File, baseURL)

			sem.acquire()
			go func(m *module) {
				defer sem.release()

				findings, err := m.run(baseURL, client)
				if err != nil {
					logf("request error: %s", err)
					return
				}

				for _, finding := range findings {
					if !finding.Hit {
						continue
					}

					fmt.Printf("finding: %s\n", finding.Msg)
				}
			}(m)

		}

		// TODO: print or store responses when enabled by some flag or other
		// fmt.Printf("%#v\n", resp)
		// fmt.Printf("%s\n\n", body)
		// fmt.Printf("%#v\n", count)

	}

	sem.wait()
}
