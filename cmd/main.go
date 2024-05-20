package main

import (
	"context"
	"errors"
	"net"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"

	"github.com/sula7/iot-reader/internal/packet"
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

func main() {
	setLogger()

	dialAddress := os.Getenv("DIAL_ADDRESS")

	var (
		reconnectRetryCount     = 4
		reconnectRetryDelay     = 0 * time.Second
		reconnectRetryDelayStep = 5 * time.Second
	)

RECONN:
	for n := range reconnectRetryCount {
		if reconnectRetryDelay > 0 {
			log.Debug().Int("retry_no", n).
				Msgf("reconnecting to the server in %s seconds", reconnectRetryDelay)
			time.Sleep(reconnectRetryDelay)
		}

		conn, err := net.DialTimeout("tcp", dialAddress, 10*time.Second)
		if err != nil {
			log.Error().Err(err).Msg("failed to dial tcp")
			reconnectRetryDelay += reconnectRetryDelayStep
			continue
		}

		log.Debug().Str("remote_address", conn.RemoteAddr().String()).
			Str("local_address", conn.LocalAddr().String()).Msg("connected")

		ctx, cancel := context.WithCancel(context.Background())
		go writePing(ctx, cancel, conn)

		dataCh := make(chan []byte)
		go responseReceiver(ctx, cancel, conn, dataCh)

		for {
			select {
			case <-ctx.Done():
				if err := conn.Close(); err != nil {
					log.Error().Err(err).Msg("failed to close conn")
				}

				reconnectRetryDelay += reconnectRetryDelayStep
				continue RECONN
			case buf := <-dataCh:
				switch buf[1] {
				case packetTypePong:
					log.Debug().Msg("pong")
				default:
					log.Error().Uints8("buf", buf).Msg("unknown packet type")
				}
			}
		}
	}
}

func writePing(ctx context.Context, cancel context.CancelFunc, conn net.Conn) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var faultyPingCount int

	header := make([]byte, 4)
	header[0] = protocolVersion1
	header[1] = packetTypePing
	body := make([]byte, 10)

	pkt := packet.NewV1(header, body)

	for range ticker.C {
		select {
		case <-ctx.Done():
			log.Debug().Msg("ctx done for write ping")
			return
		default:
		}

		log.Debug().Msg("ping")
		if err := pkt.Write(conn); err != nil {
			faultyPingCount++
			if errors.Is(err, unix.EPIPE) && faultyPingCount == 5 {
				log.Debug().Msg("faulty ping write count reached")
				cancel()
				return
			}

			continue
		}

		faultyPingCount = 0
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

func responseReceiver(ctx context.Context, cancel context.CancelFunc, conn net.Conn, dataCh chan<- []byte) {
	for {
		select {
		case <-ctx.Done():
			log.Debug().Msg("ctx done for response receiver")
			return
		default:
		}

		buf := make([]byte, 14)
		n, err := conn.Read(buf)
		if err != nil {
			if n == 0 {
				log.Debug().Str("remote_address", conn.RemoteAddr().String()).
					Str("local_address", conn.LocalAddr().String()).Msg("disconnected")
				cancel()
				return
			}

			log.Error().Err(err).Msg("failed to read")
			continue
		}

		if len(buf) < 14 {
			log.Error().Uints8("buf", buf).Msg("packet len is below required")
			continue
		}

		dataCh <- buf
	}
}
