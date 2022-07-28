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


func flagValue(key string, options map[string]interface{}, defaultValue bool) bool {
	if val,ok := options[key]; ok {
		return val.(bool)
	} else {
		return defaultValue
	}
}

func checkFields(options map[string]interface{}, keys ... string) bool {
	for _, key := range keys {
		if _,ok := options[key]; !ok {
			return false
		}
	}
	return true
}



