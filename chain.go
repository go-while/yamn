// vim: tabstop=2 shiftwidth=2

package main

import (
	"errors"
	"fmt"

	"github.com/Masterminds/log-go"
	"github.com/crooks/yamn/crandom"
	"github.com/crooks/yamn/keymgr"
)

// distanceCriteria enforces user-defined minimal distance criteria
func distanceCriteria(addresses, dist []string) (c []string) {
	for _, addy := range addresses {
		if IsMemberStr(addy, dist) {
			// Excluded due to distance
			continue
		}
		c = append(c, addy)
	}
	return
}

// nearHops returns the hops within dist of the one being chosen: the last
// dist of left (the hops before it) and the first dist of right (the hops
// after it).  A dist below 1 returns nothing.
func nearHops(left, right []string, dist int) (near []string) {
	if dist < 1 {
		return nil
	}
	near = append(near, left[max(0, len(left)-dist):]...)
	near = append(near, right[:min(dist, len(right))]...)
	return
}

// makeChain takes a chain string and constructs a valid remailer chain
func makeChain(inChain []string) (outChain []string, err error) {
	// Test if stats file has been modified since last imported
	if Pubring.StatRefresh() {
		// Try and import the modified stats file
		err = Pubring.ImportStats()
		if err != nil {
			log.Warnf("Unable to read stats: %s", err)
			return
		}
		log.Info("Stats updated and reimported")
	}
	// Check generated timestamp from stats file
	if Pubring.HaveStats() && Pubring.StatsStale(cfg.Stats.StaleHrs) {
		log.Warnf(
			"Stale stats.  Generated age exceeds "+
				"configured threshold of %d hours",
			cfg.Stats.StaleHrs,
		)
	}
	// If the chain contains a random remailer, we're going to need stats
	if !Pubring.HaveStats() && IsMemberStr("*", inChain) {
		err = errors.New("cannot use random remailers without stats")
		log.Warn(err)
		return
	}
	dist := cfg.Stats.Distance
	if dist > maxChainLength {
		dist = maxChainLength
	}
	var candidates []string // Candidate remailers for each hop
	if len(inChain) > maxChainLength {
		err = fmt.Errorf("%d hops exceeds maximum of %d", len(inChain), maxChainLength)
		return
	}
	// If dist is greater than the actual chain length, all hops will be unique.
	if dist > len(inChain) {
		dist = len(inChain)
	}
	var distance []string
	outChain = make([]string, 0, len(inChain))
	// Loop until inChain contains no more remailers
	numHops := len(inChain)
	// Named hops become addresses first, so the distance check can compare
	// them with the random candidates (which are addresses).
	inChain = append([]string(nil), inChain...)
	for i, hop := range inChain {
		if hop == "*" {
			continue
		}
		var remailer keymgr.Remailer
		remailer, err = Pubring.Get(hop)
		if err != nil {
			return
		}
		inChain[i] = remailer.Address
	}
	var hop string
	for {
		hop = popstr(&inChain)
		// The hops within dist on both sides: the named or still random
		// ones to the left and those already chosen to the right.
		distance = nearHops(inChain, outChain, dist)
		if hop == "*" {
			// Random remailer selection
			if len(outChain) == 0 {
				// Construct a list of suitable exit remailers
				candidates = Pubring.Candidates(
					cfg.Stats.Minlat,
					cfg.Stats.Maxlat,
					cfg.Stats.Relfinal,
					true)
			} else {
				// Construct a list of all suitable remailers
				candidates = Pubring.Candidates(
					cfg.Stats.Minlat,
					cfg.Stats.Maxlat,
					cfg.Stats.Minrel,
					false)
			}
			if len(candidates) > 0 {
				// Apply distance criteria
				candidates = distanceCriteria(candidates, distance)
				if len(candidates) == 0 {
					log.Warn("Insufficient remailers to comply with distance criteria")
				}
			} else {
				log.Warn("No candidate remailers match selection criteria")
			}

			if len(candidates) == 0 && flag.Remailer {
				log.Warn("Relaxing latency and uptime criteria to build chain")
				if len(outChain) == 0 {
					// Construct a list of suitable exit remailers
					log.Info("Constructing relaxed list of Exit remailers")
					candidates = Pubring.Candidates(0, 480, 0, true)
					log.Infof(
						"Discovered %d Exit Remailers matching relaxed criteria",
						len(candidates),
					)
				} else {
					// Construct a list of all suitable remailers
					log.Info("Constructing relaxed list of candidate remailers")
					candidates = Pubring.Candidates(0, 480, 0, false)
					log.Infof(
						"Discovered %d candidate Remailers matching relaxed criteria",
						len(candidates),
					)
				}
			}
			// A client gets an error here, and doesn't relax the criteria.
			if len(candidates) == 0 {
				err = errors.New("no remailers available to build random chain link")
				return
			} else if len(candidates) == 1 {
				hop = candidates[0]
				log.Warnf(
					"Only one remailer (%s) meets chain "+
						"criteria",
					hop,
				)
			} else {
				hop = candidates[crandom.RandomInt(len(candidates))]
			}
		}
		// Extend outChain by 1 element
		outChain = outChain[0 : len(outChain)+1]
		// Shuffle existing entries to the right
		copy(outChain[1:], outChain[:len(outChain)-1])
		// Insert new hop at the start of the output chain
		outChain[0] = hop

		// Break out when the input chain is empty
		if len(inChain) == 0 {
			break
		}
	}
	if len(outChain) != numHops {
		panic("Constructed chain length doesn't match input chain length")
	}
	return
}
