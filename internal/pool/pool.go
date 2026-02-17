package pool

import "sync"

type Account struct {
	ID     uint
	Email  string
	APIKey string
}

type AccountPool struct {
	Accounts []*Account
	mu       sync.Mutex
	index    int // round-robin index for spreading load
}

func NewAccountPool() *AccountPool {
	return &AccountPool{
		Accounts: []*Account{},
	}
}

// GetReadyAccount picks the next account via round-robin.
func (p *AccountPool) GetReadyAccount() *Account {
	p.mu.Lock()
	defer p.mu.Unlock()

	n := len(p.Accounts)
	if n == 0 {
		return nil
	}

	acc := p.Accounts[p.index%n]
	p.index = (p.index + 1) % n
	return acc
}

// Size returns the number of accounts in the pool.
func (p *AccountPool) Size() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.Accounts)
}
