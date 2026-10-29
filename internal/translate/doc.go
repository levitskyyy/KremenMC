// Package translate lets clients of older protocols talk to the canonical
// (packets.Protocol) server.
//
// Each supported protocol is a spec in version.go, ordered newest first. A
// clientbound packet is first rewritten once at the canonical end with
// direct canonical -> client tables (block states, items and their
// components, entity types and metadata, biomes, particles, sounds,
// attributes, command parsers, synced registry indices; see rewrite.go),
// then walks a chain of adjacent-version steps (step_<protocol>.go). A step
// only lists packets whose layout changed between its two versions;
// everything else is renumbered by name, and runs of renumbering are
// collapsed into one jump table per target, so payloads are decoded only
// where a layout actually changes. Serverbound packets walk the chain the
// other way and are rewritten back to canonical IDs at the end.
//
// Layout equality comes from per-protocol fingerprints in meta/*.json.gz;
// TestNoLayoutGaps fails when a step misses a changed packet. To add an
// older protocol N:
//
//	go run ./tools/protogen -ver <mc version> -patch none -out internal/translate/vN -pkg vN -dump /tmp/N.json
//	go run ./tools/datagen -ver <mc version> -src <minecraft-data dir>    # if not embedded yet
//	go run ./internal/translate/cmd/transgen -dump /tmp/N.json -mcdata <minecraft-data dir> \
//	    -protocol N -version <name> -packs <known pack versions> -data <data version>
//
// then append a spec with its step and make TestNoLayoutGaps pass. The
// canonical metadata (777) is built from the protogen dump of the patched
// schema with -opaque-from pointing at a 26.3 protocol.json.
package translate
