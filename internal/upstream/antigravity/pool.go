package antigravity

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
	LastUsedAt time.Time
	IsBusy     bool
	MinuteHits int
	DayHits    int
}

type AccountPool struct {
	Accounts []*Account
	mu       sync.Mutex
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

// GetReadyAccount returns the first account that is not busy and hasn't exceeded its rate limit.
func (p *AccountPool) GetReadyAccount() *Account {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, acc := range p.Accounts {
		if acc.IsBusy {
			continue
		}
		if acc.LimitType == "rpm" && acc.MinuteHits >= acc.LimitValue {
			continue
		}
		if acc.LimitType == "rpd" && acc.DayHits >= acc.LimitValue {
			continue
		}
		acc.IsBusy = true
		acc.MinuteHits++
		acc.DayHits++
		return acc
	}
	return nil
}

func (p *AccountPool) ReleaseAccount(acc *Account) {
	p.mu.Lock()
	defer p.mu.Unlock()
	acc.LastUsedAt = time.Now()
	acc.IsBusy = false
}
