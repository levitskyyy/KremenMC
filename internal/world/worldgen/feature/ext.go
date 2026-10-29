package feature

import "encoding/json"

// Type parsers registered from init() in per-type files. The built-in
// switches consult these before reporting a type as unsupported.
var (
	featureTypes   = map[string]func(rg *registry, b json.RawMessage) (Feature, error){}
	modifierTypes  = map[string]func(rg *registry, b json.RawMessage) (Modifier, error){}
	providerTypes  = map[string]func(rg *registry, b json.RawMessage) (StateProvider, error){}
	predicateTypes = map[string]func(rg *registry, b json.RawMessage) (Predicate, error){}
	heightTypes    = map[string]func(b json.RawMessage) (HeightProvider, error){}
)
