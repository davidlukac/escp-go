package escp

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/davidlukac/escp-go/internal/wire"
	"github.com/davidlukac/escp-go/model"
)

// ErrInvalidConfigFile means the reply was not a complete switch configuration file: it did not
// start with the configuration magic, a packet was missing, or two downloads of it differed. The
// message says which. ConfigBackup repeats the download before it returns this.
var ErrInvalidConfigFile = errors.New("escp: reply is not a switch configuration file")

// ConfigBackup downloads the switch's binary configuration file (the same
// bytes as System Tools > Backup Config in the web UI). The vendor utility
// requests it with an authenticated SET carrying an empty TLV 768; the file
// arrives in fragments, which are reassembled. Nothing on the switch changes. Although it is
// a SET on the wire, it is a read: it never passes the write gate, so it works on any firmware,
// like every read, even where writes are refused.
//
// The result is complete or there is none. The replies are UDP: one can be lost, and the
// fragments carry no index, total or checksum (the web UI is different: TCP, with a declared
// Content-Length). A download is therefore accepted only when it is structurally complete (it
// starts with the magic, the last fragment arrived, the packets before it are equally long) and a
// second download, requested separately, is byte for byte the same. Anything
// else is repeated, up to Config.Retries+1 rounds, and then reported as ErrInvalidConfigFile. A
// configuration that changes while it is read fails the same way. A rejection, a missing reply
// and a canceled context end the call at once.
func (c *Client) ConfigBackup(ctx context.Context) (model.ConfigFile, error) {
	if err := c.lock(ctx); err != nil {
		return nil, err
	}
	defer c.mu.Unlock()
	var out []byte
	err := c.withSession(ctx, func() error {
		var err error
		out, err = c.verifiedConfig(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// verifiedConfig downloads the file twice per round until both downloads are complete and equal.
func (c *Client) verifiedConfig(ctx context.Context) ([]byte, error) {
	var last error
	for round := 0; round <= c.cfg.Retries; round++ {
		first, err := c.downloadConfig(ctx)
		if err == nil {
			var second []byte
			if second, err = c.downloadConfig(ctx); err == nil {
				if bytes.Equal(first, second) {
					return first, nil
				}
				err = fmt.Errorf("%w: two downloads differ (%d and %d bytes)", ErrInvalidConfigFile, len(first), len(second))
			}
		}
		if !errors.Is(err, ErrInvalidConfigFile) {
			return nil, err
		}
		c.log.WarnContext(ctx, "configuration download not usable; repeating", "round", round+1, "rounds", c.cfg.Retries+1, "reason", err.Error())
		last = err // a canceled context is reported by the next download, which checks it before it sends
	}
	return nil, last
}

// downloadConfig is one request for the file, checked by assembleConfig.
func (c *Client) downloadConfig(ctx context.Context) ([]byte, error) {
	req := []wire.TLV{
		{Type: wire.TLVUsername, Value: append([]byte(c.cfg.Username), 0)},
		{Type: wire.TLVPassword, Value: append([]byte(c.cfg.Password.Reveal()), 0)},
		{Type: wire.TLVConfigFile, Value: nil},
	}
	parts, err := c.exchangeFragments(ctx, wire.OpSet, req, wire.TLVConfigFile)
	if err != nil {
		return nil, err
	}
	return assembleConfig(parts)
}

// assembleConfig joins the packets of one download, in order, and refuses what cannot be the whole
// file: no magic (the first packet is missing, or this is not a configuration), or packets of
// unequal length before the last (the switch fills every packet but the last, 1400 bytes in a
// 2258-byte file; a shorter one in front means a neighbor was lost), or a last packet longer than
// the others. A lost middle packet of equal length cannot be seen here; the second download of
// ConfigBackup catches that.
func assembleConfig(parts [][]byte) ([]byte, error) {
	var file []byte
	for _, p := range parts {
		file = append(file, p...)
	}
	if len(file) <= len(wire.ConfigMagic) || !bytes.HasPrefix(file, wire.ConfigMagic) {
		return nil, fmt.Errorf("%w (%d bytes in %d packets, no configuration magic at the start)", ErrInvalidConfigFile, len(file), len(parts))
	}
	for i, p := range parts[:len(parts)-1] {
		if len(p) != len(parts[0]) {
			return nil, fmt.Errorf("%w (packet %d has %d bytes, packet 1 has %d)", ErrInvalidConfigFile, i+1, len(p), len(parts[0]))
		}
	}
	if last := parts[len(parts)-1]; len(last) > len(parts[0]) {
		return nil, fmt.Errorf("%w (the last packet has %d bytes, more than the first, %d)", ErrInvalidConfigFile, len(last), len(parts[0]))
	}
	return file, nil
}
