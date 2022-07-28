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
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"testing"
	"time"

	"github.com/faas-facts/fact/fact"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Tester struct {
}

var hids = []string{"7234fa", "345aaf", "efd346", "ddf3123"}
var cids = []string{"847395", "63ddef", "476583", "feddee", "132456", "234543"}
var boottime = timestamppb.New(time.Now())

func (t Tester) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	end := start.Add(time.Duration(rand.Int63n(6234)) * time.Millisecond)
	trace := fact.Trace{
		ID:               uuid.New().String(),
		Timestamp:        timestamppb.New(time.Now()),
		ContainerID:      cids[rand.Intn(len(cids))],
		HostID:           hids[rand.Intn(len(hids))],
		BootTime:         boottime,
		Cost:             float32(rand.Intn(345)) / 10,
		StartTime:        timestamppb.New(start),
		Status:           200,
		EndTime:          timestamppb.New(end),
		CodeVersion:      "eefade",
		ConfigVersion:    "01",
		Platform:         "TEST",
		Runtime:          "go 1.5",
		Memory:           128,
		ExecutionLatency: durationpb.New(end.Sub(start)),
	}
	data, _ := json.Marshal(trace)
	w.WriteHeader(200)
	_, _ = w.Write(data)

}

type TestOutput struct {
	*bytes.Buffer
}

func newOutput() *TestOutput {
	return &TestOutput{
		bytes.NewBuffer(make([]byte, 0)),
	}
}

func (o *TestOutput) Close() error {
	return nil
}

func (o *TestOutput) Print() {
	fmt.Println(o.Buffer.String())
}

func TestBencherRun(t *testing.T) {
	go func() {
		http.ListenAndServe(":8080", Tester{})
	}()

	reg, _ := http.NewRequest("GET", "http://localhost:8080", nil)
	inv := &HTTPInvoker{
		Request:            reg,
		RequestBody:        nil,
		DisableCompression: true,
		DisableKeepAlive:   false,
		DisableRedirects:   false,
		H2:                 false,
		Timeout:            2,
		client:             nil,
		results:            nil,
	}

	logfile := newOutput()

	bencher := Bencher{
		outputfile: logfile,
		Work: Workload{
			Name:   "test",
			Target: "http://localhost:8080",
			PreRun: func() error {
				log.Info("pre-run")
				return nil
			},
			Phases: []Phase{
				{
					Name:    "30trps",
					Threads: 4,
					HatchRate: &FixedRPSRate{
						RPS:             30,
						BypassAtFailure: false,
					},
					Timeout:     time.Second * 15,
					Target:      "http://localhost:8080",
					PayloadFunc: nil,
					PreRun:      nil,
					PostRun:     nil,
					Invocation:  inv,
				},
			},
			PostRun: func() error {
				log.Info("post-run")
				return nil
			},
		},
		Strict: false,
	}

	bencher.Run()

	logfile.Print()
}
