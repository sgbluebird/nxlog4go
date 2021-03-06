package main

import (
	"encoding/xml"
	"fmt"
	l4g "github.com/ccpaging/nxlog4go"
	_ "github.com/ccpaging/nxlog4go/color"
	_ "github.com/ccpaging/nxlog4go/file"
	_ "github.com/ccpaging/nxlog4go/socket"
	"io/ioutil"
	"os"
)

var fname = "config.xml"

var log = l4g.GetLogger()

func main() {
	// Enable internal logger
	l4g.GetLogLog().Set("level", l4g.TRACE)

	// Open config file
	fd, err := os.Open(fname)
	if err != nil {
		panic(fmt.Sprintf("Can't load xml config file: %s %v", fname, err))
	}
	buf, err := ioutil.ReadAll(fd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not read %q: %s\n", fname, err)
		os.Exit(1)
	}

	fd.Close()

	lc := new(l4g.LoggerConfig)
	if err := xml.Unmarshal(buf, lc); err != nil {
		fmt.Fprintf(os.Stderr, "Could not parse XML configuration. %s\n", err)
		os.Exit(1)
	}
	fmt.Printf("Total configuration: %d\n", len(lc.Filters))
	// fmt.Println(lc)

	errs := log.LoadConfiguration(lc)
	for _, err := range errs {
		fmt.Println(err)
	}

	filters := log.Filters()

	fmt.Printf("Total appenders installed: %d\n", len(filters))

	if _, ok := filters["color"]; ok {
		// disable default console writer
		log.SetOutput(nil)
	}

	// And now we're ready!
	log.Finest("This will only go to those of you really cool UDP kids!  If you change enabled=true.")
	log.Debug("Oh no!  %d + %d = %d!", 2, 2, 2+2)
	log.Trace("Oh no!  %d + %d = %d!", 2, 2, 2+2)
	log.Info("About that time, eh chaps?")

	// Unload filters
	log.SetFilters(nil)
	// Do not forget close all filters
	filters.Close()

	os.Remove("_test.log")
	os.Remove("_trace.xml")
}
