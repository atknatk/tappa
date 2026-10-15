package egress

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
)

// dns_test.go -- an in-process DNS server for the egress tests, so that a name resolves to whatever a
// test says WITHOUT a packet leaving the machine (the brief: no test reaches the real network). The
// Go resolver is pointed at it through net.Resolver{PreferGo, Dial}: every query it would send to a
// name server is written into one end of a net.Pipe and answered from the other. A pipe is not a
// PacketConn, so the resolver speaks DNS-over-TCP framing (a two-byte length before each message).
//
// Only what the resolver asks is implemented: one question, A or AAAA, answered from a function of
// (name, type, how many times that name and type were asked before) -- which is how DNS rebinding is
// staged: the first A answer public, the second private.

type fakeDNS struct {
	mu     sync.Mutex
	asked  map[string]int
	answer func(name string, qtype uint16, n int) (addrs []netip.Addr, nxdomain bool)
}

const (
	typeA    uint16 = 1
	typeAAAA uint16 = 28
)

func newFakeDNS(answer func(name string, qtype uint16, n int) ([]netip.Addr, bool)) *fakeDNS {
	return &fakeDNS{asked: map[string]int{}, answer: answer}
}

// resolver is the net.Resolver that asks this server and nothing else.
func (d *fakeDNS) resolver() *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			client, server := net.Pipe()
			go d.serve(server)
			return client, nil
		},
	}
}

// count reports how many times name/qtype was asked.
func (d *fakeDNS) count(name string, qtype uint16) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.asked[strings.ToLower(strings.TrimSuffix(name, "."))+"/"+itoa16(qtype)]
}

func itoa16(v uint16) string {
	if v == typeA {
		return "A"
	}
	if v == typeAAAA {
		return "AAAA"
	}
	return "other"
}

func (d *fakeDNS) serve(c net.Conn) {
	defer c.Close()
	for {
		var lenBuf [2]byte
		if _, err := io.ReadFull(c, lenBuf[:]); err != nil {
			return
		}
		msg := make([]byte, binary.BigEndian.Uint16(lenBuf[:]))
		if _, err := io.ReadFull(c, msg); err != nil {
			return
		}
		resp := d.respond(msg)
		if resp == nil {
			return
		}
		out := make([]byte, 2+len(resp))
		binary.BigEndian.PutUint16(out, uint16(len(resp)))
		copy(out[2:], resp)
		if _, err := c.Write(out); err != nil {
			return
		}
	}
}

// respond parses one query and builds its answer.
func (d *fakeDNS) respond(q []byte) []byte {
	if len(q) < 12 {
		return nil
	}
	// The question: labels from offset 12, then type and class.
	i := 12
	var labels []string
	for {
		if i >= len(q) {
			return nil
		}
		l := int(q[i])
		i++
		if l == 0 {
			break
		}
		if i+l > len(q) {
			return nil
		}
		labels = append(labels, string(q[i:i+l]))
		i += l
	}
	if i+4 > len(q) {
		return nil
	}
	qtype := binary.BigEndian.Uint16(q[i:])
	question := q[12 : i+4]
	name := strings.ToLower(strings.Join(labels, "."))

	d.mu.Lock()
	key := name + "/" + itoa16(qtype)
	n := d.asked[key]
	d.asked[key] = n + 1
	d.mu.Unlock()

	addrs, nx := d.answer(name, qtype, n)
	var answers [][]byte
	for _, a := range addrs {
		if (qtype == typeA) != a.Is4() || (qtype != typeA && qtype != typeAAAA) {
			continue
		}
		rdata := a.AsSlice()
		rr := []byte{0xC0, 0x0C} // a pointer to the question's name
		rr = binary.BigEndian.AppendUint16(rr, qtype)
		rr = binary.BigEndian.AppendUint16(rr, 1) // IN
		rr = binary.BigEndian.AppendUint32(rr, 0) // TTL 0: nothing may be cached
		rr = binary.BigEndian.AppendUint16(rr, uint16(len(rdata)))
		rr = append(rr, rdata...)
		answers = append(answers, rr)
	}
	h := make([]byte, 12)
	copy(h, q[:2])          // the id
	flags := uint16(0x8180) // QR, RD, RA, NOERROR
	if nx {
		flags |= 3 // NXDOMAIN
	}
	binary.BigEndian.PutUint16(h[2:], flags)
	binary.BigEndian.PutUint16(h[4:], 1)
	binary.BigEndian.PutUint16(h[6:], uint16(len(answers)))
	out := append(h, question...)
	for _, rr := range answers {
		out = append(out, rr...)
	}
	return out
}

// TestFakeDNS_AnswersTheGoResolver is the harness's own CONTROL: the resolver gets exactly what the
// answer function says, NXDOMAIN included.
func TestFakeDNS_AnswersTheGoResolver(t *testing.T) {
	d := newFakeDNS(func(name string, qtype uint16, _ int) ([]netip.Addr, bool) {
		// Every other name -- the absolute one, and each search-domain variant the resolver tries
		// after an NXDOMAIN -- is NXDOMAIN.
		if name != "hook.example.com" {
			return nil, true
		}
		return []netip.Addr{netip.MustParseAddr("192.0.2.10"), netip.MustParseAddr("2001:db8::10")}, false
	})
	got, err := d.resolver().LookupNetIP(context.Background(), "ip", "hook.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("CONTROL: %v", got)
	}
	if _, err := d.resolver().LookupNetIP(context.Background(), "ip", "gone.example.com"); err == nil {
		t.Fatal("CONTROL: NXDOMAIN resolved")
	}
}
