//go:build race

package main

func init() { heavySkip = "too slow under -race" }
