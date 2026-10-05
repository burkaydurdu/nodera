// Package fake is the MTA the integration suite delivers to: it records what
// it is handed and can be told to refuse.
package fake

import (
	"context"
	"sync"

	"github.com/piggystats/nodera/contexts/mail/mta"
)

type Fake struct {
	mu         sync.Mutex
	injections []*mta.Injection
	failure    error
	refused    map[string]string
}

func New() *Fake {
	return &Fake{refused: map[string]string{}}
}

func (f *Fake) Inject(_ context.Context, in *mta.Injection) (*mta.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failure != nil {
		return nil, f.failure
	}

	f.injections = append(f.injections, in)
	result := &mta.Result{Failed: map[string]string{}}
	for _, r := range in.Envelope.Recipients {
		if reason, ok := f.refused[r.Email]; ok {
			result.Failed[r.Email] = reason
		}
	}

	return result, nil
}

// FailWith makes every injection fail until it is called with nil, as an
// unreachable MTA would.
func (f *Fake) FailWith(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failure = err
}

// Refuse makes the MTA refuse to queue the address.
func (f *Fake) Refuse(email, reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refused[email] = reason
}

// Admit takes the address again, as the MTA does once what made it refuse
// is over.
func (f *Fake) Admit(email string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.refused, email)
}

func (f *Fake) Injections() []*mta.Injection {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]*mta.Injection(nil), f.injections...)
}

func (f *Fake) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.injections = nil
	f.failure = nil
	f.refused = map[string]string{}
}
