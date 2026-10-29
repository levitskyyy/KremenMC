//go:build !linux && !darwin

package metrics

import "github.com/levitskyyy/KremenMC/api"

type platform struct{}

func (platform) sample(*api.ServerStats) {}
