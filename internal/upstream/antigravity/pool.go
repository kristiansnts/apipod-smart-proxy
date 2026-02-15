package antigravity

import (
	"sync"
	"time"
)

type Account struct {
	ID           uint
	Email        string
	RefreshToken string
	LastUsedAt   time.Time
	IsBusy       bool
}

type AccountPool struct {
	Accounts []*Account
	mu       sync.Mutex
	Cooldown time.Duration
}

func NewAccountPool(cooldown time.Duration) *AccountPool {
	return &AccountPool{
		Accounts: []*Account{},
		Cooldown: cooldown,
	}
}

// GetReadyAccount mencari akun yang tidak sibuk dan sudah melewati masa cooldown
func (p *AccountPool) GetReadyAccount() *Account {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	for _, acc := range p.Accounts {
		if !acc.IsBusy && now.Sub(acc.LastUsedAt) >= p.Cooldown {
			acc.IsBusy = true
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
