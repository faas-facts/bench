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
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/faas-facts/fact/fact"
	"github.com/google/uuid"
	"golang.org/x/net/http2"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"io/ioutil"
	"math"
	"net/http"
	"net/http/httptrace"
	"os"
	"time"
)


//TODO: needs testing
type HTTPInvoker struct {
	Request     *http.Request
	RequestBody []byte

	// DisableCompression is an option to disable compression in response
	DisableCompression bool

	// DisableKeepAlive is an option to prevents re-use of TCP connections between different HTTP requests
	DisableKeepAlive bool

	// DisableRedirects is an option to prevent the following of HTTP redirects
	DisableRedirects bool

	// H2 is an option to make HTTP/2 requests
	H2 bool

	// Timeout in seconds.
	Timeout int
	client  *http.Client
	results *fact.ResultCollector
}

//TODO: needs testing
//TODO: needs documentation
func newHttpInvoker(config InvokerConfig) (Invoker,error) {
	valid := checkFields(config.Options,"timeout")
	if !valid {
		return nil,fmt.Errorf("missing key in config")
	}

	timeout, err := time.ParseDuration(config.Options["timeout"].(string))
	if err != nil{
		return nil,err
	}

	var body []byte = nil
	if val,ok := config.Options["body"];ok {
		body = []byte(val.(string))
	}

	return &HTTPInvoker{
		RequestBody:        body,
		DisableCompression: flagValue("compression",config.Options,false),
		DisableKeepAlive:   flagValue("keep_alive",config.Options,true),
		DisableRedirects:   flagValue("redirects",config.Options,false),
		H2:                 flagValue("h2",config.Options,false),
		Timeout:            int(math.Ceil(timeout.Seconds())),
	},nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}


func (h *HTTPInvoker) Setup(phase *Phase,bencher *Bencher) error  {
	var ServerName string
	if h.Request != nil {
		ServerName = h.Request.Host
	} else {
		ServerName,_ = os.Hostname()
	}
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
			ServerName:         ServerName,
		},
		MaxIdleConnsPerHost: min(phase.Threads, maxIdleConn),
		DisableCompression:  h.DisableCompression,
		DisableKeepAlives:   h.DisableKeepAlive,
	}
	if h.H2 {
		_ = http2.ConfigureTransport(tr)
	} else {
		tr.TLSNextProto = make(map[string]func(string, *tls.Conn) http.RoundTripper)
	}
	h.client = &http.Client{Transport: tr, Timeout: time.Duration(h.Timeout) * time.Second}

	h.results = bencher.results

	if h.Request == nil{
		// set content-type
		header := make(http.Header)

		header.Set("Content-Type", "text/plain")
		header.Set("X-Benchmark", "true")

		req, err := http.NewRequest("GET", phase.Target, nil)
		if err != nil {
			return err
		}

		ua := req.UserAgent()
		if ua == "" {
			ua = doomUserAgent
		} else {
			ua += " " + doomUserAgent
		}
		header.Set("User-Agent", ua)
		req.Header = header

		h.Request = req
	}

	if phase.PayloadFunc == nil{
		h.RequestBody = []byte{}
	} else {
		h.RequestBody = phase.PayloadFunc(h)
	}

	return nil
}

func (h *HTTPInvoker) Exec(rate HatchRate) error {
	err := rate.Take()
	if err != nil{
		return err
	}

	result := h.makeRequest(h.client)

	if result.Status == 200 {
		rate.OnSuccess()
	} else if result.Status >= 400 {
		rate.OnFailed()
	}

	h.results.Add(result)

	return nil
}

func (b *HTTPInvoker) makeRequest(c *http.Client) *fact.Trace {
	id := uuid.New().String()

	var size int64
	var code int
	var resStart, RStart time.Time
	var reqDuration time.Duration
	var req = cloneRequest(b.Request, b.RequestBody)

	req.Header.Add("X-Request-ID",id)
	req.Header.Add("X-Benchmark", "doom")

	trace := &httptrace.ClientTrace{

		GotConn: func(connInfo httptrace.GotConnInfo) {
			RStart = time.Now()
		},
		WroteRequest: func(w httptrace.WroteRequestInfo) {
			reqDuration = now() - (RStart.Sub(startTime))
		},
		GotFirstResponseByte: func() {
			resStart = time.Now()
		},
	}

	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	resp, err := c.Do(req)
	log.Debugf("%s done transport delay:%s first byte:%s",id,reqDuration,RStart.Sub(resStart))
	var result fact.Trace
	if err == nil {
		size = resp.ContentLength
		code = resp.StatusCode
		log.Debugf("got %d with %d bytes",code,size)
		result = readTraceFromHttpResponse(resp)
	}
	REnd := time.Now()
	resDuration := REnd.Sub(resStart)

	if result.ID == "" {
		result.ID = id
	}
	if result.Timestamp == nil || !result.Timestamp.IsValid() || result.Timestamp.GetSeconds() <= 1 {
		result.Timestamp = timestamppb.New(RStart)
	}
	result.RequestStartTime = timestamppb.New(RStart)
	result.Status = int32(code)
	result.RequestEndTime = timestamppb.New(REnd)
	result.RequestResponseLatency = durationpb.New(resDuration)

	return &result
}

func readTraceFromHttpResponse(resp *http.Response) fact.Trace {
	var result fact.Trace
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Debugf("failed to read resp body %f", err)
	}
	err = json.Unmarshal(body, &result)
	if err != nil {
		log.Debugf("failed to read resp body %f", err)
	}
	resp.Body.Close()

	return result
}

// cloneRequest returns a clone of the provided *http.Request.
// The clone is a shallow copy of the struct and its Header map.
// Original form github.com/rakyll/hey
func cloneRequest(r *http.Request, body []byte) *http.Request {
	// shallow copy of the struct
	r2 := new(http.Request)
	*r2 = *r
	// deep copy of the Header
	r2.Header = make(http.Header, len(r.Header))
	for k, s := range r.Header {
		r2.Header[k] = append([]string(nil), s...)
	}
	if len(body) > 0 {
		r2.Body = ioutil.NopCloser(bytes.NewReader(body))
	}
	return r2
}
