package bench

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// ServePort is the default TCP port for `ptop serve`.
const ServePort = 5330

var serveMagic = [2]byte{'P', 1}

// Serve runs the ptop throughput server until ctx is cancelled. One connection
// at a time is all that matters for a point-to-point link test, but concurrent
// clients are handled anyway.
func Serve(ctx context.Context, addr string) error {
	if addr == "" {
		addr = fmt.Sprintf(":%d", ServePort)
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer ln.Close()
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return err
			}
		}
		go serveConn(conn.(*net.TCPConn))
	}
}

// Protocol (client-driven, one TCP connection):
//  1. client -> server: 6-byte header  [ 'P', 1, uint32 BE seconds ]
//  2. client sends a zero stream for `seconds`, then half-closes (CloseWrite)
//  3. server -> client: uint64 BE = bytes it received in step 2
//  4. server sends a zero stream for `seconds`, then closes
//  5. client reads until EOF and times it
func serveConn(conn *net.TCPConn) {
	defer conn.Close()
	var hdr [6]byte
	conn.SetDeadline(time.Now().Add(15 * time.Second))
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return
	}
	if hdr[0] != serveMagic[0] || hdr[1] != serveMagic[1] {
		return
	}
	secs := binary.BigEndian.Uint32(hdr[2:6])
	if secs == 0 || secs > 60 {
		secs = 10
	}
	window := time.Duration(secs)*time.Second + 30*time.Second

	// Phase 1: receive the client's upload until it half-closes.
	conn.SetDeadline(time.Now().Add(window))
	n, _ := io.Copy(io.Discard, conn)
	var cnt [8]byte
	binary.BigEndian.PutUint64(cnt[:], uint64(n))
	if _, err := conn.Write(cnt[:]); err != nil {
		return
	}

	// Phase 2: send our own stream for `secs`.
	conn.SetDeadline(time.Now().Add(window))
	buf := make([]byte, 128*1024)
	deadline := time.Now().Add(time.Duration(secs) * time.Second)
	for time.Now().Before(deadline) {
		if _, err := conn.Write(buf); err != nil {
			return
		}
	}
}

// lanThroughput runs the client side of the protocol against host:port and
// returns download and upload throughput in Mbit/s.
func lanThroughput(ctx context.Context, host string, port, secs int) (downMbit, upMbit float64, err error) {
	if secs < 3 {
		secs = 3
	}
	if secs > 15 {
		secs = 15
	}
	if port == 0 {
		port = ServePort
	}
	d := net.Dialer{Timeout: 4 * time.Second}
	c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return 0, 0, err
	}
	conn := c.(*net.TCPConn)
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(time.Duration(2*secs)*time.Second + 30*time.Second))

	var hdr [6]byte
	hdr[0], hdr[1] = serveMagic[0], serveMagic[1]
	binary.BigEndian.PutUint32(hdr[2:], uint32(secs))
	if _, err = conn.Write(hdr[:]); err != nil {
		return 0, 0, err
	}

	// Phase 1: upload.
	buf := make([]byte, 128*1024)
	upStart := time.Now()
	upEnd := upStart.Add(time.Duration(secs) * time.Second)
	for time.Now().Before(upEnd) {
		if _, werr := conn.Write(buf); werr != nil {
			return 0, 0, werr
		}
	}
	upElapsed := time.Since(upStart).Seconds()
	_ = conn.CloseWrite()

	var cnt [8]byte
	if _, err = io.ReadFull(conn, cnt[:]); err != nil {
		return 0, 0, err
	}
	if upElapsed > 0 {
		upMbit = float64(binary.BigEndian.Uint64(cnt[:])) * 8 / upElapsed / 1e6
	}

	// Phase 2: download until EOF.
	downStart := time.Now()
	nd, _ := io.Copy(io.Discard, conn)
	downElapsed := time.Since(downStart).Seconds()
	if downElapsed > 0 {
		downMbit = float64(nd) * 8 / downElapsed / 1e6
	}
	return downMbit, upMbit, nil
}
