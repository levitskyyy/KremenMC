package command

import (
	"math"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"

	"github.com/levitskyyy/KremenMC/api"
)

type sortMode uint8

const (
	sortArbitrary sortMode = iota
	sortNearest
	sortFurthest
	sortRandom
)

type negKey struct {
	v   string
	neg bool
}

type floatRange struct {
	min, max       float64
	hasMin, hasMax bool
}

func (r floatRange) contains(v float64) bool {
	return (!r.hasMin || v >= r.min) && (!r.hasMax || v <= r.max)
}

type selector struct {
	kind        byte // 0 for a plain name or UUID
	name        string
	uuid        api.UUID
	hasUUID     bool
	limit       int
	sort        sortMode
	playersOnly bool
	aliveOnly   bool
	names       []negKey
	types       []negKey
	modes       []negKey
	tags        []negKey
	distance    *floatRange
	level       *floatRange
	pos         [3]*float64
	delta       [3]*float64
}

const (
	msgSelectorNotAllowed = "Selector not allowed"
	msgOnlyOneEntity      = "Only one entity is allowed, but the provided selector allows more than one"
	msgOnlyOnePlayer      = "Only one player is allowed, but the provided selector allows more than one"
	msgOnlyPlayers        = "Only players may be affected by this command, but the provided selector includes entities"
	msgNoEntity           = "No entity was found"
	msgNoPlayer           = "No player was found"
)

var selectorKinds = "parsen"

func (s *selector) single() bool { return s.kind == 0 || s.limit == 1 }

func (s *selector) worldLimited() bool {
	return s.distance != nil || s.pos != [3]*float64{} || s.delta != [3]*float64{}
}

func (s *selector) includesNonPlayers() bool {
	return !s.playersOnly && s.kind != 0 || s.kind == 0 && s.hasUUID
}

func parseSelector(r *reader, allowSelectors bool) (*selector, error) {
	start := r.pos
	if r.canRead() && r.peek() == '@' {
		if !allowSelectors {
			return nil, r.errf(msgSelectorNotAllowed)
		}
		r.skip()
		if !r.canRead() {
			return nil, r.errf("Missing selector type")
		}
		k := r.peek()
		if strings.IndexByte(selectorKinds, k) < 0 {
			r.pos = start
			return nil, r.errAt(start, "Unknown selector type '@%c'", k)
		}
		r.skip()
		s := &selector{kind: k}
		switch k {
		case 'p':
			s.limit, s.sort, s.playersOnly, s.aliveOnly = 1, sortNearest, true, true
		case 'a':
			s.playersOnly = true
		case 'r':
			s.limit, s.sort, s.playersOnly, s.aliveOnly = 1, sortRandom, true, true
		case 's':
			s.limit = 1
		case 'e':
			s.aliveOnly = true
		case 'n':
			s.limit, s.sort, s.aliveOnly = 1, sortNearest, true
		}
		if r.canRead() && r.peek() == '[' {
			r.skip()
			if err := s.parseOptions(r); err != nil {
				return nil, err
			}
		}
		return s, nil
	}
	word := r.readWhile(func(c byte) bool { return c != ' ' })
	if u, err := api.ParseUUID(word); err == nil && strings.Count(word, "-") == 4 {
		return &selector{uuid: u, hasUUID: true, limit: 1}, nil
	}
	if word == "" || len(word) > 16 {
		r.pos = start
		return nil, r.errf("Invalid name or UUID")
	}
	return &selector{name: word, limit: 1, playersOnly: true}, nil
}

func (s *selector) parseOptions(r *reader) error {
	r.skipWhitespace()
	for r.canRead() && r.peek() != ']' {
		r.skipWhitespace()
		optStart := r.pos
		key, err := r.readString()
		if err != nil {
			return err
		}
		r.skipWhitespace()
		if !r.canRead() || r.peek() != '=' {
			r.pos = optStart
			return r.errf("Expected value for option '%s'", key)
		}
		r.skip()
		r.skipWhitespace()
		if err := s.parseOption(r, key, optStart); err != nil {
			return err
		}
		r.skipWhitespace()
		if !r.canRead() {
			break
		}
		if r.peek() == ',' {
			r.skip()
			continue
		}
		if r.peek() != ']' {
			return r.errf("Expected end of options")
		}
	}
	if !r.canRead() {
		return r.errf("Expected end of options")
	}
	r.skip()
	return nil
}

