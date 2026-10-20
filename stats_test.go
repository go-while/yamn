package main

import (
	"sync"
	"testing"
)

// TestStatsConcurrent updates counters from several goroutines while a
// report and a reset run, like the main loop and the pool goroutine do. Run
// with -race. Every increment must end up either in the values the reset
// reported or in the counter afterwards.
func TestStatsConcurrent(t *testing.T) {
	s := new(statistics)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 1000 {
				s.outMail.Add(1)
			}
		}()
	}
	s.report()
	reported := s.reportAndReset()
	wg.Wait()
	// outMail is the 5th counter in counters() order.
	if s.counters()[4] != &s.outMail {
		t.Fatal("counters() order changed; update this test")
	}
	if total := reported[4] + s.outMail.Load(); total != 4000 {
		t.Fatalf("Increments lost across the reset: reported %d + remaining %d = %d, want 4000",
			reported[4], s.outMail.Load(), total)
	}
}

func TestReportAndReset(t *testing.T) {
	s := new(statistics)
	s.inMail.Add(3)
	s.outPlain.Add(2)
	reported := s.reportAndReset()
	if reported[0] != 3 || reported[8] != 2 {
		t.Errorf("Reported values: got inMail=%d outPlain=%d, want 3 and 2", reported[0], reported[8])
	}
	for i, c := range s.counters() {
		if v := c.Load(); v != 0 {
			t.Errorf("counter %d is %d after reset, want 0", i, v)
		}
	}
}
