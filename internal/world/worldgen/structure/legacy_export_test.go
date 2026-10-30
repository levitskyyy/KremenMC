package structure

// EnableLegacy wires recorded legacy structures into generation (tests).
func EnableLegacy(keys ...string) {
	for _, k := range keys {
		legacyEnabled[k] = true
	}
}
