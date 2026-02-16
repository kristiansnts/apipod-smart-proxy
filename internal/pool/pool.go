package pool

import (
	"sync"
	"time"
)

type Account struct {
	ID         uint
	Email      string
	APIKey     string
	LimitType  string
	LimitValue int
	MinuteHits int
	DayHits    int
}

type AccountPool struct {
	Accounts []*Account
	mu       sync.Mutex
	index    int // round-robin index for spreading load
}

func NewAccountPool() *AccountPool {
	p := &AccountPool{
		Accounts: []*Account{},
	}
	go p.startMinuteResetter()
	go p.startDayResetter()
	return p
}

func (p *AccountPool) startMinuteResetter() {
	ticker := time.NewTicker(1 * time.Minute)
	for range ticker.C {
		p.mu.Lock()
		for _, acc := range p.Accounts {
			acc.MinuteHits = 0
		}
		p.mu.Unlock()
	}
}

func (p *AccountPool) startDayResetter() {
	ticker := time.NewTicker(24 * time.Hour)
	for range ticker.C {
		p.mu.Lock()
		for _, acc := range p.Accounts {
			acc.DayHits = 0
		}
		p.mu.Unlock()
	}
}

// GetReadyAccount picks the next account via round-robin that hasn't exceeded its rate limit.
// Multiple concurrent requests can use the same account — only rate limits block selection.
func (p *AccountPool) GetReadyAccount() *Account {
	p.mu.Lock()
	defer p.mu.Unlock()

	n := len(p.Accounts)
	if n == 0 {
		return nil
	}

	// Try all accounts starting from current index (round-robin)
	for i := 0; i < n; i++ {
		acc := p.Accounts[(p.index+i)%n]
		if acc.LimitType == "rpm" && acc.MinuteHits >= acc.LimitValue {
			continue
		}
		if acc.LimitType == "rpd" && acc.DayHits >= acc.LimitValue {
			continue
		}
		acc.MinuteHits++
		acc.DayHits++
		p.index = (p.index + i + 1) % n // advance past this account
		return acc
	}
	return nil
}

// Size returns the number of accounts in the pool.
func (p *AccountPool) Size() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.Accounts)
}
