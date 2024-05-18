package main

import (
	"context"
	"errors"
	"net"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/sula7/iot-reader/internal/packet"
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

	reconnectCh := make(chan struct{})
	var (
		reconnectRetryCount     = 3
		reconnectRetryDelay     = 0 * time.Second
		reconnectRetryDelayStep = 5 * time.Second
	)

RECONN:
	for n := range reconnectRetryCount {
		time.Sleep(reconnectRetryDelay)

		conn, err := net.DialTimeout("tcp", dialAddress, 10*time.Second)
		if err != nil {
			log.Error().Err(err).Msg("failed to dial tcp")
			reconnectRetryDelay += reconnectRetryDelayStep
			log.Debug().Int("retry_no", n).
				Msgf("reconnecting to the server in %s seconds", reconnectRetryDelay)
			continue
		}

		log.Debug().Str("remote_address", conn.RemoteAddr().String()).
			Str("local_address", conn.LocalAddr().String()).Msg("connected")

		ctx, cancel := context.WithCancel(context.Background())
		go writePing(ctx, conn, reconnectCh)

		dataCh := make(chan []byte)
		go responseReceiver(ctx, conn, dataCh, reconnectCh)

		for {
			select {
			case <-reconnectCh:
				cancel()

				if err := conn.Close(); err != nil {
					log.Error().Err(err).Msg("failed to close conn")
				}

				reconnectRetryDelay += reconnectRetryDelayStep
				log.Debug().Int("retry_no", n).
					Msgf("reconnecting to the server in %s seconds", reconnectRetryDelay)
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

func writePing(ctx context.Context, conn net.Conn, reconnectCh chan<- struct{}) {
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
			log.Debug().Msg("ctx cancelled pinger")
			return
		default:
		}

		log.Debug().Msg("ping")
		if err := pkt.Write(conn); err != nil {
			faultyPingCount++
			if errors.Is(err, unix.EPIPE) && faultyPingCount == 5 {
				log.Debug().Msg("faulty ping write count reached")
				reconnectCh <- struct{}{}
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

// TODO: this func consumes a lot of CPU time. Looks like for loop and io.IOF are bad
func responseReceiver(ctx context.Context, conn net.Conn, dataCh chan<- []byte, reconnectCh chan<- struct{}) {
	for {
		select {
		case <-ctx.Done():
			log.Debug().Msg("ctx cancelled responser")
			return
		default:
		}

		buf := make([]byte, 14)
		n, err := conn.Read(buf)
		if err != nil {
			if n == 0 {
				log.Debug().Str("remote_address", conn.RemoteAddr().String()).
					Str("local_address", conn.LocalAddr().String()).Msg("disconnected")
				reconnectCh <- struct{}{}
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
