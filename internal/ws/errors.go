package ws

import "errors"

// ErrNotConnected is returned when attempting to send a message
// while the WebSocket connection is not established.
var ErrNotConnected = errors.New("websocket: not connected")
