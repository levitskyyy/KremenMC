package server

import (
	"bytes"
	"crypto/rand"
	"sync"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/metrics"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

// AdminBarPermission gates /adminbar and who may keep the bar on.
const AdminBarPermission = "kremenmc.command.adminbar"

// Wire constants verified against the 26.3 server classes.
const (
	barObjective = "kremenmc_admin"

	objectiveAdd    = 0 // ClientboundSetObjectivePacket.METHOD_ADD
	objectiveRemove = 1
	renderInteger   = 0 // ObjectiveCriteria$RenderType.INTEGER
	formatBlank     = 0 // NumberFormatType registry: blank, styled, fixed
	slotSidebar     = 1 // DisplaySlot.SIDEBAR id

	bossAdd      = 0 // ClientboundBossEventPacket$OperationType ordinals
	bossRemove   = 1
	bossProgress = 2
	bossName     = 3
	bossStyle    = 4

	bossRed    = 2 // BossEvent$BossBarColor ordinals
	bossGreen  = 3
	bossYellow = 4

	overlayProgress = 0 // BossEvent$BossBarOverlay.PROGRESS
)

// barOwners are the hidden score holders, one per sidebar line; scores
// count down so the first line sorts to the top.
var barOwners = func() [metrics.MaxLines]string {
	var o [metrics.MaxLines]string
	for i := range o {
		o[i] = "#kremenmc" + string(rune('a'+i))
	}
	return o
}()

type barMode struct {
	Sidebar bool `json:"sidebar"`
	BossBar bool `json:"bossbar"`
}

type bossState struct {
	title    proto.Text
	nbt      []byte
	progress float32
	color    int32
}

// adminBar renders the server stats into a sidebar objective and a boss
// bar. Every viewer always holds the same state, so updates are diffs built
// once and the same bytes are queued to each viewer. mu also orders the
// packets sent to a viewer.
type adminBar struct {
	s      *Server
	path   string
	bossID [16]byte

	objAdd, objRemove, display, bossRm []byte

	mu      sync.Mutex
	prefs   map[api.UUID]barMode
	viewers map[*Player]barMode
	nSide   int
	nBoss   int

	lineNBT [][]byte // display text of each line as last sent
	linePkt [][]byte // set_score packet of each line as last sent
	scratch proto.Writer
	boss    bossState
	bossOK  bool
}

func newAdminBar(s *Server) *adminBar {
	b := &adminBar{s: s, path: s.path("adminbar.json"), prefs: map[api.UUID]barMode{}, viewers: map[*Player]barMode{}}
	rand.Read(b.bossID[:])
	b.objAdd = encode(&packets.ClientboundSetObjective{
		Name: barObjective, Action: objectiveAdd, DisplayText: toText(metrics.Title()),
		Type: renderInteger, NumberFormat: proto.Some[int32](formatBlank),
	})
	b.objRemove = encode(&packets.ClientboundSetObjective{Name: barObjective, Action: objectiveRemove})
	b.display = encode(&packets.ClientboundSetDisplayObjective{Position: slotSidebar, Name: barObjective})
	b.bossRm = encode(&packets.ClientboundBossEvent{EntityUUID: b.bossID, Action: bossRemove})

	var saved map[string]barMode
	if err := readJSON(b.path, &saved); err != nil {
		s.log.Warn("admin bar settings unreadable, starting empty", "err", err)
	}
	for k, m := range saved {
		if id, err := api.ParseUUID(k); err == nil && m != (barMode{}) {
			b.prefs[id] = m
		}
	}
	return b
}

func (b *adminBar) mode(p *Player) barMode {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.viewers[p]
}

// set shows m to p and persists it for p's UUID.
func (b *adminBar) set(p *Player, m barMode) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if m == (barMode{}) {
		delete(b.prefs, p.prof.UUID)
	} else {
		b.prefs[p.prof.UUID] = m
	}
	out := make(map[string]barMode, len(b.prefs))
	for id, m := range b.prefs {
		out[id.String()] = m
	}
	if err := writeJSON(b.path, out); err != nil {
		b.s.log.Error("save admin bar settings", "err", err)
	}
	b.apply(p, m)
}

// joined restores a saved bar for a player who still holds the permission.
func (b *adminBar) joined(p *Player) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if m, ok := b.prefs[p.prof.UUID]; ok && p.HasPermission(AdminBarPermission) {
		b.apply(p, m)
	}
}

// left forgets a disconnected viewer; its client state went with it.
func (b *adminBar) left(p *Player) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.forget(p)
}

func (b *adminBar) forget(p *Player) {
	m, ok := b.viewers[p]
	if !ok {
		return
	}
	if m.Sidebar {
		b.nSide--
	}
	if m.BossBar {
		b.nBoss--
	}
	delete(b.viewers, p)
}

func (b *adminBar) apply(p *Player, m barMode) {
	old := b.viewers[p]
	var raws [][]byte
	switch {
	case m.Sidebar && !old.Sidebar:
		if b.nSide == 0 {
			b.renderSidebar(b.s.stats.latest())
		}
		raws = append(raws, b.objAdd)
		raws = append(raws, b.linePkt...)
		raws = append(raws, b.display)
	case !m.Sidebar && old.Sidebar:
		raws = append(raws, b.objRemove)
	}
	switch {
	case m.BossBar && !old.BossBar:
		if b.nBoss == 0 {
			b.boss, b.bossOK = b.renderBoss(), true
		}
		raws = append(raws, b.bossAddPacket())
	case !m.BossBar && old.BossBar:
		raws = append(raws, b.bossRm)
	}
	b.forget(p)
	if m != (barMode{}) {
		b.viewers[p] = m
		if m.Sidebar {
			b.nSide++
		}
		if m.BossBar {
			b.nBoss++
		}
	}
	p.c.SendRaws(raws)
}

