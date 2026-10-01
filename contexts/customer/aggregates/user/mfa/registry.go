package mfa

type Registry struct {
	providers map[Method]Provider
}

func NewRegistry() *Registry {
	return &Registry{providers: map[Method]Provider{}}
}

func (r *Registry) Register(p Provider) {
	r.providers[p.Method()] = p
}

func (r *Registry) Lookup(m Method) (Provider, error) {
	p, ok := r.providers[m]
	if !ok {
		return nil, ErrorUnknownMethod
	}

	return p, nil
}
