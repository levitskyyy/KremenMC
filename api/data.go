package api

// DataContainer is a persistent key-value store attached to an entity, block,
// world, item or offline player. Keys are namespaced by the writing plugin
// (e.g. "myplugin:kills"). Values are opaque bytes; helpers in the SDKs encode
// primitives.
type DataContainer interface {
	Get(key Key) ([]byte, bool)
	Set(key Key, value []byte)
	Remove(key Key)
	Has(key Key) bool
	Keys() []Key
}

// KV is a per-plugin persistent store, independent of any game object.
type KV interface {
	Get(key string) ([]byte, bool, error)
	Set(key string, value []byte) error
	Delete(key string) error
	Keys(prefix string) ([]string, error)
}
