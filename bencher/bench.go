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
	"io"
	"os"
	"sync"
	"time"

	"github.com/faas-facts/fact/fact"
)

type Bencher struct {
	//Some sort of Logger/Writer
	//Worker pool
	Work       Workload
	outputfile io.WriteCloser
	results    *fact.ResultCollector
	Strict     bool
}

func (b *Bencher) openOutput() io.WriteCloser {
	if b.outputfile != nil {
		return b.outputfile
	}

	filename := fmt.Sprintf("%s_%s.csv", b.Work.Name, time.Now().Format("2006_01_02"))
	log.Infof("output not set, using %s", filename)

	resultFile, err := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatalf("could not open result file %f", err)
		return nil
	}
	b.outputfile = resultFile
	return resultFile
}

func (b *Bencher) Run() {
	resultFile := b.outputfile
	if resultFile == nil {
		log.Error("output file not present!")
		panic("output file not present!")
	}

	b.results = fact.NewCollector()
	writer := fact.NewCSVWriter()
	writer.Open(resultFile, false)

	//start periodic write to relax memory needs
	ticker := time.NewTicker(time.Second * 30)
	go func() {
		for range ticker.C {
			err := b.results.Write(writer)
			if err != nil {
				log.Errorf("failed to write results! %f", err)
			}
		}
	}()

	if b.Work.PreRun != nil {
		err := b.Work.PreRun()
		if err != nil {
			log.Error("failed to perform pre run")
		}
	}

	for i, phase := range b.Work.Phases {
		log.Infof("running phase %d", i)
		err := phase.run(b)
		if err != nil {
			log.Errorf("error in phase %d - %f", i, err)
			if b.Strict {
				return
			}
		}

	}
	if b.Work.PostRun != nil {
		err := b.Work.PostRun()
		if err != nil {
			log.Error("failed to perform post run")
		}
	}

	err := b.results.Write(writer)
	if err != nil {
		log.Errorf("failed to write results to disk - %f", err)
		log.Error(b.results.GetTraces())
	}

}

func (p *Phase) run(b *Bencher) error {
	if p.PreRun != nil {
		log.Infof("run pre-phase %s", p.Name)
		err := p.PreRun()
		if err != nil {
			log.Errorf("failed to perform pre run in phase %s", p.Name)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signal, err := p.HatchRate.Setup(ctx, p)
	if err != nil {
		log.Errorf("failed to setup hatch rate for phase %s", p.Name)
		return err
	}

	err = p.Invocation.Setup(p, b)
	if err != nil {
		log.Errorf("failed to setup invoker for phase %s", p.Name)
		return err
	}

	for i := 0; i < p.Threads; i++ {

		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				default:
					err := p.Invocation.Exec(p.HatchRate)
					if err != nil {
						if b.Strict {
							p.HatchRate.Close()
							log.Errorf("invocation failed - %f", err)
							cancel()
						}
					}
				}

			}
		}()
	}

	waitOn(signal, &p.Timeout)
	cancel()

	err = p.HatchRate.Close()
	if err != nil {
		log.Errorf("failed to close hatch rate %s", p.Name)
		return err
	}

	if p.PostRun != nil {
		log.Infof("run post-phase %s", p.Name)
		err := p.PostRun()
		if err != nil {
			log.Errorf("failed to perform pre run in phase %s", p.Name)
		}
	}

	return nil
}

func waitOn(signal *sync.Cond, timeout *time.Duration) {
	if signal == nil && timeout == nil {
		return
	}
	returnChan := make(chan struct{}, 2)
	if signal != nil {
		go func() {
			signal.Wait()
			returnChan <- struct{}{}
		}()
	}
	if timeout != nil {
		go func() {
			<-time.After(*timeout)
			returnChan <- struct{}{}
		}()
	}

	<-returnChan
}
