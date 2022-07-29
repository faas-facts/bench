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
	"fmt"
	"io"
	"strings"
)

// askForConfirmation asks the user for confirmation.
//A user must type in "yes" or some similar confirmation, no by default
func AskForConfirmation(s string, in io.Reader) bool {
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
