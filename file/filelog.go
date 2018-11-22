// Copyright (C) 2017, ccpaging <ccpaging@gmail.com>.  All rights reserved.

package filelog

import (
	"sync"
	"time"
	"os"
	"path/filepath"
	"strings"
	l4g "github.com/ccpaging/nxlog4go"
)

// This log appender sends output to a file
type FileAppender struct {
	mu sync.Mutex 		 // ensures atomic writes; protects the following fields
	layout l4g.Layout 	 // format record for output
	// 2nd cache, formatted message
	messages chan []byte
	// 3nd cache, destination for output with buffered and rotated
	out *l4g.RotateFileWriter
	// Rotate cycle in seconds
	cycle, clock int
	// write loop
	loopRunning bool
	loopReset chan time.Time
}

// Write log record
func (fa *FileAppender) Write(rec *l4g.LogRecord) {
	fa.messages <- fa.layout.Format(rec)
}

func (fa *FileAppender) Init() {
	if fa.loopRunning {
		return
	}
	fa.loopRunning = true
	ready := make(chan struct{})
	go fa.writeLoop(ready)
	<-ready
}

// Close file
func (fa *FileAppender) Close() {
	close(fa.messages)

	// drain the log channel before closing
	for i := 10; i > 0; i-- {
		// Must call Sleep here, otherwise, may panic send on closed channel
		time.Sleep(100 * time.Millisecond)
		if len(fa.messages) <= 0 {
			break
		}
	}
	if fa.out != nil {
		fa.out.Close()
	}

	close(fa.loopReset)
}

func init() {
	l4g.AddAppenderNewFunc("file", New)
	l4g.AddAppenderNewFunc("xml", NewXml)
}

// This creates a new file appender which writes to the file 
// named '<exe path base name>.log' without rotation.
func New() l4g.Appender {
	base := filepath.Base(os.Args[0])
	return NewFileAppender(strings.TrimSuffix(base, filepath.Ext(base)) + ".log", false)
}

func NewXml() l4g.Appender {
	base := filepath.Base(os.Args[0])
	appender := NewFileAppender(strings.TrimSuffix(base, filepath.Ext(base)) + ".log", false)
	appender.SetOption("head","<log created=\"%D %T\">%R")
			
	appender.SetOption("pattern", 
`	<record level="%L">
		<timestamp>%D %T</timestamp>
		<source>%S</source>
		<message>%M</message>
	</record>%R`)
			
	appender.SetOption("foot", "</log>%R")
	return appender
}

// NewFileAppender creates a new appender which writes to the given file and
// has rotation enabled if maxbackup > 0.
func NewFileAppender(fname string, rotate bool) l4g.Appender {
	return &FileAppender{
		layout: 	 l4g.NewPatternLayout(l4g.PATTERN_DEFAULT),	
		messages: 	 make(chan []byte,  l4g.LogBufferLength),
		out: 		 l4g.NewRotateFileWriter(fname, rotate),
		cycle:		 86400,
		clock:		 -1,
		loopRunning: false,
		loopReset: 	 make(chan time.Time, l4g.LogBufferLength),
	}
}

func nextTime(cycle, clock int) time.Time {
	if cycle <= 0 {
		cycle = 86400
	}
	if cycle < 86400 { // Correct invalid clock
		clock = -1
	}
	nrt := time.Now()
	if clock < 0 {
		// Now + cycle
		return nrt.Add(time.Duration(cycle) * time.Second)
	}
	// clock >= 0, next cycle midnight + clock
	nextCycle := nrt.Add(time.Duration(cycle) * time.Second)
	nrt = time.Date(nextCycle.Year(), nextCycle.Month(), nextCycle.Day(), 
					0, 0, 0, 0, nextCycle.Location())
	return nrt.Add(time.Duration(clock) * time.Second)
}

