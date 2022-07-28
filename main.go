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

package main

import (
	"bufio"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/faas-facts/bench/bencher"

	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	prefixed "github.com/x-cray/logrus-prefixed-formatter"
)

const LICENCE_TEXT = "Copyright (C) 2021 Sebastian Werner\nThis program comes with ABSOLUTELY NO WARRANTY; GNU GPLv3"

var (
	Build string
)

var logger = logrus.New()
var log *logrus.Entry

func init() {
	if Build == "" {
		Build = "Debug"
	}
	logger.Formatter = new(prefixed.TextFormatter)
	logger.SetLevel(logrus.DebugLevel)
	log = logger.WithFields(logrus.Fields{
		"prefix": "factBench",
		"build":  Build,
	})
}

func setup() {
	fmt.Println(LICENCE_TEXT)

	viper.SetConfigName("factBench")
	viper.AddConfigPath(".")

	//setup defaults
	viper.SetDefault("verbose", true)
	viper.SetDefault("unattended", false)
	viper.SetDefault("workload", "examples/workload.yml")

	//setup cmd interface
	flag.Bool("verbose", false, "for verbose logging")
	flag.String("workload", "workloads/b0.yml", "the workload descriptor file")
	flag.Bool("y", false, "run without waiting for user confirmation")

	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	viper.RegisterAlias("y", "unattended")

	err := viper.BindPFlags(pflag.CommandLine)
	if err != nil {
		log.Errorf("error parsing flags %+v", err)
	}

	if viper.GetBool("verbose") {
		logger.SetLevel(logrus.DebugLevel)
	}
}

func main() {
	setup()
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	runtime.GOMAXPROCS(runtime.NumCPU())

	if viper.GetBool("verbose") {
		logger.SetLevel(logrus.DebugLevel)
		bencher.SetDefaultLogger(log)
	}

	wlfp := viper.GetString("workload")
	config, err := os.Open(wlfp)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to open workload - %+v", err)
		os.Exit(-1)
	}

	bench, err := bencher.BencherFromConfig(config)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to create workload from %s - %+v", wlfp, err)
		os.Exit(-1)
	}

	fmt.Println("Using the following workload:")
	fmt.Println(bench.Work)

	//TODO: implement cost/request estimation

	if !viper.GetBool("unattended") {
		if !askForConfirmation("Do you want to continue with this benchmark?", os.Stdin) {
			os.Exit(0)
		}
	}

	start := time.Now()
	bench.Run()

	fmt.Printf("Benchmark completed in %s\n", time.Now().Sub(start))
}

// askForConfirmation asks the user for confirmation.
//A user must type in "yes" or some similar confirmation, no by default
func askForConfirmation(s string, in io.Reader) bool {
	reader := bufio.NewReader(in)

	fmt.Printf("%s [y/N]: ", s)

	response, err := reader.ReadString('\n')
	if err != nil {
		log.Fatal(err)
		return false
	}

	response = strings.ToLower(strings.TrimSpace(response))
	switch response {
	case "y":
		fallthrough
	case "yes":
		fallthrough
	case "fuck yeah":
		return true
	}

	return false

}
