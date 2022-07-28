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
	"time"
)

const doomUserAgent = "doom/0.0.2"


type Phase struct {
	Name        string        //unique name, might be used to identify this phase
	Threads     int           //number of threads that send the invocation
	HatchRate   HatchRate     //hatch rate, that governs how often new invocations are performed
	Timeout     time.Duration //max duration of this phase
	Target      string        //the target of this workload, can be an url or platfrom identifier (e.g. function name)
	PayloadFunc PayloadFunc   //if set this function is called during _each_ invocation to generate a payload, use for authentication
	PreRun      PreRunFunc   //if set this function will be called _once_ before running the phase
	PostRun     PostRunFunc  //if set this function will be called _once_ after running the phase
	Invocation  Invoker       //the invocation of this phase, e.g. HTTP or CLI
}


type Workload struct {
	Name    string
	Target  string      //the target of this workload, can be an url or platfrom identifier (e.g. function name)
	PreRun  PreRunFunc  //if set this function will be called once before the benchmark
	Phases  []Phase     //phases of the workload
	PostRun PostRunFunc //if set this function will be called once after the benchmark
}

type PreRunFunc func() error

type PostRunFunc func() error

type PayloadFunc func(Invoker) []byte

type BenchmarkConfig struct {
	OutputFile string `json:"output" yaml:"output"`
	Workload WorkloadConfig `json:"workload" yaml:"workload"`
}

type WorkloadConfig struct {
	Name string `json:"name" yaml:"name"`
	Target string `json:"target" yaml:"target"`
	Phases []PhaseConfig `json:"phases" yaml:"phases"`
	Invocation  InvokerConfig `json:"invoker" yaml:"invoker"`
}

func (c WorkloadConfig) Unmarshal() (Workload,error) {
	phases := make([]Phase,0)

	invoker,err := NewInvokerFromConfig(c.Invocation)
	if err != nil {
		return Workload{}, err
	}

	for _, phase := range c.Phases {
		p,err := phase.Unmarshal(c.Target,invoker)
		if err != nil {
			return Workload{}, err
		}
		phases = append(phases,p)
	}

	return Workload{
		Name:    c.Name,
		Target:  c.Target,
		Phases:  phases,

	},nil
}

type PhaseConfig struct {
	Name        string  `json:"name" yaml:"name"`
	Threads     int     `json:"threads" yaml:"threads"`
	HatchRate   HatchRateConfig `json:"hatchRate" yaml:"hatchRate"`
	Timeout     time.Duration  `json:"timeout" yaml:"timeout"`

}

func (c PhaseConfig) Unmarshal(target string, invoker Invoker) (Phase, error) {
	rate,err := NewRateFromConfig(c.HatchRate)
	if err != nil {
		return Phase{}, err
	}

	return Phase{
		Name:        c.Name,
		Threads:     c.Threads,
		HatchRate:   rate,
		Timeout:     c.Timeout,
		Target:      target,
		Invocation:  invoker,
	},nil
}