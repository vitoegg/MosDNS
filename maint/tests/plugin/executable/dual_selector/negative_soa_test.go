// SPDX-License-Identifier: GPL-3.0-only

package dual_selector

import (
	"context"
	"github.com/IrineSistiana/mosdns/v5/coremain"
	"github.com/IrineSistiana/mosdns/v5/pkg/query_context"
	"github.com/IrineSistiana/mosdns/v5/plugin/executable/sequence"
	"github.com/miekg/dns"
	"go.uber.org/zap"
	"testing"
)

func TestSuppressedResponseSOA(t *testing.T) {
	for _, prefer := range []uint16{dns.TypeA, dns.TypeAAAA} {
		s := newSelector(sequence.NewBQ(coremain.NewTestMosdnsWithPlugins(nil), zap.NewNop()), prefer)
		next := &dummyNext{returnA: true, returnAAAA: true}
		other := uint16(dns.TypeA)
		if prefer == dns.TypeA {
			other = dns.TypeAAAA
		}
		for _, qt := range []uint16{other, prefer, other} {
			q := new(dns.Msg)
			q.SetQuestion("dual.example.", qt)
			qc := query_context.NewContext(q)
			cw := sequence.NewChainWalker([]*sequence.ChainNode{{E: next}}, nil)
			if err := s.Exec(context.Background(), qc, cw); err != nil {
				t.Fatal(err)
			}
			r := qc.R()
			if qt == prefer {
				if !msgAnsHasRR(r, prefer) {
					t.Fatal("preferred answer lost")
				}
				continue
			}
			if r.Rcode != dns.RcodeSuccess || len(r.Answer) != 0 || len(r.Ns) != 1 {
				t.Fatalf("unexpected suppression: %v", r)
			}
			soa, ok := r.Ns[0].(*dns.SOA)
			if !ok || soa.Hdr.Name != q.Question[0].Name || soa.Hdr.Ttl != 300 || soa.Minttl != 86400 {
				t.Errorf("unexpected suppression SOA: %v", r.Ns)
			}
		}
		s.Close()
	}
}
