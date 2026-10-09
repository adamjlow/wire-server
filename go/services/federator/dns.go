// SPDX-License-Identifier: AGPL-3.0-only
package federator

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"golang.org/x/net/dns/dnsmessage"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

type Target struct {
	Host string
	Port uint16
}

func dnsTargets(ctx context.Context, address, domain string) ([]Target, error) {
	if address == "" {
		data, err := os.ReadFile("/etc/resolv.conf")
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(string(data), "\n") {
			f := strings.Fields(line)
			if len(f) > 1 && f[0] == "nameserver" {
				address = net.JoinHostPort(f[1], "53")
				break
			}
		}
	}
	if address == "" {
		return nil, errors.New("no DNS resolver")
	}
	name, err := dnsmessage.NewName("_wire-server-federator._tcp." + domain + ".")
	if err != nil {
		return nil, err
	}
	var id [2]byte
	if _, err = rand.Read(id[:]); err != nil {
		return nil, err
	}
	query := dnsmessage.Message{Header: dnsmessage.Header{ID: binary.BigEndian.Uint16(id[:]), RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: name, Type: dnsmessage.TypeSRV, Class: dnsmessage.ClassINET}}}
	data, err := query.Pack()
	if err != nil {
		return nil, err
	}
	response, err := dnsExchange(ctx, address, data, false)
	if err != nil {
		return nil, err
	}
	var msg dnsmessage.Message
	if err = msg.Unpack(response); err != nil {
		return nil, err
	}
	if !msg.Response || msg.ID != query.ID {
		return nil, errors.New("invalid DNS response")
	}
	if msg.Truncated {
		response, err = dnsExchange(ctx, address, data, true)
		if err != nil {
			return nil, err
		}
		if err = msg.Unpack(response); err != nil {
			return nil, err
		}
		if !msg.Response || msg.ID != query.ID || msg.Truncated {
			return nil, errors.New("invalid DNS TCP response")
		}
	}
	if msg.RCode == dnsmessage.RCodeNameError {
		return nil, nil
	}
	if msg.RCode != dnsmessage.RCodeSuccess {
		return nil, errors.New("DNS server failure")
	}
	var targets []Target
	for _, answer := range msg.Answers {
		if srv, ok := answer.Body.(*dnsmessage.SRVResource); ok && answer.Header.Class == dnsmessage.ClassINET {
			targets = append(targets, Target{srv.Target.String(), srv.Port})
		}
	}
	if len(targets) == 1 && targets[0].Host == "." {
		return nil, nil
	}
	return targets, nil // Reference uses wire order, without RFC2782 sorting.
}
func dnsExchange(ctx context.Context, address string, query []byte, tcp bool) ([]byte, error) {
	network := "udp"
	if tcp {
		network = "tcp"
	}
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	deadline := time.Now().Add(5 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err = conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if tcp {
		length := make([]byte, 2)
		binary.BigEndian.PutUint16(length, uint16(len(query)))
		query = append(length, query...)
	}
	if _, err = conn.Write(query); err != nil {
		return nil, err
	}
	if tcp {
		var size [2]byte
		if _, err = io.ReadFull(conn, size[:]); err != nil {
			return nil, err
		}
		data := make([]byte, int(binary.BigEndian.Uint16(size[:])))
		_, err = io.ReadFull(conn, data)
		return data, err
	}
	data := make([]byte, 65535)
	n, err := conn.Read(data)
	return data[:n], err
}
