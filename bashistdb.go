// Copyright (c) 2015, Marios Andreopoulos.
//
// This file is part of bashistdb.
//
// 	Bashistdb is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// 	Bashistdb is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// 	You should have received a copy of the GNU General Public License
// along with bashistdb.  If not, see <http://www.gnu.org/licenses/>.

// Command bashistdb stores and retrieves bash history into/from a sqlite3
// database.
package main

import (
	"fmt"
	"os"

	conf "projects.30ohm.com/mrsaccess/bashistdb/configuration"
	"projects.30ohm.com/mrsaccess/bashistdb/llog"
	"projects.30ohm.com/mrsaccess/bashistdb/local"
	"projects.30ohm.com/mrsaccess/bashistdb/network"
)

var log *llog.Logger

func main() {
	log = conf.Log

	switch conf.Mode {
	case conf.PRINT_VERSION:
		fmt.Println("bashistdb v" + version)
		fmt.Println("https://github.com/andmarios/bashistdb")
		os.Exit(0)
	case conf.SERVER:
		if err := network.ServerMode(); err != nil {
			log.Fatalln(err)
		}
		os.Exit(0)
	case conf.CLIENT:
		if err := network.ClientMode(); err != nil {
			log.Fatalln(err)
		}
		os.Exit(0)
	case conf.LOCAL:
		if err := local.Run(); err != nil {
			log.Fatalln(err)
		}
	}
}
