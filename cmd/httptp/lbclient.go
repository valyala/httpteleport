package main

import (
	"github.com/valyala/fasthttp"
	"sync/atomic"
	"time"
)

type client interface {
	DoTimeout(req *fasthttp.Request, resp *fasthttp.Response, timeout time.Duration) error
	PendingRequests() int
}

type lbClient struct {
	client

	penalty uint32
}

func (c *lbClient) DoTimeout(req *fasthttp.Request, resp *fasthttp.Response, timeout time.Duration) error {
	err := c.client.DoTimeout(req, resp, timeout)
	if err != nil && c.incPenalty() {
		// Penalize the client returning error, so the next requests
		// are routed to another clients.
		d := timeout
		if d < minPenaltyDuration {
			d = minPenaltyDuration
		}
		time.AfterFunc(d, c.decPenalty)
	}
	return err
}

func (c *lbClient) PendingRequests() int {
	n := c.client.PendingRequests()
	m := atomic.LoadUint32(&c.penalty)
	return n + int(m)
}

func (c *lbClient) incPenalty() bool {
	m := atomic.AddUint32(&c.penalty, 1)
	if m > maxPenalty {
		c.decPenalty()
		return false
	}
	return true
}

func (c *lbClient) decPenalty() {
	atomic.AddUint32(&c.penalty, ^uint32(0))
}

const (
	maxPenalty = 300

	minPenaltyDuration = 3 * time.Second
)

type lbClients struct {
	cs []*lbClient

	// nextIdx is for spreading requests among equally loaded clients
	// in a round-robin fashion.
	nextIdx uint32
}

func (cc *lbClients) Add(c client) {
	cc.cs = append(cc.cs, &lbClient{client: c})
}

func (cc *lbClients) AddMulti(ss lbClients) {
	cc.cs = append(cc.cs, ss.cs...)
}

func (cc *lbClients) Get() client {
	cs := cc.cs
	idx := atomic.AddUint32(&cc.nextIdx, 1)
	idx %= uint32(len(cs))

	minC := cs[idx]
	minN := minC.PendingRequests()
	if minN == 0 {
		return minC
	}
	for _, c := range cs[idx+1:] {
		n := c.PendingRequests()
		if n == 0 {
			return c
		}
		if n < minN {
			minC = c
			minN = n
		}
	}
	for _, c := range cs[:idx] {
		n := c.PendingRequests()
		if n == 0 {
			return c
		}
		if n < minN {
			minC = c
			minN = n
		}
	}
	return minC
}
