package entity

// AABB is an axis-aligned bounding box in world coordinates.
type AABB struct{ MinX, MinY, MinZ, MaxX, MaxY, MaxZ float64 }

// BoxAt returns the box of an entity whose feet are centred at (x, y, z).
func BoxAt(x, y, z, width, height float64) AABB {
	h := width / 2
	return AABB{x - h, y, z - h, x + h, y + height, z + h}
}

func (a AABB) Offset(dx, dy, dz float64) AABB {
	return AABB{a.MinX + dx, a.MinY + dy, a.MinZ + dz, a.MaxX + dx, a.MaxY + dy, a.MaxZ + dz}
}

func (a AABB) Grow(dx, dy, dz float64) AABB {
	return AABB{a.MinX - dx, a.MinY - dy, a.MinZ - dz, a.MaxX + dx, a.MaxY + dy, a.MaxZ + dz}
}

// Stretch extends the side of the box facing the movement direction.
func (a AABB) Stretch(dx, dy, dz float64) AABB {
	if dx < 0 {
		a.MinX += dx
	} else {
		a.MaxX += dx
	}
	if dy < 0 {
		a.MinY += dy
	} else {
		a.MaxY += dy
	}
	if dz < 0 {
		a.MinZ += dz
	} else {
		a.MaxZ += dz
	}
	return a
}

// Intersects reports strict overlap; boxes sharing only a face do not intersect.
func (a AABB) Intersects(o AABB) bool {
	return a.MinX < o.MaxX && a.MaxX > o.MinX &&
		a.MinY < o.MaxY && a.MaxY > o.MinY &&
		a.MinZ < o.MaxZ && a.MaxZ > o.MinZ
}

// rot swaps axis ax into the X slot so clipping can be written once for X.
func (a AABB) rot(ax int) AABB {
	switch ax {
	case axisY:
		return AABB{a.MinY, a.MinX, a.MinZ, a.MaxY, a.MaxX, a.MaxZ}
	case axisZ:
		return AABB{a.MinZ, a.MinY, a.MinX, a.MaxZ, a.MaxY, a.MaxX}
	}
	return a
}