func (fa *FileAppender) writeLoop(ready chan struct{}) {
	defer func() {
		fa.loopRunning = false
	}()

	l4g.LogLogTrace("cycle = %d, clock = %d", fa.cycle, fa.clock)
	if fa.cycle > 0 && fa.out.IsOverSize() {
		fa.out.Rotate()
	}

	nrt := nextTime(fa.cycle, fa.clock)
	rotTimer := time.NewTimer(nrt.Sub(time.Now()))
	l4g.LogLogTrace("Next time is %v", nrt.Sub(time.Now()))

	close(ready)
	for {
		select {
		case bb, ok := <-fa.messages:
			fa.mu.Lock()
			fa.out.Write(bb)
			if len(fa.messages) <= 0 {
				fa.out.Flush()
			}
			fa.mu.Unlock()
			
			if !ok {
 				// drain the log channel and write directly
				fa.mu.Lock()
				for bb := range fa.messages {
					fa.out.Write(bb)
				}
				fa.mu.Unlock()
				return
			}
		case <-rotTimer.C:
			nrt = nextTime(fa.cycle, fa.clock)
			rotTimer.Reset(nrt.Sub(time.Now()))
			l4g.LogLogDebug("Next time is %v", nrt.Sub(time.Now()))
			if fa.cycle > 0 && fa.out.IsOverSize() {
				fa.out.Rotate()
			}
		case <-fa.loopReset:
			l4g.LogLogTrace("Reset. cycle = %d, clock = %d", fa.cycle, fa.clock)
			nrt = nextTime(fa.cycle, fa.clock)
			rotTimer.Reset(nrt.Sub(time.Now()))
			l4g.LogLogTrace("Next time is %v", nrt.Sub(time.Now()))
		}
	}
}

// Set option. chainable
func (fa *FileAppender) Set(name string, v interface{}) l4g.Appender {
	fa.SetOption(name, v)
	return fa
}

/*
Set option. checkable. Better be set before SetFilters()
Option names include:
	filename  - The opened file
	flush	  - Flush file cache buffer size
	maxbackup - Max number for log file storage
	maxsize	  - Rotate at size
	maxlines  - Rotate at lines if maxlines > 0
	pattern	  - Layout format pattern
	utc	  - Log recorder time zone
	head 	  - File head format layout pattern
	foot 	  - File foot format layout pattern
	cycle	  - The cycle seconds of checking rotate size
	clock	  - The seconds since midnight
	daily	  - Checking rotate size at every midnight
*/
func (fa *FileAppender) SetOption(k string, v interface{}) (err error) {
	fa.mu.Lock()
	defer fa.mu.Unlock()

	err = nil

	switch k {
	case "filename":
		fname := ""
		if fname, err = l4g.ToString(v); err == nil && len(fname) > 0 {
			// Directory exist already, return nil
			err = os.MkdirAll(filepath.Dir(fname), l4g.FilePermDefault)
			if err == nil {
				fa.out.SetFileName(fname)
			}
		} else {
			err = l4g.ErrBadValue
		}
	case "flush":
		flush := 0
		if flush, err = l4g.ToInt(v); err == nil {
			fa.out.SetFlush(flush)
		}
	case "head", "foot", "maxbackup", "maxsize", "maxlines":
		err = fa.out.SetOption(k, v)
	case "cycle":
		cycle := 0
		if cycle, err = l4g.ToSeconds(v); err == nil {
			fa.cycle = cycle
			fa.out.Set("rotate", (fa.cycle <= 0))
			if fa.loopRunning {
				fa.loopReset <- time.Now()
			}
		}
	case "clock", "delay0":
		clock := 0
		if clock, err = l4g.ToSeconds(v); err == nil {
			fa.clock = clock
			if fa.loopRunning {
				fa.loopReset <- time.Now()
			}
		}
	case "daily":
		daily := false
		if daily, err = l4g.ToBool(v); err == nil && daily {
			fa.cycle = 86400
			fa.clock = 0
			fa.out.Set("rotate", false)
			if fa.loopRunning {
				fa.loopReset <- time.Now()
			}
		}
	default:
		err = fa.layout.SetOption(k, v)
	}
	return
}