func readNeg(r *reader) bool {
	if r.canRead() && r.peek() == '!' {
		r.skip()
		r.skipWhitespace()
		return true
	}
	return false
}

func (s *selector) parseOption(r *reader, key string, optStart int) error {
	valStart := r.pos
	switch key {
	case "name":
		neg := readNeg(r)
		v, err := r.readString()
		if err != nil {
			return err
		}
		if !neg && slices.ContainsFunc(s.names, func(n negKey) bool { return !n.neg }) {
			r.pos = optStart
			return r.errf("Option 'name' isn't applicable here")
		}
		s.names = append(s.names, negKey{v, neg})
	case "distance":
		fr, err := parseRange(r, false)
		if err != nil {
			return err
		}
		if fr.hasMin && fr.min < 0 || fr.hasMax && fr.max < 0 {
			r.pos = valStart
			return r.errf("Distance cannot be negative")
		}
		s.distance = &fr
	case "level":
		fr, err := parseRange(r, true)
		if err != nil {
			return err
		}
		if fr.hasMin && fr.min < 0 || fr.hasMax && fr.max < 0 {
			r.pos = valStart
			return r.errf("Level cannot be negative")
		}
		s.level = &fr
		s.playersOnly = true
	case "limit":
		if s.kind == 's' {
			r.pos = optStart
			return r.errf("Option 'limit' isn't applicable here")
		}
		v, err := r.readInt()
		if err != nil {
			return err
		}
		if v < 1 {
			r.pos = valStart
			return r.errf("Limit must be at least 1")
		}
		s.limit = int(v)
	case "sort":
		if s.kind == 's' {
			r.pos = optStart
			return r.errf("Option 'sort' isn't applicable here")
		}
		v := r.readUnquoted()
		switch v {
		case "nearest":
			s.sort = sortNearest
		case "furthest":
			s.sort = sortFurthest
		case "random":
			s.sort = sortRandom
		case "arbitrary":
			s.sort = sortArbitrary
		default:
			r.pos = valStart
			return r.errf("Invalid or unknown sort type '%s'", v)
		}
	case "type":
		neg := readNeg(r)
		tag := false
		if r.canRead() && r.peek() == '#' {
			r.skip()
			tag = true
		}
		k, err := r.readKey()
		if err != nil {
			return err
		}
		if tag {
			k = "#" + k
		}
		if !neg && slices.ContainsFunc(s.types, func(n negKey) bool { return !n.neg }) {
			r.pos = optStart
			return r.errf("Option 'type' isn't applicable here")
		}
		if k == "minecraft:player" && !neg {
			s.playersOnly = true
		}
		s.types = append(s.types, negKey{k, neg})
	case "gamemode":
		neg := readNeg(r)
		v := r.readUnquoted()
		if _, ok := parseGameMode(v); !ok {
			r.pos = valStart
			return r.errf("Invalid or unknown game mode '%s'", v)
		}
		s.modes = append(s.modes, negKey{v, neg})
		s.playersOnly = true
	case "tag":
		neg := readNeg(r)
		s.tags = append(s.tags, negKey{r.readUnquoted(), neg})
	case "x", "y", "z", "dx", "dy", "dz":
		v, err := r.readDouble()
		if err != nil {
			return err
		}
		i := strings.IndexByte("xyz", key[len(key)-1])
		if key[0] == 'd' {
			s.delta[i] = &v
		} else {
			s.pos[i] = &v
		}
	default:
		r.pos = optStart
		return r.errf("Unknown option '%s'", key)
	}
	return nil
}

func parseRange(r *reader, integer bool) (floatRange, error) {
	start := r.pos
	read := func() (float64, bool, error) {
		begin := r.pos
		for r.canRead() && (r.peek() >= '0' && r.peek() <= '9' || r.peek() == '-' || r.peek() == '+' ||
			r.peek() == '.' && !(r.canReadN(2) && r.s[r.pos+1] == '.')) {
			r.skip()
		}
		s := r.s[begin:r.pos]
		if s == "" {
			return 0, false, nil
		}
		var v float64
		var err error
		if integer {
			var i int64
			i, err = strconv.ParseInt(s, 10, 32)
			v = float64(i)
		} else {
			v, err = strconv.ParseFloat(s, 64)
		}
		if err != nil {
			r.pos = begin
			if integer {
				return 0, false, r.errf("Invalid integer '%s'", s)
			}
			return 0, false, r.errf("Invalid double '%s'", s)
		}
		return v, true, nil
	}
	var fr floatRange
	var err error
	if fr.min, fr.hasMin, err = read(); err != nil {
		return fr, err
	}
	if r.canReadN(2) && r.s[r.pos] == '.' && r.s[r.pos+1] == '.' {
		r.pos += 2
		if fr.max, fr.hasMax, err = read(); err != nil {
			return fr, err
		}
	} else {
		fr.max, fr.hasMax = fr.min, fr.hasMin
	}
	if !fr.hasMin && !fr.hasMax {
		r.pos = start
		return fr, r.errf("Expected value or range of values")
	}
	if fr.hasMin && fr.hasMax && fr.min > fr.max {
		r.pos = start
		return fr, r.errf("Min cannot be bigger than max")
	}
	return fr, nil
}

