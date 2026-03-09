package gostratum

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func spawnClientListener(ctx *StratumContext, connection net.Conn, s *StratumListener) error {
	defer ctx.Disconnect()

	for {
		err := readFromConnection(connection, func(line string) error {
			event, err := UnmarshalEvent(line)
			if err != nil {
				// Demote parse failures from Error -> Debug
				ctx.Logger.Debug("failed to parse client line, ignoring", zap.String("raw", line))
				return err
			}
			return s.HandleEvent(ctx, event)
		})
		if errors.Is(err, os.ErrDeadlineExceeded) {
			continue // expected timeout
		}
		if ctx.Err() != nil {
			return ctx.Err() // context cancelled
		}
		if ctx.parentContext.Err() != nil {
			return ctx.parentContext.Err() // parent context cancelled
		}
		if err != nil { // actual error
			// Demote socket read errors to Debug to reduce spam
			ctx.Logger.Debug("error reading from socket", zap.Error(err))
			return err
		}
	}
}

type LineCallback func(line string) error

func readFromConnection(connection net.Conn, cb LineCallback) error {
	deadline := time.Now().Add(5 * time.Second).UTC()
	if err := connection.SetReadDeadline(deadline); err != nil {
		return err
	}

	buffer := make([]byte, 8096*2)
	n, err := connection.Read(buffer)
	if err != nil && !(err == io.EOF && n > 0) {
		// If no bytes were read, treat this as a real error.
		// Wrap and return so caller can decide how to handle it.
		return errors.Wrapf(err, "error reading from connection")
	}

	// Truncate to the number of bytes actually read
	if n > 0 {
		buffer = buffer[:n]
	} else {
		buffer = buffer[:0]
	}

	// remove NULs and scan only the bytes we received
	buffer = bytes.ReplaceAll(buffer, []byte("\x00"), nil)
	scanner := bufio.NewScanner(strings.NewReader(string(buffer)))
	for scanner.Scan() {
		if err := cb(scanner.Text()); err != nil {
			return err
		}
	}
	// If scanner encounters an error, return it (optional)
	if err := scanner.Err(); err != nil {
		return errors.Wrapf(err, "scanner error")
	}
	return nil
}
