package dash

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// FtcDashboard talks to its web UI over a WebSocket and nothing else, so that
// is the only way to ask it what it currently holds. The client here is written
// out rather than pulled in: epsh needs one text frame out and one back, and
// a dependency for that is not worth carrying.

// Port is where the dashboard listens on the robot.
const Port = 8000

const magic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// conn is an open WebSocket.
type conn struct {
	net  net.Conn
	read *bufio.Reader
}

// dial opens a WebSocket to the dashboard at addr, which is host:port.
func dial(addr string, timeout time.Duration) (*conn, error) {
	socket, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, fmt.Errorf("cannot reach the dashboard at %s: %w", addr, err)
	}

	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		socket.Close()
		return nil, err
	}
	nonce := base64.StdEncoding.EncodeToString(key)

	request := "GET / HTTP/1.1\r\n" +
		"Host: " + addr + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + nonce + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"

	socket.SetDeadline(time.Now().Add(timeout))
	if _, err := io.WriteString(socket, request); err != nil {
		socket.Close()
		return nil, err
	}

	read := bufio.NewReader(socket)
	resp, err := http.ReadResponse(read, nil)
	if err != nil {
		socket.Close()
		return nil, fmt.Errorf("the dashboard did not answer: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		socket.Close()
		return nil, fmt.Errorf("the dashboard refused the connection: %s", resp.Status)
	}

	sum := sha1.Sum([]byte(nonce + magic))
	if resp.Header.Get("Sec-WebSocket-Accept") != base64.StdEncoding.EncodeToString(sum[:]) {
		socket.Close()
		return nil, fmt.Errorf("something on port %d is not the dashboard", Port)
	}

	return &conn{net: socket, read: read}, nil
}

func (c *conn) Close() error { return c.net.Close() }

func (c *conn) deadline(at time.Time) { c.net.SetDeadline(at) }

// opcodes used here. Continuation and binary frames are read but nothing else
// is ever sent.
const (
	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA
)

// send writes one masked text frame. Clients must mask; servers must not.
func (c *conn) send(payload string) error {
	body := []byte(payload)

	var header []byte
	header = append(header, 0x80|opText)

	switch n := len(body); {
	case n < 126:
		header = append(header, 0x80|byte(n))
	case n < 1<<16:
		header = append(header, 0x80|126, 0, 0)
		binary.BigEndian.PutUint16(header[len(header)-2:], uint16(n))
	default:
		header = append(header, 0x80|127, 0, 0, 0, 0, 0, 0, 0, 0)
		binary.BigEndian.PutUint64(header[len(header)-8:], uint64(n))
	}

	mask := make([]byte, 4)
	if _, err := rand.Read(mask); err != nil {
		return err
	}
	header = append(header, mask...)

	masked := make([]byte, len(body))
	for i, b := range body {
		masked[i] = b ^ mask[i%4]
	}

	if _, err := c.net.Write(append(header, masked...)); err != nil {
		return fmt.Errorf("cannot ask the dashboard: %w", err)
	}
	return nil
}

// receive returns the next complete text or binary message.
//
// Fragmented messages are reassembled and control frames are handled in place,
// because the dashboard streams telemetry constantly and the answer being
// waited for arrives somewhere in the middle of it.
func (c *conn) receive() (string, error) {
	var message strings.Builder

	for {
		final, opcode, payload, err := c.frame()
		if err != nil {
			return "", err
		}

		switch opcode {
		case opPing:
			// Pong carries the ping's payload back, and the dashboard drops a
			// client that stops answering.
			if err := c.control(opPong, payload); err != nil {
				return "", err
			}
			continue

		case opPong:
			continue

		case opClose:
			return "", fmt.Errorf("the dashboard closed the connection")

		case opText, opBinary, opContinuation:
			message.Write(payload)
			if final {
				return message.String(), nil
			}
		}
	}
}

// control writes a masked control frame, which is never fragmented.
func (c *conn) control(opcode byte, payload []byte) error {
	if len(payload) > 125 {
		payload = payload[:125]
	}

	mask := make([]byte, 4)
	if _, err := rand.Read(mask); err != nil {
		return err
	}

	frame := []byte{0x80 | opcode, 0x80 | byte(len(payload))}
	frame = append(frame, mask...)
	for i, b := range payload {
		frame = append(frame, b^mask[i%4])
	}

	_, err := c.net.Write(frame)
	return err
}

// frame reads one frame off the wire.
func (c *conn) frame() (final bool, opcode byte, payload []byte, err error) {
	var head [2]byte
	if _, err := io.ReadFull(c.read, head[:]); err != nil {
		return false, 0, nil, fmt.Errorf("the dashboard stopped talking: %w", err)
	}

	final = head[0]&0x80 != 0
	opcode = head[0] & 0x0F
	masked := head[1]&0x80 != 0

	length := uint64(head[1] & 0x7F)
	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(c.read, ext[:]); err != nil {
			return false, 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(c.read, ext[:]); err != nil {
			return false, 0, nil, err
		}
		length = binary.BigEndian.Uint64(ext[:])
	}

	// A server frame is never masked, so a huge length here means the stream
	// has desynchronised rather than that a huge message is coming.
	if length > maxMessage {
		return false, 0, nil, fmt.Errorf("the dashboard sent an implausible %d byte frame", length)
	}

	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(c.read, mask[:]); err != nil {
			return false, 0, nil, err
		}
	}

	payload = make([]byte, length)
	if _, err := io.ReadFull(c.read, payload); err != nil {
		return false, 0, nil, fmt.Errorf("the dashboard cut a message short: %w", err)
	}

	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}

	return final, opcode, payload, nil
}

// maxMessage caps what will be read into memory. A config tree is kilobytes;
// camera frames are the large thing on this socket and are not wanted.
const maxMessage = 32 << 20
