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
}

type AccountPool struct {
	Accounts []*Account
	mu       sync.Mutex
}

func NewAccountPool() *AccountPool {
	return &AccountPool{
		Accounts: []*Account{},
	}
}

// GetReadyAccount mencari akun yang tidak sibuk dan sudah melewati masa cooldown sesuai tipe limitnya
func (p *AccountPool) GetReadyAccount() *Account {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	for _, acc := range p.Accounts {
		if acc.IsBusy {
			continue
		}

		var cooldown time.Duration
		if acc.LimitType == "rpd" {
			// Requests Per Day: sebar merata dalam 24 jam (tambah buffer 10%)
			secondsInDay := 24 * 3600.0
			interval := secondsInDay / float64(acc.LimitValue)
			cooldown = time.Duration(interval * 1.1 * float64(time.Second))
		} else {
			// Requests Per Minute: sebar merata dalam 60 detik (tambah buffer 10%)
			interval := 60.0 / float64(acc.LimitValue)
			cooldown = time.Duration(interval * 1.1 * float64(time.Second))
		}

		if now.Sub(acc.LastUsedAt) >= cooldown {
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