func parseGameMode(s string) (api.GameMode, bool) {
	for _, g := range []api.GameMode{api.Survival, api.Creative, api.Adventure, api.Spectator} {
		if g.String() == s {
			return g, true
		}
	}
	return 0, false
}

// resolveEntities evaluates the selector against live server state.
func (c *cmdContext) resolveEntities(s *selector) ([]api.Entity, error) {
	srv := c.d.server
	if srv == nil {
		return nil, nil
	}
	switch {
	case s.kind == 0 && s.hasUUID:
		if p, ok := srv.Player(s.uuid); ok {
			return []api.Entity{p}, nil
		}
		for _, w := range srv.Worlds() {
			if e, ok := w.Entity(s.uuid); ok {
				return []api.Entity{e}, nil
			}
		}
		return nil, nil
	case s.kind == 0:
		if p, ok := srv.PlayerByName(s.name); ok {
			return []api.Entity{p}, nil
		}
		return nil, nil
	}
	origin := c.origin()
	for i, p := range s.pos {
		if p != nil {
			switch i {
			case 0:
				origin.Pos.X = *p
			case 1:
				origin.Pos.Y = *p
			case 2:
				origin.Pos.Z = *p
			}
		}
	}
	var candidates []api.Entity
	if s.kind == 's' {
		if e, ok := c.sender.(api.Entity); ok {
			candidates = []api.Entity{e}
		}
	} else {
		candidates = c.candidates(s)
	}
	out := candidates[:0:0]
	for _, e := range candidates {
		if s.matches(e, origin) {
			out = append(out, e)
		}
	}
	switch s.sort {
	case sortNearest, sortFurthest:
		slices.SortStableFunc(out, func(a, b api.Entity) int {
			da, db := a.Location().Pos.DistanceSq(origin.Pos), b.Location().Pos.DistanceSq(origin.Pos)
			if s.sort == sortFurthest {
				da, db = db, da
			}
			switch {
			case da < db:
				return -1
			case da > db:
				return 1
			}
			return 0
		})
	case sortRandom:
		rand.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	}
	if s.limit > 0 && len(out) > s.limit {
		out = out[:s.limit]
	}
	return out, nil
}

func (c *cmdContext) candidates(s *selector) []api.Entity {
	srv := c.d.server
	var worlds []api.World
	if s.worldLimited() {
		if w := c.world(); w != nil {
			worlds = []api.World{w}
		}
	}
	var out []api.Entity
	if s.playersOnly {
		var ps []api.Player
		if worlds != nil {
			ps = worlds[0].Players()
		} else {
			ps = srv.Players()
		}
		for _, p := range ps {
			out = append(out, p)
		}
		return out
	}
	if worlds == nil {
		worlds = srv.Worlds()
	}
	seen := make(map[api.UUID]bool)
	for _, w := range worlds {
		for _, p := range w.Players() {
			if !seen[p.UUID()] {
				seen[p.UUID()] = true
				out = append(out, p)
			}
		}
		for _, e := range w.Entities() {
			if !seen[e.UUID()] {
				seen[e.UUID()] = true
				out = append(out, e)
			}
		}
	}
	return out
}

