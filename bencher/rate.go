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
	"golang.org/x/time/rate"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var _hatchRateTypes = []string{"noop","slope","fixed","constant"}

type HatchRateConstructor func (config HatchRateConfig) (HatchRate,error)

var _rates = make(map[string]HatchRateConstructor)

//RegisterHatchRate is an extension method to register new HatchRates used for config resolution
func RegisterHatchRate(name string, constructor HatchRateConstructor) error{
	for _,k := range _hatchRateTypes {
		if name == k {
			return fmt.Errorf("cannot use %s to register a HatchRate",name)
		}
	}
	_rates[name]=constructor
	return nil
}

type HatchRateConfig struct {
	Type string `yaml:"type"`
	Options map[string]interface{} `yaml:",inline"`

}


func NewRateFromConfig(config HatchRateConfig) (HatchRate,error) {
	_type := strings.TrimSpace(strings.ToLower(config.Type))
	switch _type {
	case "noop":
		return &NoopRate{},nil
	case "slope":
		return newSlopingRateFromConfig(config)
	case "fixed":
		return newFixedRateFromConfig(config)
	case "constant":
		return newConstantRateFromConfig(config)
	}

	if val,ok := _rates[_type]; ok {
		return val(config)
	}

	return nil, fmt.Errorf("unknown rate type")
}

func newConstantRateFromConfig(config HatchRateConfig) (HatchRate, error) {
	if !checkFields(config.Options,  "requests") {
		return nil, fmt.Errorf("missing values for sloap type")
	}
	requests := uint64(config.Options["requests"].(int))

	return &ConstantRate{
		TotalRequests: requests,
	},nil
}

func newFixedRateFromConfig(config HatchRateConfig) (HatchRate, error) {
	if !checkFields(config.Options,  "trps") {
		return nil, fmt.Errorf("missing values for sloap type")
	}
	trps := config.Options["trps"].(int)

	bypass := flagValue("bypass",config.Options,false)
	return &FixedRPSRate{
		RPS:             int64(trps),
		BypassAtFailure: bypass,
	},nil
}

func newSlopingRateFromConfig(config HatchRateConfig) (HatchRate, error) {
	if !checkFields(config.Options, "start", "rate") {
		return nil, fmt.Errorf("missing values for sloap type")
	}

	start := config.Options["start"].(int)


	rate := config.Options["rate"].(float64)


	bypass := flagValue("bypass",config.Options,false)

	return &SlopingRate{
		StartRate:       int64(start),
		HatchRate:       rate,
		BypassAtFailure: bypass,
	}, nil
}

type HatchRate interface {
	//function is called once before starting the phase
	Setup(context.Context, *Phase) (*sync.Cond, error)
	//function should block until the next request should be made
	Take() error
	//call if the invocation was successful
	OnSuccess()
	//call if the invocation failed
	OnFailed()
	//call if the invocation was queued
	OnQueued()

	Close() error

}

type ConstantRate struct {
	TotalRequests uint64
	counter       chan struct{}
	send          uint64
	closed        error
	signal        *sync.Cond
	sync.RWMutex
}

func (f *ConstantRate) Setup(ctx context.Context, phase *Phase) (*sync.Cond, error) {
	f.Lock()
	defer f.Unlock()
	m := sync.Mutex{}
	m.Lock()
	cond := sync.NewCond(&m)
	f.counter = make(chan struct{},f.TotalRequests)
	for i := uint64(0); i < f.TotalRequests; i++ {
		f.counter<- struct{}{}
	}
	f.send = 0
	f.signal = cond

	return cond, nil
}
func (f *ConstantRate) Take() error {
	<-f.counter
	return f.closed
}
func (f *ConstantRate) OnSuccess() {
	f.Lock()
	f.send = f.send +1
	defer f.Unlock()
	if f.send >= f.TotalRequests {
		//signal done!
		f.signal.Broadcast()
	}
}
func (f *ConstantRate) OnFailed()  {
	f.counter<- struct{}{}
}
func (f *ConstantRate) OnQueued() {}
func (f *ConstantRate) Close() error {
	f.closed = fmt.Errorf("closed")
	close(f.counter)
	f.signal.Broadcast()
	return nil
}

