package transfer

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

	var pingWg sync.WaitGroup
	var pingMu sync.Mutex
	rttMeasurements := make([]time.Duration, 0, numPings)

	for pingNum := 0; pingNum < numPings; pingNum++ {
		pingWg.Add(1)
		go func() {
			defer pingWg.Done()

			timeout := 1 * time.Second
			c, err := comm.NewConnection(addr, timeout)
			if err != nil {
				return
			}
			defer c.Close()

			start := time.Now()
			err = c.Send([]byte("ping"))
			if err != nil {
				return
			}

			b, err := c.Receive()
			if err != nil || !bytes.Equal(b, []byte("pong")) {
				return
			}

			totalTime := time.Since(start)
			pingMu.Lock()
			rttMeasurements = append(rttMeasurements, totalTime)
			pingMu.Unlock()
		}()
	}

	pingWg.Wait()

	var rtt time.Duration
	if len(rttMeasurements) == 0 {
		rtt = time.Hour
	} else {
		var total time.Duration
		for _, pingRTT := range rttMeasurements {
			total += pingRTT
		}
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
	originalLevel := log.GetLevel()
	log.SetLevel("warn")
	defer log.SetLevel(originalLevel)

	rtts := make([]RelayRTT, len(relays))
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i, r := range relays {
		wg.Add(1)
		go func(index int, relay Relay) {
			defer wg.Done()

			result := TestRelayRTT(relay, index, numPings)

			mu.Lock()
			rtts[index] = result
			mu.Unlock()
		}(i, r)
	}

	wg.Wait()

	var selected RelayRTT

	totalSuccessfulPings := 0
	for _, rtt := range rtts {
		totalSuccessfulPings += rtt.SuccessfulPings
	}

	if totalSuccessfulPings == 0 && len(relays) > 0 {
		randomIndex := rand.IntN(len(relays))
		randomRelay := relays[randomIndex]
		ports := strings.Split(randomRelay.Ports, ",")
		addr := randomRelay.Address + ":" + ports[0]

		selected = RelayRTT{
			Index:           randomIndex,
			RTT:             time.Hour,
			Addr:            addr,
			SuccessfulPings: 0,
		}
		return rtts, selected
	}

	sort.Slice(rtts, func(i, j int) bool {
		return rtts[i].RTT < rtts[j].RTT
	})

	if len(rtts) == 0 {
		selected.RTT = time.Hour
	} else {
		n := topN
		if n > len(rtts) {
			n = len(rtts)
		}
		if n < 1 {
			n = 1
		}

		selectedIndex := rand.IntN(n)
		selected = rtts[selectedIndex]
	}

	return rtts, selected
}
