package main

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"os/signal"
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

	ctx, cancel := context.WithCancel(context.Background())
	go signalListener(cancel)
	go writePing(ctx, conn)

	bufCh := make(chan []byte)

	// response packet reader
	for {
		go func() {
			buf := make([]byte, 14)
			_, err := io.ReadFull(conn, buf)
			if err != nil {
				log.Error().Err(err).Msg("failed to read")
				return
			}

			if len(buf) < 10 {
				log.Error().Uints8("buf", buf).Msg("packet len is below required")
				return
			}

			bufCh <- buf
		}()

		select {
		case <-ctx.Done():
			log.Debug().Msg("reader done")
			return
		case buf := <-bufCh:
			switch buf[1] {
			case packetTypePong:
				log.Debug().Msg("pong")
			default:
				log.Error().Uints8("buf", buf).Msg("unknown packet type")
			}
		}
	}
}

func writePing(ctx context.Context, conn net.Conn) {
	ticker := time.NewTicker(5 * time.Second)
	var faultPingCount int

	for {
		if faultPingCount == 10 {
			if err := conn.Close(); err != nil {
				log.Fatal().Err(err).Msg("failed to close conn")
			}
			log.Fatal().Msg("faulty ping limit exceeded")
		}

		select {
		case <-ctx.Done():
			log.Debug().Msg("write ping done")
			ticker.Stop()
			return
		case <-ticker.C:
			header := make([]byte, 4)
			header[0] = protocolVersion1
			header[1] = packetTypePing

			body := make([]byte, 10)

			packet := append(header, body...)
			log.Debug().Msg("ping")

			_, err := conn.Write(packet)
			if err != nil {
				faultPingCount++
				if !errors.Is(err, unix.EPIPE) { // ignore broken pipe error on server fault
					log.Error().Err(err).Msg("failed to write ping packet")
					continue
				}
			}
		}
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

func signalListener(cancel context.CancelFunc) {
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, unix.SIGINT, unix.SIGTERM, unix.SIGKILL, unix.SIGSTOP)

	sig := <-signalCh
	log.Info().Str("signal", sig.String()).Msg("signal received")
	cancel()
}
