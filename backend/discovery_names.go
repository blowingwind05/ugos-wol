package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
)

// Names are optional hints. A timeout or an unsupported name service must not
// discard discovered MAC addresses or make the user wait for every host.
func nameDiscoveredDevices(ctx context.Context, network Network, devices []DiscoveredDevice, lookup func(context.Context, Network, string) string) {
	budget := 2 * time.Second
	if end, ok := ctx.Deadline(); ok && time.Until(end)-100*time.Millisecond < budget {
		budget = time.Until(end) - 100*time.Millisecond
	}
	if budget <= 0 || len(devices) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	jobs := make(chan int)
	var workers sync.WaitGroup
	for i := 0; i < min(16, len(devices)); i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				if ctx.Err() != nil {
					continue
				}
				devices[index].Name = cleanDeviceName(lookup(ctx, network, devices[index].IP))
			}
		}()
	}
	for index := range devices {
		select {
		case jobs <- index:
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return
		}
	}
	close(jobs)
	workers.Wait()
}

func cleanDeviceName(name string) string {
	name = strings.TrimSuffix(strings.TrimSpace(name), ".")
	if name == "" || !utf8.ValidString(name) || len([]rune(name)) > 253 || net.ParseIP(name) != nil {
		return ""
	}
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".in-addr.arpa") || strings.HasSuffix(lower, ".ip6.arpa") {
		return ""
	}
	alphanumeric := false
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			alphanumeric = true
			continue
		}
		if unicode.IsMark(r) || r == '-' || r == '_' || r == '.' || r == ' ' {
			continue
		}
		return ""
	}
	if !alphanumeric {
		return ""
	}
	return name
}

func lookupDeviceName(ctx context.Context, network Network, ip string) string {
	// A home router's DNS often knows DHCP hostnames. Prefer it when available.
	dnsCtx, cancel := context.WithTimeout(ctx, 450*time.Millisecond)
	resolver := &net.Resolver{PreferGo: true}
	names, _ := resolver.LookupAddr(dnsCtx, ip)
	cancel()
	for _, name := range names {
		if name = cleanDeviceName(name); name != "" {
			return name
		}
	}
	if ctx.Err() != nil {
		return ""
	}
	// Bonjour/Avahi and Windows name services cover hosts not registered in DNS.
	fallback, stop := context.WithTimeout(ctx, 650*time.Millisecond)
	defer stop()
	answers := make(chan string, 2)
	var wg sync.WaitGroup
	for _, kind := range []string{"mdns", "netbios"} {
		wg.Add(1)
		go func(kind string) {
			defer wg.Done()
			answers <- queryLocalName(fallback, network, ip, kind)
		}(kind)
	}
	name := ""
	for range 2 {
		if candidate := <-answers; name == "" && candidate != "" {
			name = candidate
			stop()
		}
	}
	wg.Wait()
	return name
}

func reverseIPv4(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil || !addr.Is4() {
		return ""
	}
	b := addr.As4()
	return fmt.Sprintf("%d.%d.%d.%d.in-addr.arpa", b[3], b[2], b[1], b[0])
}

func nameQuery(id uint16, ip, kind string) ([]byte, string) {
	query := make([]byte, 12)
	binary.BigEndian.PutUint16(query, id)
	binary.BigEndian.PutUint16(query[4:], 1)
	name, recordType := reverseIPv4(ip), uint16(12)
	if name == "" {
		return nil, ""
	}
	if kind == "netbios" {
		// RFC 1002 wildcard node status: '*' followed by fifteen zero bytes.
		label := make([]byte, 32)
		for i := range 16 {
			value := byte(0)
			if i == 0 {
				value = '*'
			}
			label[i*2], label[i*2+1] = 'A'+(value>>4), 'A'+(value&15)
		}
		name, recordType = string(label), 0x21
	}
	for _, label := range strings.Split(name, ".") {
		query = append(query, byte(len(label)))
		query = append(query, label...)
	}
	query = append(query, 0, byte(recordType>>8), byte(recordType), 0, 1)
	return query, name
}

