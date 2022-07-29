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
	"strings"
)

type Invoker interface {
	Setup(context.Context, *Phase, *Bencher) error
	Exec(rate HatchRate) error
}

type FunctionAPIInvoker interface {
	Invoker
}

var _invokerTypes = []string{"http", "ow"}

type InvokerConstructor func(config InvokerConfig) (Invoker, error)

var _invoker = make(map[string]InvokerConstructor)

//Extension Method to register more invoker used during config parsing
func RegisterInvoker(name string, constructor InvokerConstructor) error {
	for _, k := range _invokerTypes {
		if name == k {
			return fmt.Errorf("cannot use %s to register a HatchRate", name)
		}
	}
	_invoker[name] = constructor
	return nil
}

type InvokerConfig struct {
	Options map[string]interface{} `yaml:",inline"`
	Type    string                 `yaml:"type"`
}

func NewInvokerFromConfig(config InvokerConfig) (Invoker, error) {
	_type := strings.TrimSpace(strings.ToLower(config.Type))
	switch _type {
	case "http":
		return newHttpInvoker(config)
	case "ow":
		return newOpenWhiskInvoker(config)
	}

	if val, ok := _invoker[_type]; ok {
		return val(config)
	}

	return nil, fmt.Errorf("unknown rate type")
}
