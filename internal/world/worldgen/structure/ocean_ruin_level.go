package structure

// ScheduleBlockTick records a worldgen scheduled tick (falling blocks,
// water, kelp) in the proto-chunk holding x,y,z.
func (l *stageLevel) ScheduleBlockTick(x, y, z int32, typ string, delay int32, fluid bool) {
	l.sc.ScheduleTick(int(x), int(y), int(z), typ, delay, 0, fluid)
}
