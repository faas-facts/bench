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
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/apache/openwhisk-client-go/whisk"
	"github.com/faas-facts/fact/fact"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)
var whiskPropsPath string

func init() {
	home, err := os.UserHomeDir()
	if err == nil {
		whiskPropsPath = filepath.Join(home, ".wskprops")
	} else {
		//best effort this will prop. Work on unix and osx ;)
		whiskPropsPath = filepath.Join("~", ".wskprops")
	}
}

type WhiskInvoker struct {
	FunctionName string

	RequestPerMinute int64

	Host  string
	Token string

	Request interface{}

	results *fact.ResultCollector

	client *whisk.Client
	apiRateLimit *rate.Limiter
	ctx          context.Context
}

func newOpenWhiskInvoker(config InvokerConfig) (Invoker,error){
	if !checkFields(config.Options,  "host","token") {
		return nil, fmt.Errorf("missing values for whisk invoker")
	}

	var rps = int64(60)
	if val,ok := config.Options["rps"];ok {

		rps,ok = val.(int64)
		if !ok {
			return nil,fmt.Errorf("could not read rps %+v form config",val)
		}
		if rps < 0 {
			return nil, fmt.Errorf("rqs must be positive")
		} else if rps > 200 {
			log.Warn("setting rps over 200 can result in openwhisk failures in some instances")
		}
	}

	var function string = ""
	if val,ok := config.Options["function"];ok {
		function = val.(string)
	}

	return &WhiskInvoker{
		FunctionName:     function,
		RequestPerMinute: rps,
		Host:             config.Options["host"].(string),
		Token:            config.Options["token"].(string),
		Request:          nil,
	},nil

}

func (l *WhiskInvoker) Setup(phase *Phase, bencher *Bencher) error {
	err := l.setWhiskClient()
	if err != nil {
		log.Errorf("failed to create whisk client %f",err)
		return err
	}

	l.apiRateLimit = rate.NewLimiter(rate.Every(time.Minute/time.Duration(l.RequestPerMinute)), int(l.RequestPerMinute/60))

	if l.FunctionName == ""{
		l.FunctionName = phase.Target
	}

	if phase.PayloadFunc != nil {
		var payload map[string]interface{}
		err = json.Unmarshal(phase.PayloadFunc(l), &payload)
		if err != nil{
			log.Errorf("failed to create payload %f",err)
			return err
		}
		l.Request = payload
	}

	l.results = bencher.results

	return nil
}

func (l *WhiskInvoker) setWhiskClient() error {
	// lets first check the config
	host := l.Host
	token := l.Token
	var namespace = "_"

	if token == "" {
		//2. check if wskprops exsist
		if _, err := os.Stat(whiskPropsPath); err == nil {
			//. attempt to read and parse props
			if props, err := os.Open(whiskPropsPath); err == nil {
				host, token, namespace = setAtuhFromProps(readProps(props))
			}
			//3. fallback try to check the env for token
		} else {
			host, token, namespace = setAtuhFromProps(readEnviron())
		}
	}

	if token == "" {
		log.Warn("did not find a token for the whisk client!")
	}

	baseurl, _ := whisk.GetURLBase(host, "/api")
	clientConfig := &whisk.Config{
		Namespace:        namespace,
		AuthToken:        token,
		Host:             host,
		BaseURL:          baseurl,
		Version:          "v1",
		Verbose:          true,
		Insecure:         true,
		UserAgent:        "Golang/Smile cli",
		ApigwAccessToken: "Dummy Token",
	}

	client, err := whisk.NewClient(http.DefaultClient, clientConfig)
	if err != nil {
		return err
	}

	l.client = client
	return nil
}

func (l *WhiskInvoker) Exec(rate HatchRate) error {
	invoke, err := l.tryInvoke(l.Request, rate)

	if err != nil {
		return err
	}

	
	l.results.Add(invoke)
	return nil
}

