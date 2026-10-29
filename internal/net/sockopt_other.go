//go:build !unix

package net

func setReuseAddr(uintptr) error { return nil }
