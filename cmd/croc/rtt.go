package croc

import (
	"bytes"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/schollz/croc/v9/src/comm"
	log "github.com/schollz/logger"
)

// RelayRTT represents the RTT test result for a relay
type RelayRTT struct {
	Index           int
	RTT             time.Duration
	Addr            string
	SuccessfulPings int
}

// TestRelayRTT tests a single relay's RTT by running multiple parallel pings
func TestRelayRTT(relay Relay, index int, numPings int) RelayRTT {
	ports := strings.Split(relay.Ports, ",")
	addr := relay.Address + ":" + ports[0]

	// Test RTT using ping/pong mechanism - run pings in parallel
	var pingWg sync.WaitGroup
	var pingMu sync.Mutex
	rttMeasurements := make([]time.Duration, 0, numPings)

	// Run all pings in parallel
	for pingNum := 0; pingNum < numPings; pingNum++ {
		pingWg.Add(1)
		go func() {
			defer pingWg.Done()

			start := time.Now()
			timeout := 1 * time.Second
			c, err := comm.NewConnection(addr, timeout)
			
			// If connection timed out, record timeout value as RTT
			if err != nil {
				pingMu.Lock()
				rttMeasurements = append(rttMeasurements, timeout)
				pingMu.Unlock()
				return
			}

			err = c.Send([]byte("ping"))
			if err != nil {
				c.Close()
				return // Skip this ping if send fails
			}

			b, err := c.Receive()
			c.Close()
			if err != nil || !bytes.Equal(b, []byte("pong")) {
				return // Skip this ping if receive fails or wrong response
			}

			// Successful ping - record the RTT
			totalTime := time.Since(start)
			pingMu.Lock()
			rttMeasurements = append(rttMeasurements, totalTime)
			pingMu.Unlock()
		}()
	}

	// Wait for all pings to complete
	pingWg.Wait()

	// Calculate average RTT from successful pings
	var rtt time.Duration
	if len(rttMeasurements) == 0 {
		rtt = time.Hour // Unreachable, set to high RTT
	} else {
		var total time.Duration
		for _, pingRTT := range rttMeasurements {
			total += pingRTT
		}
		// Cast the length to a Duration to make them comparable
		rtt = total / time.Duration(len(rttMeasurements))
	}

	return RelayRTT{
		Index:           index,
		RTT:             rtt,
		Addr:            addr,
		SuccessfulPings: len(rttMeasurements),
	}
}

// TestAllRelaysRTT tests all relays in parallel and returns one of the top N fastest servers
func TestAllRelaysRTT(relays []Relay, numPings int, topN int) ([]RelayRTT, RelayRTT) {
	// Suppress debug logs from comm package during RTT testing
	originalLevel := log.GetLevel()
	log.SetLevel("warn")
	defer log.SetLevel(originalLevel)

	rtts := make([]RelayRTT, len(relays))
	var wg sync.WaitGroup
	var mu sync.Mutex

	log.Debug("Testing relay latency (RTT):")

	// Test all hosts in parallel
	for i, r := range relays {
		wg.Add(1)
		go func(index int, relay Relay) {
			defer wg.Done()

			result := TestRelayRTT(relay, index, numPings)

			// Store result
			mu.Lock()
			rtts[index] = result
			log.Debugf("Relay %s - RTT: %v (%d/%d successful)", result.Addr, result.RTT, result.SuccessfulPings, numPings)
			mu.Unlock()
		}(i, r)
	}

	// Wait for all hosts to complete testing
	wg.Wait()

	// Sort results by RTT (fastest first)
	sort.Slice(rtts, func(i, j int) bool {
		return rtts[i].RTT < rtts[j].RTT
	})

	// Select one of the top N fastest servers
	var selected RelayRTT
	if len(rtts) == 0 {
		selected.RTT = time.Hour
	} else {
		// Determine how many servers to consider (top N, or all if fewer than N)
		n := topN
		// clamp to the total amount of relays - full random selection
		if n > len(rtts) {
			n = len(rtts)
		}
		// If < 1 was passed, set to 1
		if n < 1 {
			n = 1
		}

		// Randomly select from the top N
		selectedIndex := rand.IntN(n)
		selected = rtts[selectedIndex]
	}

	return rtts, selected
}