func (l *WhiskInvoker) tryInvoke(invocation interface{},rate HatchRate) (*fact.Trace, error) {
	failures := make([]error, 0)
	RStart := time.Now()
	var REnd time.Time
	for i := 0; i < maxRetries; i++ {
		err := rate.Take()
		if err != nil {
			//wait canceld form the outside
			return nil, err
		}


		invoke, response, err := l.client.Actions.Invoke(l.FunctionName, invocation, true, true)

		if response == nil && err != nil {
			failures = append(failures, err)
			log.Warnf("failed [%d/%d]", i, maxRetries)
			log.Debugf("%+v %d %+v",invoke, response.StatusCode, err)
			rate.OnFailed()
			continue
		}
		REnd = time.Now()
		if response != nil {
			log.Debugf("invoked %s - %d", l.FunctionName, response.StatusCode)
			log.Debugf("%+v", invoke)
			if response.StatusCode == 200 {
				rate.OnSuccess()
				result := readTraceFromHttpResponse(response)
				result.RequestStartTime = timestamppb.New(RStart)
				result.Status = int32(response.StatusCode)
				result.RequestEndTime = timestamppb.New(REnd)
				result.RequestResponseLatency = durationpb.New(REnd.Sub(RStart))
				return &result,nil
			} else if response.StatusCode == 202 {
				if id, ok := invoke["activationId"]; ok {
					result, err := l.pollActivation(id.(string))
					REnd = time.Now()
					if err != nil {
						failures = append(failures, err)
					} else {
						result.RequestStartTime = timestamppb.New(RStart)
						result.RequestEndTime = timestamppb.New(REnd)
						result.RequestResponseLatency = durationpb.New(REnd.Sub(RStart))
						return &result,nil
					}
				}
			} else {
				failures = append(failures, fmt.Errorf("failed to invoke %d %+v", response.StatusCode,response.Body))
				log.Debugf("failed [%d/%d ] times to invoke %s with %+v  %+v %+v", i, maxRetries,
					l.FunctionName, invocation, invoke, response)
			}
		} else {
			log.Debugf("failed [%d/%d]", i, maxRetries)
		}
	}
	REnd = time.Now()

	for _, err := range failures {
		log.Debugf(err.Error())
	}

	return nil, fmt.Errorf("failed request after multiple tries")

}

func (l *WhiskInvoker) pollActivation(activationID string) (fact.Trace, error) {
	//might want to configuer the backof rate?
	backoff := 4
	var result fact.Trace
	wait := func (backoff int) int {
		//results not here yet... keep wating
		<-time.After(time.Second*time.Duration(backoff))
		//exponential backoff of 4,16,64,256,1024 seconds
		backoff = backoff * 4
		log.Debugf("results not ready waiting for %d", backoff)
		return backoff
	}

	log.Debugf("polling Activation %s", activationID)
	for x := 0; x < maxPullRetries; x++ {
		err := l.apiRateLimit.Wait(l.ctx)
		if err != nil {
			return result, err
		}
		invoke, response, err := l.client.Activations.Get(activationID)
		if err != nil || response.StatusCode == 404 {
			backoff = wait(backoff)
			if err != nil {
				log.Debugf("failed to poll %+v",err)
			}
		} else if response.StatusCode == 200 {
			log.Debugf("polled %s successfully",activationID)
			marshal, err := json.Marshal(invoke.Result)

			err = json.Unmarshal(marshal, &result)
			if err == nil {
				result.Status = int32(invoke.StatusCode)
				result.ExecutionLatency = durationpb.New(time.Duration(invoke.Duration))
				result.CodeVersion = invoke.Version
				result.ID = invoke.ActivationID
				return result,nil
			} else {
				return result, fmt.Errorf("failed to fetch activation %s due to %f", activationID, err)
			}
		}
	}
	return result, fmt.Errorf("could not fetch activation after %d ties in %s", maxPullRetries, time.Second*time.Duration(backoff+backoff-1))
}

//check props and env vars for relevant infomation ;)
func setAtuhFromProps(auth map[string]string) (string, string, string) {
	var host string
	var token string
	var namespace string
	if apihost, ok := auth["APIHOST"]; ok {
		host = apihost
	} else if apihost, ok := auth["__OW_API_HOST"]; ok {
		host = apihost
	}
	if apitoken, ok := auth["AUTH"]; ok {
		token = apitoken
	} else if apikey, ok := auth["__OW_API_KEY"]; ok {
		token = apikey
	}
	if apinamespace, ok := auth["NAMESPACE"]; ok {
		namespace = apinamespace
	} else if apinamespace, ok := auth["__OW_NAMESPACE"]; ok {
		namespace = apinamespace
	}
	return host, token, namespace
}

func readProps(in io.ReadCloser) map[string]string {
	defer in.Close()

	props := make(map[string]string)

	reader := bufio.NewScanner(in)

	for reader.Scan() {
		line := reader.Text()
		data := strings.SplitN(line, "=", 2)
		if len(data) < 2 {
			//XXX: This might leek user private data into a log...
			log.Errorf("could not read prop line %s", line)
		}
		props[data[0]] = data[1]
	}
	return props
}

func readEnviron() map[string]string {
	env := make(map[string]string)
	for _, e := range os.Environ() {
		data := strings.SplitN(e, "=", 2)
		if len(data) < 2 {
			//This might leek user private data into a log...
			log.Errorf("could not read prop line %s", e)
		}
		env[data[0]] = data[1]
	}
	return env
}