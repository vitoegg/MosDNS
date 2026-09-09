// SPDX-License-Identifier: GPL-3.0-only

package sequence

import (
	"context"
	"errors"
	"github.com/IrineSistiana/mosdns/v5/coremain"
	"github.com/IrineSistiana/mosdns/v5/pkg/query_context"
	"github.com/miekg/dns"
	"testing"
)

func TestRejectSOACompatibility(t *testing.T) {
	for _, tc := range []struct {
		exec string
		code int
	}{
		{"reject 3", dns.RcodeNameError}, {"reject 0", dns.RcodeSuccess},
		{"reject", dns.RcodeRefused}, {"reject 2", dns.RcodeServerFailure},
	} {
		for _, qt := range []uint16{dns.TypeA, dns.TypeAAAA, dns.TypePTR, dns.TypeHTTPS} {
			ps := map[string]any{"after": &dummy{wantErr: errors.New("reject did not terminate")}}
			m := coremain.NewTestMosdnsWithPlugins(ps)
			s, err := NewSequence(coremain.NewBP("test", m), []RuleArgs{{Exec: tc.exec}, {Exec: "$after"}})
			if err != nil {
				t.Fatal(err)
			}
			q := new(dns.Msg)
			q.SetQuestion("blocked.example.", qt)
			qc := query_context.NewContext(q)
			if err := s.Exec(context.Background(), qc); err != nil {
				t.Fatal(err)
			}
			s.Close()
			r := qc.R()
			if r == nil || r.Rcode != tc.code || len(r.Answer) != 0 {
				t.Fatalf("%s: invalid response %v", tc.exec, r)
			}
			if tc.code == dns.RcodeSuccess || tc.code == dns.RcodeNameError {
				if len(r.Ns) != 1 {
					t.Errorf("%s type=%d: missing SOA", tc.exec, qt)
					continue
				}
				soa, ok := r.Ns[0].(*dns.SOA)
				if !ok || soa.Hdr.Name != q.Question[0].Name || soa.Hdr.Ttl != 300 || soa.Minttl != 86400 {
					t.Errorf("invalid SOA: %v", r.Ns)
				}
			} else {
				want := new(dns.Msg)
				want.SetReply(q)
				want.Rcode = tc.code
				gotWire, e1 := r.Pack()
				wantWire, e2 := want.Pack()
				if e1 != nil || e2 != nil || string(gotWire) != string(wantWire) {
					t.Errorf("%s: legacy response changed", tc.exec)
				}
			}
		}
	}
}
