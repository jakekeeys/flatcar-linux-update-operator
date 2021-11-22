// Copyright 2015 CoreOS, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package updateengine

import (
	"fmt"
	"os"
	"strconv"

	dbus "github.com/godbus/dbus/v5"
)

const (
	dbusPath      = "/com/coreos/update1"
	dbusInterface = "com.coreos.update1.Manager"
	dbusMember    = "StatusUpdate"
	signalBuffer  = 32 // TODO(bp): What is a reasonable value here?
)

// Client allows reading update-engine status using D-Bus.
//
// New instance should be initialized using New() function.
//
// When finished using this object, Close() should be called to close D-Bus connection.
type Client struct {
	conn   *dbus.Conn
	object dbus.BusObject
	ch     chan *dbus.Signal
}

// New creates new instance of Client and initializes it.
func New() (*Client, error) {
	conn, err := dbus.SystemBusPrivate()
	if err != nil {
		return nil, fmt.Errorf("opening private connection to system bus: %w", err)
	}

	methods := []dbus.Auth{dbus.AuthExternal(strconv.Itoa(os.Getuid()))}

	if err := conn.Auth(methods); err != nil {
		// Best effort closing the connection.
		_ = conn.Close()

		return nil, fmt.Errorf("authenticating to system bus: %w", err)
	}

	if err := conn.Hello(); err != nil {
		// Best effort closing the connection.
		_ = conn.Close()

		return nil, fmt.Errorf("sending hello to system bus: %w", err)
	}

	// Setup the filter for the StatusUpdate signals.
	match := fmt.Sprintf("type='signal',interface='%s',member='%s'", dbusInterface, dbusMember)

	if call := conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, match); call.Err != nil {
		return nil, fmt.Errorf("adding filter: %w", call.Err)
	}

	ch := make(chan *dbus.Signal, signalBuffer)
	conn.Signal(ch)

	return &Client{
		ch:     ch,
		conn:   conn,
		object: conn.Object("com.coreos.update1", dbus.ObjectPath(dbusPath)),
	}, nil
}

// Close closes internal D-Bus connection.
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}

	return nil
}

// ReceiveStatuses receives signal messages from dbus and sends them as Statues
// on the rcvr channel, until the stop channel is closed. An attempt is made to
// get the initial status and send it on the rcvr channel before receiving
// starts.
func (c *Client) ReceiveStatuses(rcvr chan<- Status, stop <-chan struct{}) {
	// If there is an error getting the current status, ignore it and just
	// move onto the main loop.
	st, _ := c.getStatus()
	rcvr <- st

	for {
		select {
		case <-stop:
			return
		case signal := <-c.ch:
			rcvr <- NewStatus(signal.Body)
		}
	}
}

// getStatus gets the current status from update_engine.
func (c *Client) getStatus() (Status, error) {
	call := c.object.Call(dbusInterface+".GetStatus", 0)
	if call.Err != nil {
		return Status{}, call.Err
	}

	return NewStatus(call.Body), nil
}
