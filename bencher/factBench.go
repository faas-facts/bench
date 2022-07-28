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
	"fmt"
	"gopkg.in/yaml.v3"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"time"
)

func BencherFromConfigFile(configFile io.ReadCloser) (*Bencher,error) {
	var config BenchmarkConfig

	data, err := ioutil.ReadAll(configFile)
	defer configFile.Close()
	if err != nil {
		return nil,err
	}

	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil,err
	}

	return BencherFromConfig(config)
}

func BencherFromConfig(config BenchmarkConfig) (*Bencher,error) {

	if config.OutputFile == "" {
		return nil, fmt.Errorf("config dose not contain an output file")
	}

	var outfile = config.OutputFile
	if strings.Contains(config.OutputFile,"$date"){
		outfile = strings.Replace(config.OutputFile,"$date",time.Now().Format("2006_01_02"),-1)
	}



	workload, err := config.Workload.Unmarshal()
	if err != nil {
		return nil,err
	}

	if strings.Contains(config.OutputFile,"$name"){
		outfile = strings.Replace(config.OutputFile,"$name",workload.Name,-1)
	}

	//check if file exsist or can be created
	out, err := os.OpenFile(outfile, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0664)
	if err != nil {
		return nil,err
	}

	return &Bencher{
		Work:       workload,
		outputfile: out,
		Strict:     false,
	},nil
}

func WithPreRun(bencher *Bencher,runFunc PreRunFunc) *Bencher {
	if bencher == nil {
		return nil
	}
	bencher.Work.PreRun = runFunc
	return bencher
}

func WithPostRun(bencher *Bencher,runFunc PostRunFunc) *Bencher {
	if bencher == nil {
		return nil
	}
	bencher.Work.PostRun = runFunc
	return bencher
}

func WithPayloadFunc(bencher *Bencher, payloadFunc PayloadFunc) *Bencher{
	if bencher == nil {
		return nil
	}
	for _, phase := range bencher.Work.Phases {
		phase.PayloadFunc = payloadFunc
	}
	return bencher
}

func WithPhasePreRun(phaseIndex int,bencher *Bencher, runFunc PreRunFunc) *Bencher{
	if bencher == nil {
		return nil
	}

	if phaseIndex >= 0 && phaseIndex < len(bencher.Work.Phases) {
		bencher.Work.Phases[phaseIndex].PreRun = runFunc
	}

	return bencher
}


func WithPhasePostRun(phaseIndex int,bencher *Bencher, runFunc PostRunFunc) *Bencher{
	if bencher == nil {
		return nil
	}

	if phaseIndex >= 0 && phaseIndex < len(bencher.Work.Phases) {
		bencher.Work.Phases[phaseIndex].PostRun = runFunc
	}

	return bencher
}