package noise

// Interval is the reference closed float interval used for value ranges.
type Interval struct{ Min, Max float32 }

// Symmetric is [-v, v].
func Symmetric(v float32) Interval { return Interval{-v, v} }