// refresh drops viewers who lost the permission and sends the changed
// sidebar lines.
func (b *adminBar) refresh(st *api.ServerStats) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for p := range b.viewers {
		if !p.HasPermission(AdminBarPermission) {
			b.apply(p, barMode{})
		}
	}
	if b.nSide == 0 {
		b.lineNBT, b.linePkt = b.lineNBT[:0], b.linePkt[:0]
		return
	}
	if raws := b.renderSidebar(st); len(raws) > 0 {
		for p, m := range b.viewers {
			if m.Sidebar {
				p.c.SendRaws(raws)
			}
		}
	}
}

// renderSidebar brings lineNBT/linePkt up to date with st and returns the
// packets for the lines that changed.
func (b *adminBar) renderSidebar(st *api.ServerStats) [][]byte {
	lines := metrics.Lines(st)
	lines = lines[:min(len(lines), metrics.MaxLines)]
	var out proto.Writer
	var spans [][2]int
	var changed []int
	for i, l := range lines {
		t := toText(l)
		b.scratch.Reset()
		t.Encode(&b.scratch)
		if i < len(b.lineNBT) && bytes.Equal(b.lineNBT[i], b.scratch.B) {
			continue
		}
		if i == len(b.lineNBT) {
			b.lineNBT = append(b.lineNBT, nil)
		}
		if i >= len(b.linePkt) {
			b.linePkt = append(b.linePkt, nil)
		}
		b.lineNBT[i] = append(b.lineNBT[i][:0], b.scratch.B...)
		start := len(out.B)
		packets.Marshal(&out, &packets.ClientboundSetScore{
			ItemName: barOwners[i], ScoreName: barObjective, Value: int32(len(barOwners) - i),
			DisplayName: proto.Some(t),
		})
		spans = append(spans, [2]int{start, len(out.B)})
		changed = append(changed, i)
	}
	for i := len(lines); i < len(b.lineNBT); i++ {
		start := len(out.B)
		packets.Marshal(&out, &packets.ClientboundResetScore{EntityName: barOwners[i], ObjectiveName: proto.Some(barObjective)})
		spans = append(spans, [2]int{start, len(out.B)})
	}
	b.lineNBT, b.linePkt = b.lineNBT[:len(lines)], b.linePkt[:len(lines)]
	raws := make([][]byte, len(spans))
	for i, sp := range spans {
		raws[i] = out.B[sp[0]:sp[1]:sp[1]]
	}
	for j, i := range changed {
		b.linePkt[i] = raws[j]
	}
	return raws
}

func (b *adminBar) renderBoss() bossState {
	s := b.s
	st := *s.stats.latest()
	st.TPS, st.MSPT, st.Players = s.TPS(), s.MSPT(), s.playerCount()
	bs := bossState{title: toText(metrics.BossTitle(&st)), progress: float32(min(max(st.TPS/20, 0), 1))}
	switch metrics.TPSColor(st.TPS) {
	case "green":
		bs.color = bossGreen
	case "yellow":
		bs.color = bossYellow
	default:
		bs.color = bossRed
	}
	var w proto.Writer
	bs.title.Encode(&w)
	bs.nbt = w.B
	return bs
}

// updateBoss sends the boss bar fields that changed since the last update.
func (b *adminBar) updateBoss() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.nBoss == 0 {
		b.bossOK = false
		return
	}
	raws := b.bossUpdates(b.renderBoss())
	if len(raws) == 0 {
		return
	}
	for p, m := range b.viewers {
		if m.BossBar {
			p.c.SendRaws(raws)
		}
	}
}

func (b *adminBar) bossAddPacket() []byte {
	return encode(&packets.ClientboundBossEvent{
		EntityUUID: b.bossID, Action: bossAdd, Title: b.boss.title,
		Health: b.boss.progress, Color: b.boss.color, Dividers: overlayProgress,
	})
}

// bossUpdates returns the operations turning the sent state into nb and
// records nb as sent.
func (b *adminBar) bossUpdates(nb bossState) [][]byte {
	var raws [][]byte
	if !b.bossOK || !bytes.Equal(nb.nbt, b.boss.nbt) {
		raws = append(raws, encode(&packets.ClientboundBossEvent{EntityUUID: b.bossID, Action: bossName, Title: nb.title}))
	}
	if !b.bossOK || nb.progress != b.boss.progress {
		raws = append(raws, encode(&packets.ClientboundBossEvent{EntityUUID: b.bossID, Action: bossProgress, Health: nb.progress}))
	}
	if !b.bossOK || nb.color != b.boss.color {
		raws = append(raws, encode(&packets.ClientboundBossEvent{EntityUUID: b.bossID, Action: bossStyle, Color: nb.color, Dividers: overlayProgress}))
	}
	b.boss, b.bossOK = nb, true
	return raws
}

// AdminBar reports which admin bar parts p is shown.
func (s *Server) AdminBar(p api.Player) (sidebar, bossbar bool) {
	pl, ok := s.livePlayer(p)
	if !ok {
		return false, false
	}
	m := s.bar.mode(pl)
	return m.Sidebar, m.BossBar
}

// SetAdminBar shows or hides the admin bar parts for p and remembers the
// choice across sessions.
func (s *Server) SetAdminBar(p api.Player, sidebar, bossbar bool) bool {
	pl, ok := s.livePlayer(p)
	if !ok {
		return false
	}
	s.bar.set(pl, barMode{Sidebar: sidebar, BossBar: bossbar})
	return true
}

func (s *Server) livePlayer(p api.Player) (*Player, bool) {
	pl, ok := p.(*Player)
	if !ok || pl.s != s || !pl.joined.Load() {
		return nil, false
	}
	return pl, true
}
