// Copyright (c) 2015, Marios Andreopoulos.
//
// This file is part of bashistdb.
//
//      Bashistdb is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
//      Bashistdb is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
//      You should have received a copy of the GNU General Public License
// along with bashistdb.  If not, see <http://www.gnu.org/licenses/>.

// Package network provides network functions for bashistdb.
package network

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os"

	"github.com/andmarios/crypto/nacl/saltsecret"

	conf "github.com/andmarios/bashistdb/configuration"
	"github.com/andmarios/bashistdb/database"
	"github.com/andmarios/bashistdb/llog"
)

// Message Types
const (
	RESULT  = "result"
	HISTORY = "history"
	STATS   = "stats"
	QUERY   = "query"
)

type Message struct {
	Type     string
	Payload  []byte
	User     string
	Hostname string
	QParams  conf.QueryParams
}

var log *llog.Logger
var db database.Database

func init() {
	log = conf.Log
}

func ServerMode() error {
	var err error
	db, err = database.New()
	if err != nil {
		return err
	}
	defer db.Close()

	s, err := net.Listen("tcp", conf.Address)
	if err != nil {
		return err
	}
	log.Info.Println("Started listening on:", conf.Address)
	for {
		conn, err := s.Accept()
		if err != nil {
			log.Fatalln(err)
		}
		log.Info.Printf("Connection from %s.\n", conn.RemoteAddr())
		err = db.LogConn(conn.RemoteAddr())
		if err != nil {
			log.Fatalln(err)
		}
		go handleConn(conn)
	}
	//	return nil // go vet doesn't like this...
}

func ClientMode() error {
	log.Debug.Println("Connecting to: ", conf.Address)
	conn, err := net.Dial("tcp", conf.Address)
	if err != nil {
		return err
	}
	defer conn.Close()

	var msg Message

	switch conf.Operation {
	case conf.OP_IMPORT: // If Operation == OP_IMPORT, attempt to read from Stdin
		r := bufio.NewReader(os.Stdin)
		history, err := ioutil.ReadAll(r)
		if err != nil {
			return err
		}

		msg := Message{Type: HISTORY, Payload: history, User: conf.User, Hostname: conf.Hostname}

		if err := encryptDispatch(conn, msg); err != nil {
			return err
		}

		log.Info.Println("Sent history.")

		reply, err := receiveDecrypt(conn)
		if err != nil {
			return err
		}

		switch reply.Type {
		case RESULT:
			log.Info.Println("Received:", string(reply.Payload))
		}
		return nil
	case conf.OP_STATS:
		msg = Message{Type: STATS, User: conf.User, Hostname: conf.Hostname}
	case conf.OP_QUERY:
		msg = Message{Type: QUERY, User: conf.User, Hostname: conf.Hostname, QParams: conf.QParams}
	default:
		return errors.New("unknown function")
	}
	if err := encryptDispatch(conn, msg); err != nil {
		return err
	}
	log.Info.Println("Sent request.")

	reply, err := receiveDecrypt(conn)
	if err != nil {
		return err
	}

	switch reply.Type {
	case RESULT:
		fmt.Println(string(reply.Payload))
	}
	return nil
}

// handleConn is the server code that handles clients (reads message type and performs relevant operation)
func handleConn(conn net.Conn) {
	defer conn.Close()

	msg, err := receiveDecrypt(conn)
	if err != nil {
		log.Info.Println(err, "["+conn.RemoteAddr().String()+"]")
		return
	}

	var result []byte
	switch msg.Type {
	case HISTORY:
		r := bufio.NewReader(bytes.NewReader(msg.Payload))
		res, err := db.AddFromBuffer(r, msg.User, msg.Hostname)
		if err != nil {
			result = []byte(err.Error())
		} else {
			result = []byte(res)
		}
		log.Info.Println("Client sent history: ", res)
	case STATS:
		res1, err := db.TopK(conf.QueryParams{User: "%", Host: "%", Command: "%", Kappa: 20})
		if err != nil {
			log.Fatalln(err)
		}
		res2, err := db.LastK(conf.QueryParams{User: "%", Host: "%", Command: "%", Kappa: 10})
		if err != nil {
			log.Fatalln(err)
		}
		result := res1
		result = append(result, []byte("\n\n")...)
		result = append(result, res2...)
		log.Info.Println("Client asked for some stats.")
	case QUERY:
		result, err = db.RunQuery(msg.QParams)
		if err != nil {
			log.Fatalln(err)
		}
		log.Info.Printf("Client sent query for '%s' as '%s'@'%s', '%s' format.\n",
			msg.QParams.User, msg.QParams.Host, msg.QParams.Command, msg.QParams.Format)
	}

	reply := Message{Type: RESULT, Payload: result}
	if err := encryptDispatch(conn, reply); err != nil {
		log.Println(err)
	}
}

func encryptDispatch(conn net.Conn, m Message) error {
	// We want to sent encrypted data.
	// In order to encrypt, we need to first serialize the message.
	// In order to sent/receive hassle free, we need to serialize the encrypted message
	// So: msg -> [GOB] -> [ENCRYPT] -> [GOB] -> (dispatch)

	// Create encrypter
	var encMsg bytes.Buffer
	encrypter, err := saltsecret.NewWriter(&encMsg, conf.Key, saltsecret.ENCRYPT, true)
	if err != nil {
		return err
	}

	// Serialize message
	enc := gob.NewEncoder(encrypter)
	if err = enc.Encode(m); err != nil {
		return err
	}

	// Flush encrypter to actuall encrypt the message
	if err = encrypter.Flush(); err != nil {
		return err
	}

	// Serialize encrypted message and dispatch it
	dispatch := gob.NewEncoder(conn)
	if err = dispatch.Encode(encMsg.Bytes()); err != nil {
		return err
	}

	return nil
}

func receiveDecrypt(conn net.Conn) (Message, error) {
	// Our work is:
	// (receive) -> [de-GOB] -> [DECRYPT] -> [de-GOB] -> msg

	// Receive data and de-serialize to get the encrypted message
	encMsg := new([]byte)
	receive := gob.NewDecoder(conn)
	if err := receive.Decode(encMsg); err != nil {
		return Message{}, err
	}

	// Create decrypter and pass it the encrypted message
	r := bytes.NewReader(*encMsg)
	decrypter, err := saltsecret.NewReader(r, conf.Key, saltsecret.DECRYPT, false)
	if err != nil {
		return Message{}, err
	}

	// Read unencrypted serialized message and de-serialize it
	msg := new(Message)
	dec := gob.NewDecoder(decrypter)
	if err = dec.Decode(msg); err != nil {
		return Message{}, err
	}

	return *msg, nil
}