type FixedRPSRate struct{
	RPS             int64
	BypassAtFailure bool
	rate            *rate.Limiter
	bypass          uint64
	ctx             context.Context
	cancel			context.CancelFunc
}

func (f *FixedRPSRate) Setup(ctx context.Context, phase *Phase) (*sync.Cond, error) {
	f.ctx,f.cancel = context.WithCancel(ctx)
	m := sync.Mutex{}
	m.Lock()
	signal := sync.NewCond(&m)
	f.bypass = 0
	f.rate = rate.NewLimiter(rate.Every(time.Second/time.Duration(f.RPS)),1)
	go func() {
		time.Sleep(phase.Timeout)
		f.cancel()
		signal.Broadcast()
	}()




	return signal, nil
}
func (f *FixedRPSRate) Take() error {
	if atomic.LoadUint64(&f.bypass) > 0 {
		atomic.AddUint64(&f.bypass,^uint64(0))
		return nil
	}

	return f.rate.Wait(f.ctx)
}
func (f *FixedRPSRate) OnSuccess() {}
func (f *FixedRPSRate) OnFailed() {
	if f.BypassAtFailure {
		atomic.AddUint64(&f.bypass,1)

	}
}
func (f *FixedRPSRate) OnQueued() {}
func (f *FixedRPSRate) Close() error {
	f.cancel()
	f.bypass = 0
	return nil
}

type SlopingRate struct {
	StartRate       int64
	HatchRate       float64
	BypassAtFailure bool
	tickets         chan struct{}
	step       		int64

	lastInsert time.Time
	ctx  context.Context
	cancel context.CancelFunc
	closed bool
}
func (r *SlopingRate) Setup(ctx context.Context, phase *Phase) (*sync.Cond, error) {
	r.ctx,r.cancel = context.WithCancel(ctx)

	r.tickets = make(chan struct{})
	r.closed = false

	go func() {
		for {
			delay := time.Duration(math.Max(float64(time.Second - (time.Now().Sub(r.lastInsert))),0))
			select {
				case <-time.After(delay):
				case <-r.ctx.Done():
					return
			}
			r.step++
			r.insert()
		}

	}()

	return nil, nil
}
func (r *SlopingRate) insert() {
	goingRate := r.StartRate*int64(math.Pow(float64(r.step),r.HatchRate))
	for i := int64(0); i < goingRate; i++ {
		if !r.closed{
			r.tickets <- struct{}{}
		}
	}
	r.lastInsert = time.Now()
}
func (r *SlopingRate) Take() error {
	select {
		case <-r.tickets:
			return nil
		case <-r.ctx.Done():
			return fmt.Errorf("done")
	}
}
func (r *SlopingRate) OnSuccess() {

}
func (r *SlopingRate) OnFailed() {
	if r.BypassAtFailure {
		r.tickets <- struct{}{}
	}
}
func (r *SlopingRate) OnQueued() {}
func (r *SlopingRate) Close() error {
	r.closed = !r.closed
	r.cancel()
	return nil
}

type NoopRate struct{
	ctx context.Context
	cancel context.CancelFunc
}
func (n *NoopRate) Setup(ctx context.Context, phase *Phase) (*sync.Cond, error) {
	n.ctx,n.cancel = context.WithCancel(ctx)
	return nil, nil
}
func (n *NoopRate) Take() error {
	select {
		case <-n.ctx.Done():
			return fmt.Errorf("closed")
	}
}
func (n *NoopRate) OnSuccess() {}
func (n *NoopRate) OnFailed() {}
func (n *NoopRate) OnQueued() {}
func (n *NoopRate) Close() error {
	n.cancel()
	return nil
}