package integrationtest

import "github.com/piggystats/nodera/contexts/kubernetes/engine"

func errUnavailable() error { return engine.ErrUnavailable }
