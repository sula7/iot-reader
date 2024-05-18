package packet

import (
	"fmt"
	"net"
	"time"

	"github.com/rs/zerolog/log"
)

type PacketV1 struct {
	Header     []byte
	Payload    []byte
	RetryCount int
	RetryDelay time.Duration
}

func NewV1(header []byte, payload []byte) *PacketV1 {
	return &PacketV1{
		Header:  header,
		Payload: payload,
	}
}

func (p *PacketV1) SetRetryCount(count int) {
	p.RetryCount = count
}

func (p *PacketV1) SetRetryDelay(delay time.Duration) {
	p.RetryDelay = delay
}

// Write writes packet into connection. Serves both options with delay and without
func (p *PacketV1) Write(conn net.Conn) error {
	if p.RetryCount == 0 {
		return p.write(conn)
	}

	for retry := range p.RetryCount {
		err := p.write(conn)
		if err == nil {
			break
		}

		if retry == p.RetryCount {
			return fmt.Errorf("failed to write packet after %n retries: %w", retry, err)
		}

		log.Debug().Msg("faulty packet, sleep and retry")

		time.Sleep(p.RetryDelay)
	}

	return nil
}

func (p *PacketV1) write(conn net.Conn) error {
	pkt := append(p.Header, p.Payload...)
	_, err := conn.Write(pkt)
	return err
}