func (s *selector) matches(e api.Entity, origin api.Location) bool {
	p, isPlayer := e.(api.Player)
	if s.playersOnly && !isPlayer {
		return false
	}
	if s.aliveOnly {
		if l, ok := e.(api.LivingEntity); ok && l.IsDead() {
			return false
		}
	}
	for _, n := range s.names {
		if (entityName(e) == n.v) == n.neg {
			return false
		}
	}
	for _, t := range s.types {
		if strings.HasPrefix(t.v, "#") {
			continue
		}
		if (e.Type().Normalize() == api.Key(t.v)) == t.neg {
			return false
		}
	}
	if len(s.modes) > 0 {
		if !isPlayer {
			return false
		}
		for _, m := range s.modes {
			if (p.GameMode().String() == m.v) == m.neg {
				return false
			}
		}
	}
	for _, t := range s.tags {
		var has bool
		if t.v == "" {
			has = len(e.Tags()) > 0
			if has != t.neg {
				return false
			}
			continue
		}
		has = slices.Contains(e.Tags(), t.v)
		if has == t.neg {
			return false
		}
	}
	if s.level != nil && (!isPlayer || !s.level.contains(float64(p.Level()))) {
		return false
	}
	loc := e.Location()
	if s.worldLimited() && origin.World != "" && loc.World != "" && loc.World != origin.World {
		return false
	}
	if s.distance != nil && !s.distance.contains(math.Sqrt(loc.Pos.DistanceSq(origin.Pos))) {
		return false
	}
	if s.delta != [3]*float64{} {
		var d [3]float64
		for i, v := range s.delta {
			if v != nil {
				d[i] = *v
			}
		}
		lo := api.Vec3{X: origin.Pos.X + math.Min(d[0], 0), Y: origin.Pos.Y + math.Min(d[1], 0), Z: origin.Pos.Z + math.Min(d[2], 0)}
		hi := api.Vec3{X: origin.Pos.X + math.Max(d[0], 0) + 1, Y: origin.Pos.Y + math.Max(d[1], 0) + 1, Z: origin.Pos.Z + math.Max(d[2], 0) + 1}
		bmin, bmax := e.BoundingBox()
		if bmin == bmax {
			bmin, bmax = loc.Pos, loc.Pos
		}
		if bmax.X < lo.X || bmin.X > hi.X || bmax.Y < lo.Y || bmin.Y > hi.Y || bmax.Z < lo.Z || bmin.Z > hi.Z {
			return false
		}
	}
	return true
}

func entityName(e api.Entity) string {
	if p, ok := e.(api.Player); ok {
		return p.Name()
	}
	if n, ok := e.CustomName(); ok {
		return n.Plain()
	}
	return displayType(e.Type())
}

func displayType(k api.Key) string {
	p := strings.ReplaceAll(k.Path(), "_", " ")
	if p == "" {
		return string(k)
	}
	return strings.ToUpper(p[:1]) + p[1:]
}

func joinNames(es []api.Entity) string {
	names := make([]string, len(es))
	for i, e := range es {
		names[i] = entityName(e)
	}
	return strings.Join(names, ", ")
}

// profileArg is a parsed game_profile argument: a selector or a raw name.
type profileArg struct {
	sel  *selector
	name string
}

func (c *cmdContext) resolveProfiles(a *profileArg) ([]api.GameProfile, error) {
	if a.sel != nil {
		es, _ := c.resolveEntities(a.sel)
		out := make([]api.GameProfile, 0, len(es))
		for _, e := range es {
			if p, ok := e.(api.Player); ok {
				out = append(out, p.Profile())
			}
		}
		if len(out) == 0 {
			return nil, fail(msgNoPlayer)
		}
		return out, nil
	}
	if p, ok := c.d.lookupProfile(a.name); ok {
		return []api.GameProfile{p}, nil
	}
	return nil, fail("That player does not exist")
}

func (d *Dispatcher) lookupProfile(name string) (api.GameProfile, bool) {
	srv := d.server
	if srv == nil {
		return api.GameProfile{}, false
	}
	if p, ok := srv.PlayerByName(name); ok {
		return p.Profile(), true
	}
	if fn := d.profileLookup.Load(); fn != nil {
		if p, ok := (*fn)(name); ok {
			return p, true
		}
	}
	if wl := srv.Whitelist(); wl != nil {
		for _, p := range wl.Entries() {
			if strings.EqualFold(p.Name, name) {
				return p, true
			}
		}
	}
	if bl := srv.ProfileBans(); bl != nil {
		for _, e := range bl.Entries() {
			if strings.EqualFold(e.Name, name) {
				if u, err := api.ParseUUID(e.Target); err == nil {
					return api.GameProfile{UUID: u, Name: e.Name}, true
				}
			}
		}
	}
	return api.GameProfile{}, false
}
