/*
 * Copyright (C) 2021.   Sebastian Werner, TU Berlin, Germany
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <https://www.gnu.org/licenses/>.
 */

package bencher

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type hatchRecorder struct {
	start     time.Time
	failed    uint
	requests  []time.Duration
	tickDelay time.Duration
}

func (r *hatchRecorder) Exec(rate HatchRate) {
	err := rate.Take()
	if err != nil {
		return
	}
	reg := time.Now().Sub(r.start)

	<-time.After(r.tickDelay)
	if rand.Int63n(2) < 1 {
		rate.OnSuccess()
	} else {
		rate.OnFailed()
		r.failed++
	}
	r.requests = append(r.requests, reg)
}

func (r *hatchRecorder) Plot() {

	var max = time.Duration(0)
	for _, request := range r.requests {
		if max < request {
			max = request
		}
	}

	max = max / time.Second
	line := make([]int, int(max)+1)

	for _, request := range r.requests {
		idx := int(request / time.Second)
		line[idx]++
	}
	fmt.Printf("%+v\n", line)
}

func testHatchRate(t *testing.T, rate HatchRate, timeout time.Duration, tick *time.Duration) *hatchRecorder {
	rand.Seed(0x10c0ffee) //reset the seed every time

	//setup a device to capture all alloed requests
	recorder := &hatchRecorder{
		start:    time.Now(),
		requests: make([]time.Duration, 0),
	}

	if tick != nil {
		recorder.tickDelay = *tick
	} else {
		recorder.tickDelay = time.Millisecond * 40
	}

	signal, err := rate.Setup(context.Background(), &Phase{Timeout: timeout})
	if err != nil {
		t.Fatal(err)
	}

	//greedy execution (worst case really)
	for i := 0; i < runtime.NumCPU(); i++ {
		go func() {
			for {
				recorder.Exec(rate)
			}
		}()
	}
	waitOn(signal, &timeout)
	_ = rate.Close()
	return recorder
}

func TestNoopRate(t *testing.T) {
	rate := &NoopRate{}

	recorder := testHatchRate(t, rate, time.Second*5, nil)

	assert.Empty(t, recorder.requests)
}

func TestFixedRate(t *testing.T) {
	rate := &ConstantRate{
		TotalRequests: 10,
	}

	recorder := testHatchRate(t, rate, time.Second*10, nil)

	assert.True(t, len(recorder.requests) >= int(rate.TotalRequests),
		fmt.Sprintf("total request less than allowed expexted:%d got:%d", rate.TotalRequests, len(recorder.requests)))
	assert.True(t, int(rate.TotalRequests) >= len(recorder.requests)-int(recorder.failed),
		fmt.Sprintf("successful request more than allowed expexted:%d got:%d", rate.TotalRequests, len(recorder.requests)-int(recorder.failed)))

}

func TestFixedRPSRate(t *testing.T) {
	rate := &FixedRPSRate{
		RPS: 20,
	}

	tick := time.Second
	recorder := testHatchRate(t, rate, time.Second*10, &tick)
	fmt.Printf("no recovery:%d/%d \n", len(recorder.requests), recorder.failed)
	assert.GreaterOrEqual(t, 20*10, len(recorder.requests))

	rate = &FixedRPSRate{
		RPS:             20,
		BypassAtFailure: true,
	}

	recorder = testHatchRate(t, rate, time.Second*10, &tick)

	assert.GreaterOrEqual(t, 20*10, len(recorder.requests)-int(recorder.failed))
	fmt.Printf("recovery:%d/%d \n", len(recorder.requests), recorder.failed)

}

func TestSlopeingRate(t *testing.T) {
	tests := []struct {
		n int64
		r float64
		b bool
	}{
		{0, 0, false},
		{20, 0, false},
		{20, 1, false},
		{20, 1.5, false},
		{20, 2, false},
		{60, 0.5, false},
		{60, -0.5, false},
	}
	for _, test := range tests {
		testSlopingRate(t, test.n, test.r, test.b)
	}
}

func testSlopingRate(t *testing.T, n int64, r float64, bypassAtFailure bool) {
	const runtime = 5

	rate := &SlopingRate{
		StartRate:       n,
		HatchRate:       r,
		BypassAtFailure: bypassAtFailure,
	}
	//shitty version of an intergal under the slop function...
	target := int64(0)
	for i := 1; i <= runtime; i++ {
		target += rate.StartRate * int64(math.Pow(float64(i), rate.HatchRate))
	}
	tick := time.Duration(0)
	recorder := testHatchRate(t, rate, time.Second*runtime, &tick)

	var got int64
	if bypassAtFailure {
		got = int64(len(recorder.requests) - int(recorder.failed))
	} else {
		got = int64(len(recorder.requests))
	}

	if !assert.True(t, math.Abs(float64(target-got)) <= float64(target)*.1,
		fmt.Sprintf("(%d,%.1f,%+v) expected:%d got:%d [%d/%d]",
			n, r, bypassAtFailure, target, got, len(recorder.requests), recorder.failed)) {
		recorder.Plot()
	}
}
