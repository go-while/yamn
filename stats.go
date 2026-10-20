package main

import (
	"fmt"
	"runtime"
	"sync/atomic"

	"github.com/Masterminds/log-go"
)

// statistics holds throughput counters.  They're updated from both the main
// server loop and the outbound pool goroutine, so they're atomic.
type statistics struct {
	inDummy    atomic.Int64
	inMail     atomic.Int64
	inRemFoo   atomic.Int64
	inYamn     atomic.Int64
	outDummy   atomic.Int64
	outMail    atomic.Int64
	outYamn    atomic.Int64
	outLoop    atomic.Int64
	outRandhop atomic.Int64
	outPlain   atomic.Int64
}

// counters returns the counters in report order.
func (s *statistics) counters() []*atomic.Int64 {
	return []*atomic.Int64{
		&s.inMail, &s.inRemFoo, &s.inYamn, &s.inDummy,
		&s.outMail, &s.outYamn, &s.outLoop, &s.outRandhop, &s.outPlain, &s.outDummy,
	}
}

// log writes one report from values given in counters() order.
func (s *statistics) log(v []int64) {
	log.Infof(
		"MailIn=%d, RemFoo=%d, YamnIn=%d, DummyIn=%d",
		v[0], v[1], v[2], v[3],
	)
	line1 := fmt.Sprintf(
		"MailOut=%d, YamnOut=%d, YamnLoop=%d, Randhop=%d, ",
		v[4], v[5], v[6], v[7],
	)
	line2 := fmt.Sprintf(
		"FinalOut=%d, DummyOut=%d",
		v[8], v[9],
	)
	log.Infof(line1 + line2)
}

// report logs the current counters.
func (s *statistics) report() {
	var v []int64
	for _, c := range s.counters() {
		v = append(v, c.Load())
	}
	s.log(v)
}

// reportAndReset logs the counters and sets them to zero, returning the
// values it reported in counters() order.  Each counter is read and zeroed
// in one step, so increments made meanwhile aren't lost.
func (s *statistics) reportAndReset() []int64 {
	var v []int64
	for _, c := range s.counters() {
		v = append(v, c.Swap(0))
	}
	s.log(v)
	log.Info("Daily stats reset")
	return v
}

var stats = new(statistics)

// logMemInfo logs the process's memory use from the Go runtime (--meminfo).
// It helps to spot a long-running remailer growing over time.  HeapInUse is
// memory in use by Go objects, Sys what the process got from the OS, and
// TotalAlloc everything allocated since start (it only grows).
func logMemInfo() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	mib := func(b uint64) string { return fmt.Sprintf("%.1fMiB", float64(b)/(1<<20)) }
	log.Infof(
		"Memory: HeapInUse=%s, HeapObjects=%d, Sys=%s, TotalAlloc=%s, NumGC=%d, Goroutines=%d",
		mib(m.HeapInuse),
		m.HeapObjects,
		mib(m.Sys),
		mib(m.TotalAlloc),
		m.NumGC,
		runtime.NumGoroutine(),
	)
}
