// SPDX-License-Identifier: GPL-3.0-only

package dnsutils

import (
	"github.com/miekg/dns"
	"testing"
)

func TestNegativeSOAQuestionCount(t *testing.T) {
	for _, n := range []int{0, 1, 2} {
		for _, code := range []int{dns.RcodeSuccess, dns.RcodeNameError} {
			q := new(dns.Msg)
			q.SetQuestion("blocked.example.", dns.TypePTR)
			first := q.Question[0]
			q.Question = nil
			for i := 0; i < n; i++ {
				q.Question = append(q.Question, first)
			}
			r := GenEmptyReply(q, code)
			if r.Rcode != code || !r.Response || r.Id != q.Id || len(r.Answer) != 0 || len(r.Ns) != 1 {
				t.Fatalf("n=%d code=%d: invalid response: %v", n, code, r)
			}
			soa, ok := r.Ns[0].(*dns.SOA)
			name := "."
			if n == 1 {
				name = first.Name
			}
			if !ok || soa.Hdr.Name != name || soa.Hdr.Ttl != 300 || soa.Minttl != 86400 || soa.Hdr.Class != dns.ClassINET {
				t.Errorf("n=%d code=%d: unexpected SOA: %v", n, code, r.Ns)
			}
		}
	}
}
