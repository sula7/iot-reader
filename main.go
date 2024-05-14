package main

import (
	"errors"
	"io"
	"net"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
)

// header (4 bytes):
// - protocol version
// - packet type
// - reserved
// - reserved
// body (10 bytes)

const (
	protocolVersion1  uint8 = 1
	packetTypePing    uint8 = 10
	packetTypePong    uint8 = 11
	packetTypePayload uint8 = 20
)

// TODO: to refactor (e.g. move methods under Driver struct)
// TODO: to add re-dialing (restoring) connection after loose

func main() {
	setLogger()

	dialAddress := os.Getenv("DIAL_ADDRESS")

	conn, err := net.DialTimeout("tcp", dialAddress, 10*time.Second)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to dial tcp")
	}

	defer func() {
		log.Debug().Msg("closing connection")
		if err := conn.Close(); err != nil {
			log.Fatal().Err(err).Msg("failed to close conn")
		}
	}()

	go writePing(conn)

	// response packet reader
	for {
		buf := make([]byte, 14)
		n, err := io.ReadFull(conn, buf)
		if err != nil {
			if n == 0 && errors.Is(err, io.EOF) {
				continue
			}

			log.Error().Err(err).Msg("failed to read")
			continue
		}

		if len(buf) < 14 {
			log.Error().Uints8("buf", buf).Msg("packet len is below required")
			continue
		}

		switch buf[1] {
		case packetTypePong:
			log.Debug().Msg("pong")
		default:
			log.Error().Uints8("buf", buf).Msg("unknown packet type")
		}
	}
}

func writePing(conn net.Conn) {
	ticker := time.NewTicker(5 * time.Second)
	var faultPingCount int

	for range ticker.C {
		if faultPingCount == 10 { // TODO: 10 packets sequentially, not in summary
			if err := conn.Close(); err != nil {
				log.Error().Err(err).Msg("failed to close conn")
			}
			log.Fatal().Msg("faulty ping limit exceeded")
		}

		header := make([]byte, 4)
		header[0] = protocolVersion1
		header[1] = packetTypePing

		body := make([]byte, 10)

		packet := append(header, body...)

		_, err := conn.Write(packet)
		if err != nil {
			faultPingCount++
			if !errors.Is(err, unix.EPIPE) { // ignore broken pipe error on server fault
				log.Error().Err(err).Msg("failed to write ping packet")
				continue
			}
		}

		log.Debug().Msg("ping")
	}
}

func setLogger() {
	level, exists := os.LookupEnv("LOG_LEVEL")
	if !exists {
		level = "info"
	}

	zeroLvl, err := zerolog.ParseLevel(level)
	if err != nil {
		zeroLvl = zerolog.InfoLevel
		log.Info().Err(err).Msg("failed to parse log level, using info")
	}

	zerolog.SetGlobalLevel(zeroLvl)
	log.Info().Str("set_level", zeroLvl.String()).Msg("logger is set")
}
