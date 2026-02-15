package antigravity

import (
	"sync"
	"time"
)

type Account struct {
	ID           uint
	Email        string
	RefreshToken string
	LimitType    string
	LimitValue   int
	LastUsedAt   time.Time
	IsBusy       bool
	MinuteHits   int
	TotalErrors  int
}

type AccountPool struct {
	Accounts []*Account
	mu       sync.Mutex
}

func NewAccountPool() *AccountPool {
	p := &AccountPool{
		Accounts: []*Account{},
	}
	// Jalankan per-minute resetter secara background
	go p.startStatsResetter()
	return p
}

func (p *AccountPool) startStatsResetter() {
	ticker := time.NewTicker(1 * time.Minute)
	for range ticker.C {
		p.mu.Lock()
		for _, acc := range p.Accounts {
			// Di sini nanti bisa ditambahkan logic push ke DB sebelum reset
			acc.MinuteHits = 0
		}
		p.mu.Unlock()
	}
}

// GetReadyAccount mencari akun yang sedang tidak sibuk (sedang melayani request lain)
func (p *AccountPool) GetReadyAccount() *Account {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, acc := range p.Accounts {
		if !acc.IsBusy {
			acc.IsBusy = true
			acc.MinuteHits++
			return acc
		}
	}
	return nil
}


func (p *AccountPool) ReleaseAccount(acc *Account) {
	p.mu.Lock()
	defer p.mu.Unlock()
	acc.LastUsedAt = time.Now()
	acc.IsBusy = false
}