func queryLocalName(ctx context.Context, network Network, ip, kind string) string {
	var random [2]byte
	if _, err := rand.Read(random[:]); err != nil {
		return ""
	}
	id := binary.BigEndian.Uint16(random[:])
	query, owner := nameQuery(id, ip, kind)
	if query == nil {
		return ""
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(network.IP)})
	if err != nil {
		return ""
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(650 * time.Millisecond)
	}
	if conn.SetDeadline(deadline) != nil {
		return ""
	}
	target := &net.UDPAddr{IP: net.ParseIP(ip), Port: 137}
	if kind == "mdns" {
		// Legacy mDNS queries use an ephemeral source port and unicast replies.
		target = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}
		var source [4]byte
		copy(source[:], net.ParseIP(network.IP).To4())
		raw, err := conn.SyscallConn()
		if err != nil {
			return ""
		}
		var socketErr error
		if err := raw.Control(func(fd uintptr) {
			socketErr = syscall.SetsockoptInet4Addr(int(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_IF, source)
			if socketErr == nil {
				socketErr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_TTL, 255)
			}
		}); err != nil || socketErr != nil {
			return ""
		}
	}
	if _, err := conn.WriteToUDP(query, target); err != nil {
		return ""
	}
	packet := make([]byte, 8192)
	for {
		n, peer, err := conn.ReadFromUDP(packet)
		if err != nil {
			return ""
		}
		if !peer.IP.Equal(net.ParseIP(ip)) || peer.Port != target.Port {
			continue
		}
		if name := parseNameAnswer(packet[:n], id, owner, kind); name != "" {
			return name
		}
	}
}

// DNS and NetBIOS name service share DNS-style names and resource record headers.
// Bound pointer traversal to reject compression cycles and malformed LAN input.
func dnsName(packet []byte, offset int) (string, int, error) {
	labels, next, total := []string{}, -1, 0
	for jumps := 0; jumps < 128; jumps++ {
		if offset < 0 || offset >= len(packet) {
			break
		}
		length := int(packet[offset])
		if length&0xc0 == 0xc0 {
			if offset+1 >= len(packet) {
				break
			}
			if next < 0 {
				next = offset + 2
			}
			offset = (length&0x3f)<<8 | int(packet[offset+1])
			continue
		}
		if length == 0 {
			if next < 0 {
				next = offset + 1
			}
			return strings.Join(labels, "."), next, nil
		}
		if length > 63 || offset+1+length > len(packet) || total+length+1 > 255 {
			break
		}
		labels = append(labels, string(packet[offset+1:offset+1+length]))
		total += length + 1
		offset += length + 1
	}
	return "", 0, errors.New("invalid DNS name")
}

func parseNameAnswer(packet []byte, id uint16, owner, kind string) string {
	if len(packet) < 12 || binary.BigEndian.Uint16(packet) != id {
		return ""
	}
	flags := binary.BigEndian.Uint16(packet[2:])
	if flags&0x8000 == 0 || flags&0x780f != 0 {
		return ""
	}
	questions, answers := int(binary.BigEndian.Uint16(packet[4:])), int(binary.BigEndian.Uint16(packet[6:]))
	if questions > 16 || answers > 256 {
		return ""
	}
	offset := 12
	for range questions {
		_, end, err := dnsName(packet, offset)
		if err != nil || end+4 > len(packet) {
			return ""
		}
		offset = end + 4
	}
	for range answers {
		name, end, err := dnsName(packet, offset)
		if err != nil || end+10 > len(packet) {
			return ""
		}
		typeID, class := binary.BigEndian.Uint16(packet[end:]), binary.BigEndian.Uint16(packet[end+2:])&0x7fff
		ttl := binary.BigEndian.Uint32(packet[end+4:])
		size := int(binary.BigEndian.Uint16(packet[end+8:]))
		start := end + 10
		if start+size > len(packet) {
			return ""
		}
		offset = start + size
		if !strings.EqualFold(name, owner) || class != 1 {
			continue
		}
		if kind == "mdns" && typeID == 12 && ttl > 0 {
			value, finish, err := dnsName(packet, start)
			if err == nil && finish <= offset {
				if value = cleanDeviceName(value); value != "" {
					return value
				}
			}
		}
		if kind == "netbios" && typeID == 0x21 && size > 0 {
			count := int(packet[start])
			if 1+count*18 > size {
				return ""
			}
			serverName := ""
			for i := 0; i < count; i++ {
				entry := packet[start+1+i*18 : start+1+(i+1)*18]
				if binary.BigEndian.Uint16(entry[16:])&0x8000 != 0 {
					continue
				}
				value := cleanDeviceName(strings.TrimSpace(string(entry[:15])))
				if value != "" && entry[15] == 0 {
					return value
				}
				if value != "" && entry[15] == 0x20 {
					serverName = value
				}
			}
			if serverName != "" {
				return serverName
			}
		}
	}
	return ""
}
