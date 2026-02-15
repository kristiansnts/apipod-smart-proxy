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
	RPM      int // Total RPM limit for this pool
}

func NewAccountPool(rpm int) *AccountPool {
	return &AccountPool{
		Accounts: []*Account{},
		RPM:      rpm,
	}
}

// GetReadyAccount mencari akun yang tidak sibuk dan sudah melewati masa cooldown dinamis
func (p *AccountPool) GetReadyAccount() *Account {
	p.mu.Lock()
	defer p.mu.Unlock()

	numAccounts := len(p.Accounts)
	if numAccounts == 0 {
		return nil
	}

	// Hitung cooldown dinamis: 60 detik / (RPM total / jumlah akun)
	// Misal: RPM 100, Akun 10 -> 10 RPM per akun -> Cooldown 6 detik.
	// Kita beri buffer aman (1.1x) agar tidak pas-pasan dengan limit provider.
	rpmPerAccount := float64(p.RPM) / float64(numAccounts)
	cooldownSecs := 60.0 / rpmPerAccount
	dynamicCooldown := time.Duration(cooldownSecs*1.1*float64(time.Second))

	now := time.Now()
	for _, acc := range p.Accounts {
		if !acc.IsBusy && now.Sub(acc.LastUsedAt) >= dynamicCooldown {
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
